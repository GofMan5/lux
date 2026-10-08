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

func TestCatalogPaintsTableAndStatus(t *testing.T) {
	app := newCatalog()
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	painted := frame.Canvas.Buffer()
	if !strings.Contains(rowText(painted, 0), "LUX WIDGET CATALOG") {
		t.Fatalf("title missing: %q", rowText(painted, 0))
	}
	if !strings.Contains(rowText(painted, 1), "WIDGET") {
		t.Fatalf("header missing: %q", rowText(painted, 1))
	}
	if !strings.Contains(rowText(painted, 2), "Table") {
		t.Fatalf("first row missing: %q", rowText(painted, 2))
	}
	// The status line reports the row-zero selection.
	foundStatus, foundFooter := false, false
	for y := 10; y < 16; y++ {
		row := rowText(painted, y)
		foundStatus = foundStatus || strings.Contains(row, "8 widgets")
		foundFooter = foundFooter || strings.Contains(row, "esc quit")
	}
	if !foundStatus || !foundFooter {
		t.Fatalf("status=%v footer=%v", foundStatus, foundFooter)
	}
}

func TestCatalogTinyTerminalGuards(t *testing.T) {
	app := newCatalog()
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	if painted := frame.Canvas.Buffer(); !strings.Contains(rowText(painted, 0), "enlarge terminal") {
		t.Fatalf("tiny guard missing: %q", rowText(painted, 0))
	}
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

func TestCatalogEndToEndKeysClickAndQuit(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newCatalog()
	app.observe = make(chan catalogState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.selected == 0 })

	// The focused table takes the arrow keys; each keystroke waits for its
	// frame so the next input lands on a settled view.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.selected == 1 })
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.selected == 2 })

	// A body click selects the row under the pointer: the header takes local
	// row 0, so absolute Y 3 is body row 1.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 12, Y: 3}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.selected == 1 })

	// The checkbox toggles on the mouse release: press focuses, release
	// activates.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 3}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 3}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.checked })

	// A radio press picks the row under the pointer: local row 1 is violet.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 6}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.station == 1 })

	// The closed select opens on its release, popup over the panel. The state
	// is reported inside View, before Present commits the grown popup region,
	// so the press that follows also waits for a presented frame.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 10}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 10}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.open })
	waitFrame(t, backend.frames)

	// Pressing a popup option picks it (row 11 is option index 0 under
	// offset 0); the release commits and closes the dropdown. Each step
	// waits for its frame so the next input lands on a committed region.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.choice == 0 })
	waitFrame(t, backend.frames)
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return !state.open && state.choice == 0 })
	waitFrame(t, backend.frames)

	// The priority Escape shortcut quits even while a widget holds focus.
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

func waitStateWhere(t *testing.T, states <-chan catalogState, match func(catalogState) bool) catalogState {
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
			return catalogState{}
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
