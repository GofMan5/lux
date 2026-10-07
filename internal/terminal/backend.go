// Package terminal implements the default VT terminal backend.
package terminal

import (
	"context"
	"errors"
	"fmt"
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
	// Mouse tracking asks for 1003 all-motion rather than 1002
	// motion-while-pressed: hover states need plain pointer motion with no
	// button held, which 1003 reports as SGR button 35. The 1006 SGR
	// encoding rides along as before.
	enterTerminal = "\x1b[?1049h\x1b[?25l\x1b[?1004h\x1b[?2004h\x1b[?1003h\x1b[?1006h"
	leaveTerminal = "\x1b[?1006l\x1b[?1003l\x1b[?2004l\x1b[?1004l\x1b[?25h\x1b[?1049l\x1b[0m"
	// enterPixel is the pixel-mouse twin of enterTerminal: mode 1016
	// replaces 1006 rather than stacking on it (kitty's SGR pixel encoding
	// uses the same CSI < shape with pixel coordinates in place of cell
	// numbers), and the cell-size query rides the same write so the reply
	// is already in flight when the read loop starts. It is a full
	// constant, not enterTerminal with substitutions, so the enter/leave
	// pair stays visually diffable.
	enterPixel = "\x1b[?1049h\x1b[?25l\x1b[?1004h\x1b[?2004h\x1b[?1003h\x1b[?1016h\x1b[16t"
	leavePixel = "\x1b[?1016l\x1b[?1003l\x1b[?2004l\x1b[?1004l\x1b[?25h\x1b[?1049l\x1b[0m"
	// cellSizeQuery re-asks the cell size after a resize: the query is
	// CSI 16 t and the reply CSI 6 ; height ; width t, which the parser
	// absorbs to keep pixel-to-cell mapping current.
	cellSizeQuery = "\x1b[16t"
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
	// win32Enter turns on win32-input-mode, the Windows terminal
	// counterpart of kitty's disambiguate flag: DEC private mode 9001
	// makes a Windows terminal report key events as CSI-underscore
	// records carrying the raw KEY_EVENT_RECORD fields, so ambiguous
	// compositions (Ctrl+letters, Alt pairs, releases, AltGr text)
	// arrive labeled instead of pre-encoded. Unlike the kitty protocol
	// this is a plain DEC private mode — a literal set and reset with no
	// flag stack to push or pop — so the leave is an unconditional mode
	// reset. The mode is only entered when the terminal answered the 9001
	// probe and kitty did not answer: kitty carries the same
	// disambiguation plus releases on keys 9001 leaves shell-safe and
	// alternate-key detail 9001 cannot express, so kitty wins when both
	// are available and 9001 fills the gap on terminals without it.
	win32Enter  = "\x1b[?9001h"
	win32Leave  = "\x1b[?9001l"
	escapeDelay = 35 * time.Millisecond
)

// Backend is a VT-first terminal adapter. Its methods serialize lifecycle and
// output state; one goroutine may block in NextEvent while another presents.
type Backend struct {
	input  *os.File
	output *os.File
	writer *os.File

	mu        sync.Mutex
	writeOnce sync.Once
	writeSlot chan struct{}
	encoded   []byte
	opened    bool
	plain     bool
	kitty     bool
	// win32 latches win32-input-mode use: it needs a probe answer for
	// mode 9001 and the kitty protocol being absent, and every exit path
	// uses it to decide whether the mode reset belongs in the restore.
	win32 bool
	// pixelOpted records the application's WithPixelMouse choice; pixel
	// latches the mode actually in use, which needs both the opt-in and
	// a terminal that answered the 1016 probe.
	pixelOpted bool
	pixel      bool
	// hyperlinkOpted records the application's Hyperlinks choice; hyperlink
	// latches the encoder's OSC 8 emission, which needs only the opt-in: the
	// hyperlink specification defines no detection, so silence cannot be
	// separated from support.
	hyperlinkOpted bool
	hyperlink      bool
	features       capability.Set
	state          *term.State
	restoreVT      func() error
	previous       *render.Buffer
	parser         *parser
	events         chan event.Event
	resizes        chan event.Event
	cancel         context.CancelFunc
	readDone       chan struct{}
	resize         resizeWatcher
	lastResizeW    int
	lastResizeH    int
	lastResizeOK   bool
}

// Option configures a Backend at construction time, before any Open.
type Option func(*Backend)

// PixelMouse opts into pixel-precision mouse tracking (kitty mode 1016) for
// terminals that answer the probe's mode query. The opt-in is required
// because the encoding is a breaking change on the input stream: coordinates
// become pixels, pointer leaves arrive as dedicated reports, and buttons
// beyond the classic three appear. A terminal without the mode, or a program
// that never opts in, keeps the cell encoding unchanged. The choice is
// honored per Open, so a second Open after Close re-negotiates.
func PixelMouse(enabled bool) Option {
	return func(b *Backend) { b.pixelOpted = enabled }
}

// Hyperlinks opts into OSC 8 hyperlinks in presented frames. The opt-in is
// required because the hyperlink specification defines no detection: an
// answer cannot be separated from silence, so the application decides. On
// terminals without OSC 8 the regions degrade to plain text, and a program
// that never opts in encodes frames byte-identical to a build without link
// support. The choice is honored per Open, like every other Option.
func Hyperlinks(enabled bool) Option {
	return func(b *Backend) { b.hyperlinkOpted = enabled }
}

// New constructs a closed backend. Nil files are allowed for tests and plain
// output-only use, but an interactive Open requires both terminal handles.
func New(input *os.File, output *os.File, opts ...Option) *Backend {
	backend := &Backend{input: input, output: output, parser: newParser(), events: make(chan event.Event, 64), resizes: make(chan event.Event, 1)}
	for _, opt := range opts {
		if opt != nil {
			opt(backend)
		}
	}
	return backend
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
	// Only a forced-on override claims synchronized output upfront: with
	// the probe disabled there is no reply to enable it, while auto and
	// forced-off start without the wrapper and let the verdict below (or
	// the override, which the verdict re-applies) decide.
	sync := syncOutputConfigured()
	b.features = detectCapabilities(sync == syncOutputOn)
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
		b.features = applyProbeFeatures(b.features, reply, sync)
		leftover = pending
	}
	// kitty and pixel are latched before the enter write so every exit path
	// — the failure branch below, Close, or a second Open — knows which
	// sequences were pushed and must be popped.
	b.kitty = b.features.KeyboardEnhanced
	b.pixel = b.pixelOpted && b.features.PixelMouse
	// win32-input-mode is the Windows stand-in for kitty disambiguation
	// and never runs alongside it: kitty's CSI u carries the same
	// disambiguation plus releases on the text-safe keys, so it wins when
	// both are available and 9001 fills the gap on terminals without it.
	b.win32 = b.features.Win32Input && !b.kitty
	// OSC 8 has no probe to latch from: the opt-in alone decides, and the
	// encoder closes every region it opens, so a terminal without support
	// just renders the text.
	b.hyperlink = b.hyperlinkOpted
	// The parser decodes in the mode that is actually entered, from the
	// very first byte the read loop sees — including the cell-size reply
	// that enterPixel just put in flight.
	b.parser.pixelMouse = b.pixel
	b.parser.win32Input = b.win32
	enter, leave := enterTerminal, leaveTerminal
	if b.pixel {
		enter, leave = enterPixel, leavePixel
	}
	if b.kitty {
		enter += kittyEnter
		leave = kittyLeave + leave
	}
	if b.win32 {
		enter += win32Enter
		leave = win32Leave + leave
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
		pixel, writer := b.pixel, b.writer
		b.mu.Unlock()
		if pixel && writer != nil && b.acquireWriteSlot(0) {
			// A resize can change the cell size along with the screen
			// dimensions, so the query goes out again and the parser
			// re-learns the mapping from the next reply. The slot is taken
			// without waiting — a frame being presented simply wins, and
			// the stale cell size only degrades coordinate precision until
			// the next resize — and the write happens off the mutex so it
			// cannot deadlock against Close, which takes both.
			_ = writeAll(writer, []byte(cellSizeQuery))
			<-b.writeSlotChan()
		}
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
	output, plain, synchronized, scrollRegions, links, features, previous := b.writer, b.plain, b.features.SynchronizedOutput, b.features.ScrollRegions, b.hyperlink, b.features, b.previous
	b.mu.Unlock()
	// The write slot serializes Present, so the encode scratch and the retained
	// frame are reused instead of reallocated once per frame.
	var data []byte
	if plain {
		data = appendPlain(b.encoded[:0], frame)
	} else {
		data = appendFrame(b.encoded[:0], frame, previous, features.Color, scrollRegions, synchronized, links)
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
	plain, writer, input, state, restoreVT, kitty, win32, pixel := b.plain, b.writer, b.input, b.state, b.restoreVT, b.kitty, b.win32, b.pixel
	b.writer = nil
	b.state = nil
	b.restoreVT = nil
	b.kitty = false
	b.win32 = false
	b.pixel = false
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
			// The win32 reset rides in front of it — the mode has no stack,
			// so this is a plain reset — and sharing one Write with
			// leaveTerminal is what guarantees each restore happens wherever
			// the others do.
			leave := leaveTerminal
			if pixel {
				leave = leavePixel
			}
			if kitty {
				leave = kittyLeave + leave
			}
			if win32 {
				leave = win32Leave + leave
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
		// Synchronized output is the opposite: unconfirmed terminals must
		// never receive the mode-set wrapper, so only a forced-on override
		// claims it here — every other path waits for the probe.
		ScrollRegions:      true,
		SynchronizedOutput: syncOutput,
		Graphics:           capability.GraphicsCells,
	}
}

// syncOutputMode is the LUX_TUI_SYNC_OUTPUT setting. The probe owns the
// auto default: a terminal that positively recognizes mode 2026 gets the
// frame wrapper, and silence never does. The two forced modes are the
// escape hatch for a report that lies, each overriding the probe verdict.
type syncOutputMode uint8

const (
	// syncOutputAuto lets the DECRPM 2026 reply decide.
	syncOutputAuto syncOutputMode = iota
	// syncOutputOn forces the wrapper on even when the probe disagrees.
	syncOutputOn
	// syncOutputOff forces the wrapper off even when the probe agrees.
	syncOutputOff
)

// syncOutputConfigured reads LUX_TUI_SYNC_OUTPUT. Empty, "auto", and
// "default" leave the decision to the probe; "1", "true", "yes", and "on"
// force the wrapper on; "0", "false", "no", and "off" force it off.
// Anything else falls back to auto after printing a one-line notice on
// stderr — Open runs before the alternate screen is entered, so the notice
// cannot disturb a frame.
func syncOutputConfigured() syncOutputMode {
	switch value := strings.ToLower(strings.TrimSpace(os.Getenv("LUX_TUI_SYNC_OUTPUT"))); value {
	case "", "auto", "default":
		return syncOutputAuto
	case "1", "true", "yes", "on":
		return syncOutputOn
	case "0", "false", "no", "off":
		return syncOutputOff
	default:
		fmt.Fprintf(os.Stderr, "lux-tui: invalid LUX_TUI_SYNC_OUTPUT %q, leaving the decision to the probe\n", value)
		return syncOutputAuto
	}
}
