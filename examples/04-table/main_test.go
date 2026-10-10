package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// hangTimeout bounds a test that would otherwise wait forever; a loaded
// machine should never turn a stall into a false failure. A starved shared
// CI runner can freeze the process for tens of seconds while sibling
// package binaries run, so the guard absorbs runner starvation and still
// fails a real hang long before go test's package timeout.
const hangTimeout = 45 * time.Second

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

// pressAt builds a routed-style press with region-local coordinates, the
// shape ui.Context.Handle produces for real mouse events.
func pressAt(width, height, x, y int) ui.Action {
	return ui.Action{
		Kind:   ui.Scroll,
		Bounds: render.Rect{W: width, H: height},
		X:      x,
		Y:      y,
		Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			Action: event.MousePress,
			Button: event.MouseLeft,
		}},
	}
}

func TestTableSortsByHeaderAndView(t *testing.T) {
	app := newTabledemo(50)

	// Keyboard navigation reaches the second row through the routed shape.
	app.applyTable(ui.Action{
		Kind:   ui.Scroll,
		Bounds: render.Rect{W: 60, H: 10},
		Event:  event.KeyPress(event.KeyDown, 0),
	})
	if app.table.Selected != 1 {
		t.Fatalf("selected = %d; want 1", app.table.Selected)
	}

	// A header click on NAME (fixed ID is 7 wide, gutter at x 7, NAME at
	// x 8) requests an ascending lexical sort of the index view. The demo
	// serves the request, so SortRequested is already cleared.
	app.applyTable(pressAt(60, 10, 8, 0))
	if !app.table.SortActive || app.table.SortColumn != 1 || app.table.SortDesc || app.table.SortRequested {
		t.Fatalf("sort state: %#v", app.table)
	}
	first, last := app.rows[app.order[0]], app.rows[app.order[len(app.order)-1]]
	if first.name > last.name {
		t.Fatalf("ascending sort broken: %q vs %q", first.name, last.name)
	}
	// A second click on the same header flips to descending.
	app.table.SortRequested = false
	app.applyTable(pressAt(60, 10, 8, 0))
	if !app.table.SortDesc {
		t.Fatalf("second click did not flip direction: %#v", app.table)
	}
	first, last = app.rows[app.order[0]], app.rows[app.order[len(app.order)-1]]
	if first.name < last.name {
		t.Fatalf("descending sort broken: %q vs %q", first.name, last.name)
	}

	// The CPU column sorts numerically, not lexically: with width 60 the
	// CPU% header starts at x 48.
	app.table = ui.TableState{}
	app.applyTable(pressAt(60, 10, 48, 0))
	if !app.table.SortActive || app.table.SortColumn != 3 {
		t.Fatalf("cpu sort state: %#v", app.table)
	}
	first, last = app.rows[app.order[0]], app.rows[app.order[len(app.order)-1]]
	if first.cpu > last.cpu {
		t.Fatalf("numeric cpu sort broken: %v vs %v", first.cpu, last.cpu)
	}

	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	painted := frame.Canvas.Buffer()
	if !strings.Contains(rowText(painted, 0), "LUX TABLE") {
		t.Fatalf("title missing: %q", rowText(painted, 0))
	}
	if !strings.Contains(rowText(painted, 1), "NAME") {
		t.Fatalf("header missing: %q", rowText(painted, 1))
	}
	// The CPU column is sorted ascending, so the first body row is the
	// lowest observed CPU value (a multiple of 0.1 under 10.0).
	if !strings.Contains(rowText(painted, 2), "running") && !strings.Contains(rowText(painted, 2), "sleep") && !strings.Contains(rowText(painted, 2), "idle") && !strings.Contains(rowText(painted, 2), "stopped") {
		t.Fatalf("body row missing state: %q", rowText(painted, 2))
	}
	foundStatus, foundFooter := false, false
	for y := 10; y < 16; y++ {
		row := rowText(painted, y)
		foundStatus = foundStatus || strings.Contains(row, "50 rows")
		foundFooter = foundFooter || strings.Contains(row, "esc quit")
	}
	if !foundStatus || !foundFooter {
		t.Fatalf("status=%v footer=%v", foundStatus, foundFooter)
	}
	if !strings.Contains(rowText(painted, 14), "CPU% asc") {
		t.Fatalf("sort status missing: %q", rowText(painted, 14))
	}
}

func TestTableTinyTerminalGuards(t *testing.T) {
	app := newTabledemo(10)
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

func TestTableEndToEndKeysClickAndQuit(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newTabledemo(50)
	app.observe = make(chan tabledemoState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.rows == 50 })

	// The focused table takes the arrow keys.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.selected == 2 })

	// A header click on NAME (absolute x 10 = body x 8) sorts ascending.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 10, Y: 1}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.sortActive && state.sortColumn == 1 && !state.sortDesc })

	// The same header flips to descending.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 10, Y: 1}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.sortDesc })

	// A body click selects the row under the pointer.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 12, Y: 3}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.selected == 1 })

	// The priority Escape shortcut quits even while the table holds focus.
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

func waitStateWhere(t *testing.T, states <-chan tabledemoState, done <-chan error, match func(tabledemoState) bool) tabledemoState {
	t.Helper()
	deadline := time.After(hangTimeout)
	for {
		select {
		case state := <-states:
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			t.Fatalf("matching state timeout; goroutine dump follows\n%s", stack)
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
