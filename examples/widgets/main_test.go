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
	// The same row carries the tabs strip on the right: each label renders
	// in its slot, so the strip reads " table  form  motion ".
	tabs := rowText(painted, 2)
	if !strings.Contains(tabs, " table ") || !strings.Contains(tabs, " form ") || !strings.Contains(tabs, " motion ") {
		t.Fatalf("tabs strip missing labels: %q", tabs)
	}
	// Row 12 is the display-only pair: the gauge tracks the row-zero
	// selection (1 of 8 over 20 cells = 2.5 cells, so 2 full, 1 half), the
	// spinner ticks on the wall clock, and the label explains both.
	instrumented := rowText(painted, 12)
	if !strings.Contains(instrumented, "██▌") {
		t.Fatalf("gauge fill missing: %q", instrumented)
	}
	if !strings.Contains(instrumented, strings.Repeat("░", 4)) {
		t.Fatalf("gauge track missing: %q", instrumented)
	}
	if !strings.Contains(instrumented, "syncing") {
		t.Fatalf("spinner label missing: %q", instrumented)
	}
	glyph := painted.Cell(24, 12).Grapheme
	inSet := false
	for _, frame := range ui.SpinnerBraille {
		if glyph == frame {
			inSet = true
		}
	}
	if !inSet {
		t.Fatalf("spinner glyph %q is not a braille frame", glyph)
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

	// The closed select opens on its release, popup over the panel. The
	// state is reported inside View, before Present commits the grown
	// popup region, so settle waits for a frame whose follow-up proves the
	// commit already ran.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 10}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 10}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.open })
	settle(t, backend.frames)

	// Pressing a popup option picks it (row 11 is option index 0 under
	// offset 0); the release commits and closes the dropdown. The popup
	// geometry never moves, so the release lands on the same committed
	// region without another settle.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.choice == 0 })
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return !state.open && state.choice == 0 })

	// The tabs strip picks on press: slot 2 ("form") spans panel-local
	// X 7-12, so absolute X 48 with the strip at X 40 lands inside it.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 48, Y: 2}}
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.tab == 1 })
	// The press moved focus to the strip, so the arrow keys now drive the
	// selection without another click.
	backend.events <- event.KeyPress(event.KeyRight, 0)
	waitStateWhere(t, app.observe, func(state catalogState) bool { return state.tab == 2 })

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

// settle waits until the interaction snapshot of a freshly painted frame is
// committed. ui.Context commits through Frame.OnPresented, which the runtime
// runs immediately after a successful Present, so a following frame proves
// the previous commit already ran. The catalog keeps drawing (the spinner is
// a live clock), so fresh frames always arrive. It returns the last frame it
// observed.
func settle(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	for {
		select {
		case <-frames:
		default:
			waitFrame(t, frames)
			return waitFrame(t, frames)
		}
	}
}
