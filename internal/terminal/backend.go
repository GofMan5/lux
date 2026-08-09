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
	escapeDelay   = 35 * time.Millisecond
)

// Backend is a VT-first terminal adapter. Its methods serialize lifecycle and
// output state; one goroutine may block in NextEvent while another presents.
type Backend struct {
	input  *os.File
	output *os.File
	writer *os.File

	mu           sync.Mutex
	writeMu      sync.Mutex
	opened       bool
	plain        bool
	synchronized bool
	features     capability.Set
	state        *term.State
	restoreVT    func() error
	previous     *render.Buffer
	parser       *parser
	events       chan event.Event
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
	return &Backend{input: input, output: output, parser: newParser(), events: make(chan event.Event, 64)}
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
	b.synchronized = syncOutputConfigured()
	b.features = detectCapabilities(b.synchronized)
	b.opened = true
	if err := writeAll(b.writer, []byte(enterTerminal)); err != nil {
		_ = writeAll(b.writer, []byte(leaveTerminal))
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
	b.resize.start(readCtx, b.input, b.events)
	go b.readLoop(readCtx, b.readDone, b.events, b.parser)
	return b.features, nil
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
		select {
		case <-ctx.Done():
			return event.Event{}, ctx.Err()
		case next := <-b.events:
			b.mu.Lock()
			stillOpen := b.opened
			b.mu.Unlock()
			if !stillOpen {
				return event.Event{}, errors.New("terminal: backend is closed")
			}
			if next.Kind == event.Resize {
				b.mu.Lock()
				if b.lastResizeOK && next.Width == b.lastResizeW && next.Height == b.lastResizeH {
					b.mu.Unlock()
					continue
				}
				b.lastResizeW, b.lastResizeH = next.Width, next.Height
				b.lastResizeOK = true
				b.mu.Unlock()
			}
			if next.At.IsZero() {
				next.At = time.Now()
			}
			if next.Kind == event.Error {
				return next, next.Err
			}
			return next, nil
		}
	}
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
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	b.mu.Lock()
	if !b.opened {
		b.mu.Unlock()
		return errors.New("terminal: backend is not open")
	}
	if b.writer == nil {
		b.mu.Unlock()
		return errors.New("terminal: nil output")
	}
	output, plain, synchronized, features, previous := b.writer, b.plain, b.synchronized, b.features, b.previous
	b.mu.Unlock()
	var data []byte
	if plain {
		data = appendPlain(nil, frame)
	} else {
		data = appendFrame(nil, frame, previous, features.Color, synchronized)
	}
	if len(data) == 0 {
		return nil
	}
	if err := writeAllContext(ctx, output, data); err != nil {
		return err
	}
	b.mu.Lock()
	if b.opened {
		b.previous = frame.Clone()
	}
	b.mu.Unlock()
	return nil
}

// Size returns the current terminal size. Redirected streams do not have one.
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
	plain, writer, input, state, restoreVT := b.plain, b.writer, b.input, b.state, b.restoreVT
	b.writer = nil
	b.state = nil
	b.restoreVT = nil
	b.previous = nil
	b.lastResizeOK = false
	b.mu.Unlock()

	if cancel != nil {
		cancel()
		cancelRead(input)
	}
	var result error
	if writer != nil {
		if !plain {
			result = errors.Join(result, writeAll(writer, []byte(leaveTerminal)))
		}
		result = errors.Join(result, writer.Close())
	}
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
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

func (b *Backend) readLoop(ctx context.Context, done chan struct{}, events chan<- event.Event, parser *parser) {
	defer func() {
		close(done)
		if ctx.Err() == nil {
			b.restoreInput()
		}
	}()
	buffer := make([]byte, 4096)
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
	color := capability.ANSI16
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") {
		color = capability.TrueColor
	} else if strings.Contains(terminal, "256color") {
		color = capability.ANSI256
	}
	if os.Getenv("NO_COLOR") != "" {
		color = capability.Monochrome
	}
	return capability.Set{
		Terminal:           terminal,
		Interactive:        true,
		Unicode:            true,
		Color:              color,
		AlternateScreen:    true,
		CursorAddressing:   true,
		Mouse:              true,
		Focus:              true,
		BracketedPaste:     true,
		SynchronizedOutput: syncOutput,
		Graphics:           capability.GraphicsCells,
	}
}

func syncOutputConfigured() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("LUX_TUI_SYNC_OUTPUT")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
