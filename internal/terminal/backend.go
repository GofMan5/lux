// Package terminal implements the default VT terminal backend.
package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"golang.org/x/term"
)

const (
	enterTerminal = "\x1b[?1049h\x1b[?25l\x1b[?1004h\x1b[?2004h\x1b[?1002h\x1b[?1006h"
	leaveTerminal = "\x1b[?1006l\x1b[?1002l\x1b[?2004l\x1b[?1004l\x1b[?25h\x1b[?1049l\x1b[0m"
	// kittyEnter pushes the kitty keyboard protocol's disambiguate and
	// event-type flags (0b11, written as 3): ambiguous keys arrive as
	// complete CSI u sequences while plain text and functional keys keep
	// their legacy encodings, and each such sequence reports whether it is
	// a press, an auto-repeat, or a release. Enter, Tab, and Backspace
	// keep their legacy bytes and never report releases — the spec's
	// shell-safety exception — so the release stream stays text-free.
	// Releases are decoded in full but filtered by the app layer unless
	// the program opts in; alternate keys, all-keys-as-codes, and
	// associated text stay off until a later stage can represent them.
	// The push is written in the same Write call as enterTerminal, and the
	// matching pop shares a Write with leaveTerminal on every exit path —
	// including the enter failure below and Close — so the terminal's
	// flag stack cannot leak a frame behind the program, the bubbletea
	// v2.0.9 failure mode.
	kittyEnter = "\x1b[>3u"
	kittyLeave = "\x1b[<u"
	escapeDelay = 35 * time.Millisecond
)

// Backend is a VT-first terminal adapter. Its methods serialize lifecycle and
// output state; one goroutine may block in NextEvent while another presents.
type Backend struct {
	input  *os.File
	output *os.File
	writer *os.File

	mu           sync.Mutex
	writeOnce    sync.Once
	writeSlot    chan struct{}
	encoded      []byte
	opened       bool
	plain        bool
	kitty        bool
	features     capability.Set
	state        *term.State
	restoreVT    func() error
	previous     *render.Buffer
	parser       *parser
	events       chan event.Event
	resizes      chan event.Event
	cancel       context.CancelFunc
	readDone     chan struct{}
	resize       resizeWatcher
	lastResizeW  int
	lastResizeH  int
	lastResizeOK bool
}

// New constructs a closed backend. Nil files are allowed for tests and plain
// output-only use, but an interactive Open requires both terminal handles.
func New(input *os.File, output *os.File) *Backend {
	return &Backend{input: input, output: output, parser: newParser(), events: make(chan event.Event, 64), resizes: make(chan event.Event, 1)}
}

// Open activates raw input and terminal modes when both handles are terminals.
// Redirected output and TERM=dumb use a portable plain-text fallback.
func (b *Backend) Open(ctx context.Context) (capability.Set, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.opened {
		return b.features, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.plain = b.output == nil || os.Getenv("TERM") == "dumb" || !isTerminal(b.output)
	if b.plain {
		writer, err := duplicateFile(b.output, "lux-tui-output")
		if err != nil && b.output != nil {
			return capability.Set{}, err
		}
		b.writer = writer
		b.features = capability.Portable()
		b.events = make(chan event.Event, 64)
		b.opened = true
		return b.features, nil
	}
	if b.input == nil || !isTerminal(b.input) {
		return capability.Set{}, errors.New("terminal: interactive output requires terminal input")
	}
	state, err := term.MakeRaw(int(b.input.Fd()))
	if err != nil {
		return capability.Set{}, err
	}
	b.state = state
	restoreVT, err := enableVT(b.output)
	if err != nil {
		_ = term.Restore(int(b.input.Fd()), b.state)
		b.state = nil
		return capability.Set{}, err
	}
	b.restoreVT = restoreVT
	writer, err := duplicateFile(b.output, "lux-tui-output")
	if err != nil {
		_ = term.Restore(int(b.input.Fd()), b.state)
		_ = b.restoreVT()
		b.state, b.restoreVT = nil, nil
		return capability.Set{}, err
	}
	b.writer = writer
	b.features = detectCapabilities(syncOutputConfigured())
	// The size is snapshotted before the probe and compared after it: a
	// resize inside the probe window is invisible to the watchers, which
	// start only once the probe returns — Unix has no SIGWINCH handler
	// registered yet, and the Windows poller takes its baseline after the
	// probe has already passed. Windows terminalSize answers through the
	// console's screen buffer no matter which handle it is given, so the
	// snapshot works through the input file on both platforms.
	preWidth, preHeight, sizeKnown := 0, 0, false
	if width, height, err := terminalSize(b.input); err == nil {
		preWidth, preHeight, sizeKnown = width, height, true
	}
	// Negotiation runs while raw mode is already active but before the
	// alternate screen is entered, so the probe is matched against a quiet
	// stream: the only reports in flight are the ones it requested.
	var leftover []byte
	if timeout, probe := probeTimeoutConfigured(); probe {
		reply, pending := negotiate(ctx, b.input, b.writer, timeout)
		b.features = applyProbeFeatures(b.features, reply)
		leftover = pending
	}
	// kitty is latched before the enter write so every exit path — the
	// failure branch below, Close, or a second Open — knows whether the
	// protocol was pushed and must be popped.
	b.kitty = b.features.KeyboardEnhanced
	enter, leave := enterTerminal, leaveTerminal
	if b.kitty {
		enter += kittyEnter
		leave = kittyLeave + leave
	}
	b.opened = true
	if err := writeAll(b.writer, []byte(enter)); err != nil {
		// The pop rides the same write as the legacy restore: a pop without
		// a matching push is a no-op on a protocol terminal, and a
		// half-delivered enter cannot leave the stack pushed without its
		// corresponding pop being attempted.
		_ = writeAll(b.writer, []byte(leave))
		_ = b.writer.Close()
		b.writer = nil
		_ = term.Restore(int(b.input.Fd()), b.state)
		if b.restoreVT != nil {
			_ = b.restoreVT()
			b.restoreVT = nil
		}
		b.state = nil
		b.opened = false
		return capability.Set{}, err
	}
	readCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.readDone = make(chan struct{})
	b.events = make(chan event.Event, 64)
	b.resizes = make(chan event.Event, 1)
	// The watcher registers before the post-probe snapshot is taken, so a
	// resize landing in the microseconds between the two is caught by
	// whichever of them sees it first: Unix buffers the SIGWINCH, the
	// Windows poller notices the change on its first tick, and the replay
	// below covers everything before the watcher existed.
	b.resize.start(readCtx, b.input, b.resizes)
	// A resize that landed inside the probe window is replayed here as the
	// first event, so the app's first layout reflects the real terminal size
	// instead of the pre-probe snapshot. The offer is plain, not
	// deliverResize: the watcher's report is the authoritative one, so a
	// replay never displaces it, and equal sizes coalesce in NextEvent.
	postWidth, postHeight, postErr := terminalSize(b.input)
	if resize, resized := resizeDuringProbe(sizeKnown, preWidth, preHeight, postWidth, postHeight, postErr); resized {
		select {
		case b.resizes <- resize:
		default:
		}
	}
	// Keystrokes that raced the probe travel to the read loop as a prefill:
	// Open still holds the mutex here, so replaying events into the channel
	// in place could wedge Open against a full buffer on a large paste, and
	// the parser belongs to the loop. The loop feeds the prefill before its
	// first read, which preserves the byte order.
	go b.readLoop(readCtx, b.readDone, b.events, b.parser, leftover)
	return b.features, nil
}

// resizeDuringProbe reports the resize event synthesized for a size change
// that landed inside the probe window. The watchers start only after the
// probe returns, so without this replay the first layout would miss a
// terminal resized mid-handshake. It is pure so the comparison itself is
// table-tested; Open supplies the snapshots from both sides of the probe.
func resizeDuringProbe(sizeKnown bool, preWidth, preHeight, postWidth, postHeight int, postErr error) (event.Event, bool) {
	if !sizeKnown || postErr != nil || (postWidth == preWidth && postHeight == preHeight) {
		return event.Event{}, false
	}
	return event.ResizeTo(postWidth, postHeight), true
}

// deliverResize writes a resize into the watcher's one-slot channel with
// last-wins semantics. The shared input queue can sit 64 events deep while
// the app drains a paste, and a resize that merely attempted a non-blocking
// send would be dropped and never retried: the Windows poller advances its
// size baseline before the send and Unix has already consumed the SIGWINCH.
// Overwriting the slot instead guarantees the newest size is only delayed
// by the queue depth, never lost; NextEvent coalesces the duplicates that
// delivers. Overwriting the slot instead guarantees the newest size is only
// delayed by the queue depth, never lost; NextEvent coalesces the duplicates
// that leaves behind. The slot is bidirectional because draining the stale
// event is half of the last-wins swap.
func deliverResize(slot chan event.Event, resize event.Event) {
	select {
	case slot <- resize:
	default:
		select {
		case <-slot:
		default:
		}
		select {
		case slot <- resize:
		default:
		}
	}
}

// NextEvent waits for the next decoded input or resize event.
func (b *Backend) NextEvent(ctx context.Context) (event.Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	opened, plain := b.opened, b.plain
	b.mu.Unlock()
	if !opened {
		return event.Event{}, errors.New("terminal: backend is not open")
	}
	if plain {
		<-ctx.Done()
		return event.Event{}, ctx.Err()
	}
	for {
		// A pending resize outranks queued input: it carries the newest
		// terminal truth, and its one-slot channel would otherwise sit
		// behind up to 64 buffered key events.
		select {
		case next := <-b.resizes:
			if done, ev, err := b.deliverEvent(next); done {
				return ev, err
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return event.Event{}, ctx.Err()
		case next := <-b.events:
			if done, ev, err := b.deliverEvent(next); done {
				return ev, err
			}
		case next := <-b.resizes:
			if done, ev, err := b.deliverEvent(next); done {
				return ev, err
			}
		}
	}
}

// deliverEvent applies the post-processing every event shares on its way out
// of NextEvent: the closed check, resize coalescing against the last size
// the app actually saw, and the timestamp default. done=false asks the
// caller to keep waiting — the event was a coalesced duplicate.
func (b *Backend) deliverEvent(next event.Event) (done bool, ev event.Event, err error) {
	b.mu.Lock()
	stillOpen := b.opened
	b.mu.Unlock()
	if !stillOpen {
		return true, event.Event{}, errors.New("terminal: backend is closed")
	}
	if next.Kind == event.Resize {
		b.mu.Lock()
		if b.lastResizeOK && next.Width == b.lastResizeW && next.Height == b.lastResizeH {
			b.mu.Unlock()
			return false, event.Event{}, nil
		}
		b.lastResizeW, b.lastResizeH = next.Width, next.Height
		b.lastResizeOK = true
		b.mu.Unlock()
	}
	if next.At.IsZero() {
		next.At = time.Now()
	}
	if next.Kind == event.Error {
		return true, next, next.Err
	}
	return true, next, nil
}

// Present renders a frame. Interactive terminals receive changed row runs;
// portable output receives a complete text snapshot per call.
func (b *Backend) Present(ctx context.Context, frame *render.Buffer) error {
	if frame == nil {
		return errors.New("terminal: nil frame")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := b.writeSlotChan()
	// The acquire honors ctx the same way Close's bounded restoreGrace does:
	// a Present parked behind a writer stuck in one Write call must surface
	// the cancellation instead of parking forever.
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slot }()
	b.mu.Lock()
	if !b.opened {
		b.mu.Unlock()
		return errors.New("terminal: backend is not open")
	}
	if b.writer == nil {
		b.mu.Unlock()
		return errors.New("terminal: nil output")
	}
	output, plain, synchronized, scrollRegions, features, previous := b.writer, b.plain, b.features.SynchronizedOutput, b.features.ScrollRegions, b.features, b.previous
	b.mu.Unlock()
	// The write slot serializes Present, so the encode scratch and the retained
	// frame are reused instead of reallocated once per frame.
	var data []byte
	if plain {
		data = appendPlain(b.encoded[:0], frame)
	} else {
		data = appendFrame(b.encoded[:0], frame, previous, features.Color, scrollRegions, synchronized)
	}
	b.encoded = data
	if len(data) == 0 {
		return nil
	}
	if err := writeAllContext(ctx, output, data); err != nil {
		// The encoder may already have moved rows in its model of the terminal,
		// and a failed write leaves the real terminal in an unknown state, so the
		// model is discarded and the next frame repaints from scratch.
		b.mu.Lock()
		if b.previous == previous {
			b.previous = nil
		}
		b.mu.Unlock()
		return err
	}
	b.mu.Lock()
	if b.opened && b.previous == previous {
		if previous == nil {
			b.previous = frame.Clone()
		} else {
			previous.CopyFrom(frame)
		}
	}
	b.mu.Unlock()
	return nil
}

// Size returns the current terminal size, preferring the output handle and
// falling back to the input side. A stream redirected away from the terminal
// has no size of its own — with the notable Windows case that stdout piped
// to a file while a console stays attached still answers through that
// console. When neither side is a terminal, there is no size to report.
func (b *Backend) Size() (int, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.output != nil && isTerminal(b.output) {
		return terminalSize(b.output)
	}
	if b.input != nil && isTerminal(b.input) {
		return terminalSize(b.input)
	}
	return 0, 0, errors.New("terminal: size unavailable for redirected streams")
}

// restoreGrace bounds how long Close waits for an in-flight frame before it
// restores the terminal without writing its own sequences.
const restoreGrace = 100 * time.Millisecond

// writeSlotChan returns the one-slot semaphore that serializes writer access.
// It is a channel rather than a mutex because Close must be able to stop
// waiting for it.
func (b *Backend) writeSlotChan() chan struct{} {
	b.writeOnce.Do(func() { b.writeSlot = make(chan struct{}, 1) })
	return b.writeSlot
}

// writerBusy reports whether a write currently holds the slot. It exists for
// tests that need to observe a blocked writer.
func (b *Backend) writerBusy() bool { return len(b.writeSlotChan()) == 1 }

func (b *Backend) acquireWriteSlot(wait time.Duration) bool {
	slot := b.writeSlotChan()
	select {
	case slot <- struct{}{}:
		return true
	default:
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case slot <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// Close disables terminal modes and restores the exact raw input state saved
// by Open. It is idempotent and makes a best effort to perform both restores.
func (b *Backend) Close() error {
	b.mu.Lock()
	if !b.opened {
		b.mu.Unlock()
		return nil
	}
	b.opened = false
	cancel, done := b.cancel, b.readDone
	b.cancel, b.readDone = nil, nil
	b.resize.stop()
	plain, writer, input, state, restoreVT, kitty := b.plain, b.writer, b.input, b.state, b.restoreVT, b.kitty
	b.writer = nil
	b.state = nil
	b.restoreVT = nil
	b.kitty = false
	b.previous = nil
	b.lastResizeOK = false
	b.mu.Unlock()

	if cancel != nil {
		cancel()
		cancelRead(input)
	}
	var result error
	if writer != nil {
		// Splicing the restore into a frame another goroutine is still writing
		// would corrupt the screen, so it is only written once the writer is idle.
		// Waiting forever is not an option either: a write blocked on a terminal
		// that stopped draining holds the slot until the descriptor is closed, and
		// on Windows it may not return even then.
		if !plain && b.acquireWriteSlot(restoreGrace) {
			// The kitty pop leads the legacy restore so the stack is unwound
			// before the modes it was layered on, mirroring the enter order.
			// Sharing one Write with leaveTerminal is what guarantees the pop
			// happens wherever the restore does.
			leave := leaveTerminal
			if kitty {
				leave = kittyLeave + leave
			}
			result = errors.Join(result, writeAll(writer, []byte(leave)))
			<-b.writeSlotChan()
		}
		result = errors.Join(result, writer.Close())
	}
	if b.acquireWriteSlot(restoreGrace) {
		<-b.writeSlotChan()
	}
	if state != nil && input != nil {
		result = errors.Join(result, term.Restore(int(input.Fd()), state))
	}
	if restoreVT != nil {
		result = errors.Join(result, restoreVT())
	}
	// Closing/cancelling does not reliably interrupt a blocked console read on
	// every platform, so Close never waits for readDone.
	_ = done
	return result
}

func (b *Backend) readLoop(ctx context.Context, done chan struct{}, events chan<- event.Event, parser *parser, prefill []byte) {
	defer func() {
		close(done)
		if ctx.Err() == nil {
			b.restoreInput()
		}
	}()
	buffer := make([]byte, 4096)
	if len(prefill) > 0 {
		// Probe leftovers are replayed before the first read so keystrokes
		// that raced the probe keep their place in the stream. eof=false
		// leaves a truncated escape tail pending for the escape delay to
		// resolve, exactly like bytes from a regular read.
		for _, next := range parser.feed(prefill, false) {
			select {
			case events <- next:
			case <-ctx.Done():
				return
			}
		}
	}
	for {
		wait := time.Duration(-1)
		if parser.pendingEscape() {
			wait = escapeDelay
		}
		count, err := readInput(ctx, b.input, buffer, wait)
		if count > 0 {
			for _, next := range parser.feed(buffer[:count], false) {
				select {
				case events <- next:
				case <-ctx.Done():
					return
				}
			}
		}
		if errors.Is(err, errReadTimeout) {
			for _, next := range parser.flushEscape() {
				select {
				case events <- next:
				case <-ctx.Done():
					return
				}
			}
			continue
		}
		if count == 0 && err == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, io.EOF) {
				for _, next := range parser.feed(nil, true) {
					select {
					case events <- next:
					case <-ctx.Done():
						return
					}
				}
				select {
				case events <- event.Event{Kind: event.Error, Err: io.EOF}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case events <- event.Event{Kind: event.Error, Err: err}:
			case <-ctx.Done():
			}
			return
		}
	}
}

func (b *Backend) restoreInput() {
	b.mu.Lock()
	state, input := b.state, b.input
	b.state = nil
	b.mu.Unlock()
	if state != nil && input != nil {
		_ = term.Restore(int(input.Fd()), state)
	}
}

func isTerminal(file *os.File) bool { return file != nil && term.IsTerminal(int(file.Fd())) }

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count <= 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}

func writeAllContext(ctx context.Context, writer io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count <= 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}

func detectCapabilities(syncOutput bool) capability.Set {
	terminal := os.Getenv("TERM")
	if terminal == "" {
		terminal = "vt"
	}
	// The platform floor is what the host is known to render without help;
	// environment overrides below raise it when the terminal earned more.
	color := defaultColorDepth()
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") {
		color = capability.TrueColor
	} else if color < capability.ANSI256 && strings.Contains(terminal, "256color") {
		color = capability.ANSI256
	}
	if os.Getenv("NO_COLOR") != "" {
		color = capability.Monochrome
	}
	return capability.Set{
		Terminal:         terminal,
		Interactive:      true,
		Unicode:          true,
		Color:            color,
		AlternateScreen:  true,
		CursorAddressing: true,
		Mouse:            true,
		Focus:            true,
		BracketedPaste:   true,
		// Scroll regions are the historical baseline every VT102 descendant
		// implements; negotiation's job is only to veto this default.
		ScrollRegions:      true,
		SynchronizedOutput: syncOutput,
		Graphics:           capability.GraphicsCells,
	}
}

func syncOutputConfigured() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("LUX_TUI_SYNC_OUTPUT")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
