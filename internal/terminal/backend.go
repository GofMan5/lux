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

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
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
	win32Enter = "\x1b[?9001h"
	win32Leave = "\x1b[?9001l"
	// unicodeCoreEnter turns on Unicode core mode, DEC private mode 2027
	// from the contour-led terminal-unicode-core proposal. An enabled
	// terminal clusters printed text into extended grapheme clusters per
	// UAX #29 itself, measures emoji as two cells, may widen
	// VS16-prefixed clusters to two cells, and erases-plus-overwrites
	// when text is printed at a cluster cell. This backend's own
	// measurement is already cluster-aware and overlay-updated to
	// Unicode 18.0.0, so enabling the mode changes no measurement code:
	// it moves the terminal from coincidentally agreeing with our cells
	// to being held to the same clustering contract. The proposal is an
	// ALPHA draft with one implementing terminal (contour) and one more
	// consumer (Charm's Ultraviolet), so the mode is entered only after
	// the terminal positively answered the 2027 probe — silence or
	// rejection keeps it off. Ultraviolet does not reset the mode on
	// restore; the spec leaves 2027's default undefined after DECSTR
	// and RIS, so this backend resets it on every exit path, like every
	// other mode it enters.
	unicodeCoreEnter = "\x1b[?2027h"
	unicodeCoreLeave = "\x1b[?2027l"
	// titlePush and titlePop are the XTWINOPS title stack: CSI 22;0 t
	// saves the icon and window titles, CSI 23;0 t restores them. The
	// stack is the only restore mechanism that needs no report parsing —
	// the CSI 21 t title query has no uniform reply shape across
	// terminals, and a misparsed reply would leak as keystrokes — so the
	// push rides the enter Write, the pop rides the leave Write on every
	// exit path, and the pair is the same contract kitty's flag stack
	// already follows: never pop what was not pushed. A terminal without
	// the stack ignores both operations, which leaves the program's title
	// behind on exit there — the documented matrix, not a silent failure:
	// OSC 0 itself is honored by every terminal that passes the
	// interactive gate.
	titlePush   = "\x1b[22;0t"
	titlePop    = "\x1b[23;0t"
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
	// suspended latches the job-control state between the terminal half and
	// the present path: suspendTerminal sets it under the mutex before any
	// restore is attempted, Present refuses to write frames while it is set
	// (a frame cannot be painted onto the shell's screen), and resumeTerminal
	// clears it only after the terminal is back. The latch also makes plain
	// and closed backends refuse the whole dance, which keeps the signal
	// watchers from stopping a process that never had terminal state.
	suspended bool
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
	// unicodeCoreOpted records the application's UnicodeCore choice: nil
	// means automatic (the probe's mode 2027 verdict decides), and an
	// explicit true or false overrides the probe both ways — the escape
	// hatch for a terminal whose report lies. unicodeCore latches the
	// mode actually in use; like kitty and win32, every exit path uses
	// it to decide whether the reset belongs in the restore.
	unicodeCoreOpted *bool
	unicodeCore      bool
	// title holds the sanitized window title the application asked for.
	// An empty string means no title program: nothing is pushed, set, or
	// popped on any path. It survives Close — it is an application choice,
	// not a probe latch, so a second Open restores the same title.
	// progress records the application's Progress choice: the annex
	// writes themselves arrive per frame through WriteAnnex, but the
	// leave-time OSC 9;4 reset needs no frame, so the latch lives here.
	// Like title it survives Close — a second Open keeps resetting.
	title        string
	progress     bool
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

// UnicodeCore overrides the automatic Unicode core mode decision (DEC private
// mode 2027, the contour-led terminal-unicode-core proposal). By default the
// backend enables the mode exactly when the terminal positively answered the
// probe's 2027 query; UnicodeCore(true) forces it on despite silence, and
// UnicodeCore(false) keeps it off despite a positive answer. The app package
// passes false whenever the application claimed an explicit text width policy,
// because an explicit claim and mode 2027 would both steer the same stream and
// the explicit claim wins. The override cannot conjure behavior out of nothing:
// the backend still writes only the set-mode sequence, and a terminal without
// the mode ignores it — the option exists for terminals whose reports lie,
// not for ones that stay silent. The choice is honored per Open, like every
// other Option.
func UnicodeCore(enabled bool) Option {
	return func(b *Backend) {
		set := enabled
		b.unicodeCoreOpted = &set
	}
}

// Title sets the terminal window and icon title for the next Open. The
// title is sanitized — C0 controls, DEL, and one-byte C1 controls are
// stripped, because a BEL or ESC inside an OSC 0 string would end the
// sequence early and leak the rest of the title as commands — while every
// graphic rune, Unicode included, passes through. An empty title after
// sanitization means no title program: the backend writes no title bytes
// at all. The title is a lifecycle write, not a per-frame one: it rides
// the same Write call as the enter sequences, and its restore rides the
// leave. Plain and redirected streams never write title bytes, because
// they never write the enter either. The choice is honored per Open, like
// every other Option.
func Title(title string) Option {
	return func(b *Backend) { b.title = sanitizeTitle(title) }
}

// Progress opts into OSC 9;4 taskbar progress reporting. The per-frame
// writes arrive through WriteAnnex — the application queues them during
// View — but the option also arms the leave-time reset: every leave
// sequence Close, suspend, and the Open failure path write gains the
// OSC 9;4 "no progress" state, because a taskbar progress bar belongs to
// the running program and must not outlive it on terminals that showed
// one (Windows Terminal, ConEmu, iTerm2). Terminals without OSC 9;4
// ignore the reset exactly as they ignored the writes. Plain and
// redirected streams never write either — they never write the leave
// that carries the reset. The choice is honored per Open, like every
// other Option.
func Progress(enabled bool) Option {
	return func(b *Backend) { b.progress = enabled }
}

// sanitizeTitle returns title without the bytes an OSC string cannot
// carry: C0 controls (a BEL would end the title early, an ESC would start
// a new sequence), DEL, and the one-byte C1 controls. Graphic bytes —
// including every multi-byte UTF-8 sequence — pass through, so a title in
// any script survives intact; invalid UTF-8 is left as-is too, because it
// cannot inject a control sequence into a terminal in UTF-8 mode.
func sanitizeTitle(title string) string {
	if strings.IndexFunc(title, unsafeTitleRune) < 0 {
		return title
	}
	var clean strings.Builder
	clean.Grow(len(title))
	for _, r := range title {
		if !unsafeTitleRune(r) {
			clean.WriteRune(r)
		}
	}
	return clean.String()
}

// unsafeTitleRune reports whether r is a control rune an OSC 0 title must
// never carry: C0, DEL, or C1.
func unsafeTitleRune(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// setWindowTitle builds the OSC 0 sequence that sets the icon and window
// title to title. The BEL terminator is the portable form: every terminal
// that honors OSC at all accepts it.
func setWindowTitle(title string) string {
	return "\x1b]0;" + title + "\a"
}

// sequences builds the enter and leave byte strings for a latch state.
// Open and Close both call it, so the restore a terminal receives on any
// exit path — the enter failure below, Present's write error, or Close —
// is exactly the inverse of the enter it received, assembled from one
// table instead of two hand-maintained compositions. Order: the title
// push and set lead the enter (the title changes before any mode the
// modes could interact with), and the title pop closes the leave (the
// stack unwinds after every mode is reset, mirroring the enter order in
// reverse — the same discipline the kitty pop follows). The progress
// reset rides at the very end of the leave: it is not a mode with a
// stack, and clearing the taskbar after everything else has been
// restored keeps the terminal's own state fully back before the
// program's desktop-integration state is dismissed.
func sequences(pixel, kitty, win32, unicodeCore bool, title string, progress bool) (enter, leave string) {
	enter, leave = enterTerminal, leaveTerminal
	if pixel {
		enter, leave = enterPixel, leavePixel
	}
	if kitty {
		enter += kittyEnter
		leave = kittyLeave + leave
	}
	if win32 {
		enter += win32Enter
		leave = win32Leave + leave
	}
	if unicodeCore {
		enter += unicodeCoreEnter
		leave = unicodeCoreLeave + leave
	}
	if title != "" {
		enter = titlePush + setWindowTitle(title) + enter
		leave = leave + titlePop
	}
	if progress {
		leave += progressClear
	}
	return enter, leave
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
	state, err := makeRaw(int(b.input.Fd()))
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
		// The provenance gate runs before verdicts are applied: when the
		// reports came from a multiplexer's pane, the input-family answers
		// describe what the host speaks to the mux, not what the pane
		// receives, so they are downgraded and legacy decoding stands.
		reply = gateMultiplexerInput(reply)
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
	// Unicode core mode follows the probe by default; an explicit
	// UnicodeCore choice overrides the verdict both ways, the same way
	// an explicit width policy overrides it from the app package.
	switch {
	case b.unicodeCoreOpted == nil:
		b.unicodeCore = b.features.UnicodeCoreMode
	case *b.unicodeCoreOpted:
		b.unicodeCore = true
	default:
		b.unicodeCore = false
	}
	// The parser decodes in the mode that is actually entered, from the
	// very first byte the read loop sees — including the cell-size reply
	// that enterPixel just put in flight.
	b.parser.pixelMouse = b.pixel
	b.parser.win32Input = b.win32
	enter, leave := sequences(b.pixel, b.kitty, b.win32, b.unicodeCore, b.title, b.progress)
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
	// The lifecycle watcher translates job-control signals and console
	// events into terminal suspension, resume, and terminal-loss errors:
	// SIGHUP is terminal loss on Unix, console-close is the same error on
	// Windows, and an external SIGINT/SIGTERM (Unix) or console-close
	// signal (Windows) is the restore-then-error contract — the runtime
	// restores the terminal, the application owns the exit code through
	// the error it receives, and SIGKILL promises nothing. It rides the
	// read context like the resize watcher, so Close cancels it together
	// with the read side, and the suspend and resume paths re-check the
	// backend under the mutex so a Close racing the signals never leaves a
	// half-restored terminal.
	go b.watchLifecycle(readCtx)
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
	if b.suspended {
		// A suspended terminal is showing the shell's screen. Painting a
		// frame there would corrupt it, and committing the frame into the
		// diff baseline would make the terminal remember a state it never
		// showed. The frame is dropped instead — the suspension is short,
		// the user is not looking, and resumeTerminal drops the baseline so
		// the next frame repaints everything anyway.
		b.mu.Unlock()
		return nil
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
	plain, writer, input, state, restoreVT, kitty, win32, pixel, unicodeCore, title, progress := b.plain, b.writer, b.input, b.state, b.restoreVT, b.kitty, b.win32, b.pixel, b.unicodeCore, b.title, b.progress
	b.writer = nil
	b.state = nil
	b.restoreVT = nil
	b.kitty = false
	b.win32 = false
	b.pixel = false
	b.unicodeCore = false
	b.previous = nil
	b.suspended = false
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
			// so this is a plain reset — and the Unicode core reset leads
			// them all: it was the last mode entered, so it leaves first,
			// restoring the terminal's own clustering before the modes that
			// rode underneath it. The title pop closes the leave, and the
			// progress reset follows it — the desktop-integration state is
			// dismissed last. sequences builds this from the same table
			// Open used, which is what guarantees each restore happens
			// wherever the others do.
			_, leave := sequences(pixel, kitty, win32, unicodeCore, title, progress)
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

// makeRaw is the raw-mode entry both Open and the resume path use,
// indirected as a var so the suspend and resume write paths stay testable
// on plain files — a real terminal is not available in a unit-test process,
// and the actual MakeRaw/Restore round trip is pinned by the PTY
// verification lane. Production runs the platform default makeRawTerm:
// Unix term.MakeRaw, Windows the same plus ENABLE_VIRTUAL_TERMINAL_INPUT so
// function keys reach the parser. Tests swap the var; the var is only read
// on paths that run on whatever goroutine called them — Open on the setup
// goroutine, the resume on the run loop's — so the swap is safe while no
// suspend handshake is in flight.
var makeRaw = makeRawTerm

// suspendTerminal hands the terminal back to the shell and latches the
// suspended state. It is the terminal half of job-control suspension: the
// same leave sequences Close writes, from the same table, bounded by the same
// write-slot grace so a wedged in-flight frame can neither deadlock the signal
// watcher nor hold the process stopped. The returned bool reports whether the
// suspended state was entered; a plain or closed backend refuses, and so does
// a double suspend, which keeps a stray second SIGTSTP from re-restoring a
// terminal that is already cooked. Write and restore errors are tolerated
// rather than returned — the process is about to stop regardless, and
// resumeTerminal's full repaint heals whatever a failed leave left behind.
func (b *Backend) suspendTerminal() bool {
	b.mu.Lock()
	if !b.opened || b.plain || b.suspended {
		b.mu.Unlock()
		return false
	}
	b.suspended = true
	writer, input, state := b.writer, b.input, b.state
	pixel, kitty, win32, unicodeCore, title, progress := b.pixel, b.kitty, b.win32, b.unicodeCore, b.title, b.progress
	b.mu.Unlock()
	if writer != nil && b.acquireWriteSlot(restoreGrace) {
		// The re-check under the mutex closes the Close race down to the
		// concurrent window: if the backend closed while the slot was being
		// acquired, Close's own leave has already run or is about to, and a
		// second leave here would pop the kitty and title stacks twice.
		b.mu.Lock()
		stillOpen := b.opened
		b.mu.Unlock()
		if stillOpen {
			_, leave := sequences(pixel, kitty, win32, unicodeCore, title, progress)
			_ = writeAll(writer, []byte(leave))
		}
		<-b.writeSlotChan()
	}
	if state != nil && input != nil {
		// Cooked input is restored before the process stops, so the shell
		// that takes the terminal over sees the termios it left behind —
		// the state Open saved, the same state the eventual Close restores.
		// Restore failures are tolerated: the terminal half is best effort
		// by the same contract as Close, and a stopped process cannot
		// report the error anywhere useful anyway.
		_ = term.Restore(int(input.Fd()), state)
	}
	return true
}

// resumeTerminal takes the terminal back after job control returned it: raw
// mode first, then the same enter sequences Open wrote from the same table,
// then the latch clears and the diff baseline drops so the next frame
// repaints everything. The baseline drops on every exit path — after a
// suspension the terminal has been through the shell's hands and the baseline
// is untrusted — which is what makes a failed leave in suspendTerminal
// recoverable: the first presented frame heals the screen. MakeRaw failing
// leaves the latch set, so Present keeps refusing frames on a terminal whose
// input mode is unknown; the enter write failing clears the latch instead,
// because the full repaint is precisely the recovery for a half-delivered
// enter. The returned error reports either failure, and the watcher routes it
// to the application through the error event.
func (b *Backend) resumeTerminal() error {
	b.mu.Lock()
	if !b.opened || b.plain || !b.suspended {
		b.mu.Unlock()
		return nil
	}
	input, writer := b.input, b.writer
	pixel, kitty, win32, unicodeCore, title, progress := b.pixel, b.kitty, b.win32, b.unicodeCore, b.title, b.progress
	b.mu.Unlock()
	if input == nil {
		b.mu.Lock()
		if b.opened && b.suspended {
			b.previous = nil
		}
		b.mu.Unlock()
		return errors.New("terminal: resume without terminal input")
	}
	// Raw mode returns before the enter sequences are written so the input
	// stream is already in the mode those sequences enable: a keypress
	// landing between the two is decoded in raw, exactly as during Open.
	// The state MakeRaw returns is discarded — b.state keeps the Open-time
	// cooked state the eventual Close restores, so a shell that changed the
	// termios while the job was stopped finds its own state again on exit.
	// The call goes through the makeRaw var: the resume write path has to be
	// testable on plain files, and the real PTY round trip is pinned by the
	// pty verification lane instead of by these unit tests.
	if _, err := makeRaw(int(input.Fd())); err != nil {
		b.mu.Lock()
		if b.opened && b.suspended {
			b.previous = nil
		}
		b.mu.Unlock()
		return err
	}
	var enterErr error
	if writer != nil && b.acquireWriteSlot(restoreGrace) {
		b.mu.Lock()
		stillOpen := b.opened
		b.mu.Unlock()
		if stillOpen {
			enter, _ := sequences(pixel, kitty, win32, unicodeCore, title, progress)
			enterErr = writeAll(writer, []byte(enter))
		}
		<-b.writeSlotChan()
	}
	b.mu.Lock()
	if b.opened && b.suspended {
		b.suspended = false
		b.previous = nil
	}
	b.mu.Unlock()
	return enterErr
}

// deliverLifecycle offers a terminal-loss error — a hangup (Unix) or a
// console-close (Windows) — or a termination error for a trapped
// SIGINT/SIGTERM, to the application without ever blocking the lifecycle
// watcher. A blocked send would wedge the watcher's own exit, and the
// events queue is deep enough that a full buffer means the application is
// busy, not that the notice is load-bearing: the run exits through its
// deferred Close whether or not the error event is seen, and for the
// termination pair the terminal restore has already run before the event
// is offered. The suspend handshake delivers no events from this path —
// its Suspend and Resume events are delivered by the run loop, in order
// around the handshake — so the timestamping by deliverEvent is the only
// shared machinery left.
func (b *Backend) deliverLifecycle(next event.Event) {
	select {
	case b.events <- next:
	default:
	}
}

// ErrInterrupt reports the SIGINT the lifecycle watcher trapped and turned
// into an application exit: the Unix watcher delivers it after the terminal
// restore has run, so a caller mapping it to the conventional exit status
// (130) has nothing left to clean up. Exported only for the application
// package's re-export: the sentinel belongs to the module, not to any
// importer of the internal tree.
var ErrInterrupt = errors.New("terminal: interrupt")

// ErrTerminated reports a trapped SIGTERM under the same contract as
// ErrInterrupt; the conventional exit status is 143. SIGKILL is not part
// of the contract: no runtime cleanup is promised for it.
var ErrTerminated = errors.New("terminal: terminated")

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
