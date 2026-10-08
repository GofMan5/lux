package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lux-tui/lux"
	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
)

// hangTimeout bounds a test that would otherwise wait forever; a loaded
// machine should never turn a stall into a false failure.
const hangTimeout = 10 * time.Second

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

func TestTextEditEditsByGraphemeAndView(t *testing.T) {
	app := newTextedit()
	if len(app.editor.Lines) != 3 {
		t.Fatalf("seed lines = %q", app.editor.Lines)
	}
	app.editor.Apply(ui.Action{Kind: ui.Input, Event: event.RuneKey('!', 0)})
	if last := app.editor.Lines[2]; last != "Wrap follows the terminal width; edit anywhere and scroll.!" {
		t.Fatalf("appended line = %q", last)
	}
	// A combining pair appended after the caret must stay one edit unit.
	app.editor.Apply(ui.Action{Kind: ui.Input, Event: event.RuneKey(0x301, 0)})
	if last := app.editor.Lines[2]; last != "Wrap follows the terminal width; edit anywhere and scroll.!"+"\u0301" {
		t.Fatalf("combining append = %q", last)
	}
	if !app.editor.Undo() || !app.editor.Undo() {
		t.Fatalf("undo stack did not cover both edits: %q", app.editor.Lines)
	}
	if last := app.editor.Lines[2]; last != "Wrap follows the terminal width; edit anywhere and scroll." {
		t.Fatalf("undone line = %q", last)
	}

	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	painted := frame.Canvas.Buffer()
	if !strings.Contains(rowText(painted, 0), "LUX TEXTEDIT") {
		t.Fatalf("title missing: %q", rowText(painted, 0))
	}
	foundSeed, foundStatus, foundFooter := false, false, false
	for y := 1; y < 16 && !(foundSeed && foundStatus && foundFooter); y++ {
		row := rowText(painted, y)
		foundSeed = foundSeed || strings.Contains(row, "multiline")
		foundStatus = foundStatus || strings.Contains(row, "Ln 3/3")
		foundFooter = foundFooter || strings.Contains(row, "[esc] quit")
	}
	if !foundSeed || !foundStatus || !foundFooter {
		t.Fatalf("paint seed=%v status=%v footer=%v", foundSeed, foundStatus, foundFooter)
	}
	// The registration fix: the first focusable paints its caret on the very
	// first frame, which a static app otherwise never repaints.
	if x, y, visible := painted.Caret(); !visible || x < 2 || x >= 62 || y < 1 || y >= 13 {
		t.Fatalf("caret missing or outside editor body: x=%d y=%d visible=%v", x, y, visible)
	}
	app.editor.Wrap = render.WrapChar
	app.View(frame)
	for y := 12; y < 15; y++ {
		if strings.Contains(rowText(painted, y), "wrap char") {
			return
		}
	}
	t.Fatalf("wrap mode not reported: %q", rowText(painted, 14))
}

type interactiveBackend struct {
	events chan event.Event
	frames chan *render.Buffer
}

func (b *interactiveBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}
func (b *interactiveBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}
func (b *interactiveBackend) Present(_ context.Context, frame *render.Buffer) error {
	select {
	case b.frames <- frame.Clone():
	default:
	}
	return nil
}
func (b *interactiveBackend) Size() (int, int, error) { return 64, 16, nil }
func (*interactiveBackend) Close() error              { return nil }

func TestTextEditEndToEndInputAndQuit(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newTextedit()
	app.observe = make(chan texteditState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	frame := waitFrame(t, backend.frames)
	// The focused editor owns the terminal caret from the first static frame.
	if x, y, visible := frame.Caret(); visible {
		if x < 2 || x >= 62 || y < 1 || y >= 14 {
			t.Fatalf("caret outside editor body: x=%d y=%d", x, y)
		}
	} else if x, y := waitCaret(t, backend.frames); x < 2 || x >= 62 || y < 1 || y >= 14 {
		t.Fatalf("caret outside editor body: x=%d y=%d", x, y)
	}
	waitStateWhere(t, app.observe, func(state texteditState) bool { return state.lines == 3 })

	// Committed Unicode reaches the focused editor; Enter splits a line.
	backend.events <- event.RuneKey(0x754c, 0)
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, func(state texteditState) bool { return state.lines == 4 })
	// Ctrl+Z undoes the line split as one edit.
	backend.events <- event.RuneKey('z', event.ModCtrl)
	waitStateWhere(t, app.observe, func(state texteditState) bool { return state.lines == 3 })
	// The priority Escape shortcut quits even while the editor holds focus.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
}

func waitStateWhere(t *testing.T, states <-chan texteditState, match func(texteditState) bool) texteditState {
	t.Helper()
	deadline := time.After(hangTimeout)
	for {
		select {
		case state := <-states:
			if match(state) {
				return state
			}
		case <-deadline:
			t.Fatal("matching state timeout")
			return texteditState{}
		}
	}
}

func waitCaret(t *testing.T, frames <-chan *render.Buffer) (int, int) {
	t.Helper()
	deadline := time.After(hangTimeout)
	for {
		select {
		case frame := <-frames:
			if x, y, visible := frame.Caret(); visible {
				return x, y
			}
		case <-deadline:
			t.Fatal("caret never became visible")
			return -1, -1
		}
	}
}

func waitFrame(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		t.Fatal("frame timeout")
		return nil
	}
}
