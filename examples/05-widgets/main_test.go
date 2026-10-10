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

// TestCatalogPaintsTableAndStatus drives the controls panel through all
// four pages with the same catalog state and asserts what each page paints:
// the table page explains the catalog, the form page holds the input
// widgets, the motion page the live display widgets, and the tree page the
// file tree.
func TestCatalogPaintsTableAndStatus(t *testing.T) {
	app := newCatalog()
	draw := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	// Page 0 (table): the inventory table and the static explanation.
	painted := draw()
	if !strings.Contains(rowText(painted, 0), "LUX WIDGET CATALOG") {
		t.Fatalf("title missing: %q", rowText(painted, 0))
	}
	if !strings.Contains(rowText(painted, 1), "WIDGET") {
		t.Fatalf("header missing: %q", rowText(painted, 1))
	}
	if !strings.Contains(rowText(painted, 2), "Table") {
		t.Fatalf("first row missing: %q", rowText(painted, 2))
	}
	// The tabs strip pages the controls panel. On page 0 the strip holds
	// table, form, and motion; the fourth tab is clipped until selected.
	tabs := rowText(painted, 2)
	if !strings.Contains(tabs, " table ") || !strings.Contains(tabs, " form ") || !strings.Contains(tabs, " motion ") {
		t.Fatalf("tabs strip missing labels: %q", tabs)
	}
	if strings.Contains(tabs, " tree ") {
		t.Fatalf("tree tab should be clipped on page 0: %q", tabs)
	}
	// The explanation replaces the form widgets on this page.
	if !strings.Contains(rowText(painted, 5), "catalog itself") {
		t.Fatalf("table page explanation missing: %q", rowText(painted, 5))
	}
	// The status line reports the row-zero selection and the page.
	foundStatus, foundFooter := false, false
	for y := 10; y < 16; y++ {
		row := rowText(painted, y)
		foundStatus = foundStatus || strings.Contains(row, "8 widgets")
		foundFooter = foundFooter || strings.Contains(row, "esc quit")
	}
	if !foundStatus || !foundFooter {
		t.Fatalf("status=%v footer=%v", foundStatus, foundFooter)
	}

	// Page 2 (motion): the gauge tracks the row-zero selection (1 of 8
	// over 20 cells = 2.5 cells, so 2 full, 1 half), the spinner ticks on
	// the wall clock, and the seeded sparkline paints a full wave.
	app.pages.Selected = 2
	painted = draw()
	instrumented := rowText(painted, 4)
	if !strings.Contains(instrumented, "██▌") {
		t.Fatalf("gauge fill missing: %q", instrumented)
	}
	if !strings.Contains(instrumented, strings.Repeat("░", 4)) {
		t.Fatalf("gauge track missing: %q", instrumented)
	}
	spinnerRow := rowText(painted, 6)
	if !strings.Contains(spinnerRow, "syncing") {
		t.Fatalf("spinner label missing: %q", spinnerRow)
	}
	glyph := painted.Cell(40, 6).Grapheme
	inSet := false
	for _, frame := range ui.SpinnerBraille {
		if glyph == frame {
			inSet = true
		}
	}
	if !inSet {
		t.Fatalf("spinner glyph %q is not a braille frame", glyph)
	}
	if !strings.ContainsAny(rowText(painted, 9), "▁▂▃▄▅▆▇█") {
		t.Fatalf("sparkline glyphs missing: %q", rowText(painted, 9))
	}
	if !strings.ContainsAny(rowText(painted, 8), "▁▂▃▄▅▆▇█") {
		t.Fatalf("sparkline cap row glyphs missing: %q", rowText(painted, 8))
	}

	// Page 3 (tree): the collapsed file tree with ASCII branch markers.
	app.pages.Selected = 3
	painted = draw()
	treeRows := rowText(painted, 3)
	if !strings.Contains(treeRows, "+ ui") || !strings.Contains(rowText(painted, 4), "+ examples") {
		t.Fatalf("collapsed tree missing: %q / %q", treeRows, rowText(painted, 4))
	}
	// Selecting the tree page scrolls the fourth tab into the strip.
	if !strings.Contains(rowText(painted, 2), " tree ") {
		t.Fatalf("tree tab not visible when selected: %q", rowText(painted, 2))
	}
	// Expanding a node via the widget's own Apply grows the painted rows:
	// the same press contract the e2e test exercises end to end.
	rows := app.treeRows()
	app.tree.Apply(ui.Action{Kind: ui.Activate, Event: event.KeyPress(event.KeyEnter, 0)}, len(rows), func(index int) ui.TreeRow { return rows[index] })
	app.tree.Selected = 1
	painted = draw()
	if !strings.Contains(rowText(painted, 3), "- ui") || !strings.Contains(rowText(painted, 4), "table.go") || !strings.Contains(rowText(painted, 6), "sparkline.go") {
		t.Fatalf("expanded tree missing: %q / %q / %q", rowText(painted, 3), rowText(painted, 4), rowText(painted, 6))
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
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 })
	// The state above arrived from inside the first View, before the
	// writer goroutine ran the present callbacks that commit the table's
	// focus: ui.Context routes events against the last successfully
	// presented snapshot, so a keystroke that beats the first commit
	// finds an empty focus table and is dropped silently. Settling before
	// the first input — not only before region changes — is what keeps
	// this suite off the 2-vCPU CI lottery; TestKeystrokeBeforeFirstCommitIsDropped
	// pins the contract deterministically.
	settle(t, backend.frames)

	// The focused table takes the arrow keys; each keystroke waits for its
	// frame so the next input lands on a settled view.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 1 })
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 2 })

	// A body click selects the row under the pointer: the header takes local
	// row 0, so absolute Y 3 is body row 1.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 12, Y: 3}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 1 })

	// The form widgets live on the tabs' second page: slot 2 ("form")
	// spans panel-local X 7-12, so absolute X 48 with the strip at X 40
	// lands inside it. The page switch changes the panel's regions, so
	// settle proves the form controls committed before they are clicked.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 48, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 1 })
	settle(t, backend.frames)

	// The checkbox toggles on the mouse release: press focuses, release
	// activates.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 3}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 3}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.checked })

	// A radio press picks the row under the pointer: local row 1 is violet.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 6}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.station == 1 })

	// The closed select opens on its release, popup over the panel. The
	// state is reported inside View, before Present commits the grown
	// popup region, so settle waits for a frame whose follow-up proves the
	// commit already ran.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 10}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 10}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.open })
	settle(t, backend.frames)

	// Pressing a popup option picks it (row 11 is option index 0 under
	// offset 0); the release commits and closes the dropdown. The popup
	// geometry never moves, so the release lands on the same committed
	// region without another settle.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.choice == 0 })
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 42, Y: 11}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return !state.open && state.choice == 0 })

	// The select kept focus through its own activation, so the strip takes
	// a press to hand focus back before the arrow keys page again. Pressing
	// the already-selected form tab keeps the page and moves focus.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 48, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 1 })
	backend.events <- event.KeyPress(event.KeyRight, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 2 })
	backend.events <- event.KeyPress(event.KeyRight, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 3 })

	// The tree page registers its own region geometry, so settle proves the
	// tree committed before the press. A press on panel row 1 selects the
	// second visible row ("examples", Y 4 with the tree at Y 3).
	settle(t, backend.frames)
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 42, Y: 4}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.treeSel == 1 })

	// The press focused the tree, so Enter activates the selected row:
	// "examples" has children, so its expansion set entry flips and the
	// next view flattens one more level.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.treeSel == 1 && state.treeOpen == 1 })

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

// gatedBackend delays every Present until released. Holding the writer
// inside its first Present turns the scheduler lottery the CI flakes rode
// into a deterministic interleaving: a keystroke provably reaches the run
// loop while no snapshot has been committed yet.
type gatedBackend struct {
	interactiveBackend
	release chan struct{}
}

func (b *gatedBackend) Present(ctx context.Context, frame *render.Buffer) error {
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.interactiveBackend.Present(ctx, frame)
}

// TestKeystrokeBeforeFirstCommitIsDropped pins the routing contract the E2E
// suites rely on when they settle before the first input: ui.Context routes
// a terminal event against the last successfully presented snapshot, not
// against the View that is mid-flight. A keystroke that beats the first
// present's commit callbacks finds an empty focus table and is dropped —
// silently and forever — instead of being queued. That drop is what the
// 2-in-4 CI failures were (runs #26 and #32 and the scheduled stress lane
// all lost this lottery): the suites observed the initial state from inside
// the first View and typed immediately.
func TestKeystrokeBeforeFirstCommitIsDropped(t *testing.T) {
	release := make(chan struct{})
	backend := &gatedBackend{interactiveBackend: interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}, release: release}
	app := newCatalog()
	app.observe = make(chan catalogState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	released := false
	defer func() {
		// Even after a hard failure the gate must open before Escape: the
		// writer is parked inside its gated Present, and the quit key is
		// useless until it can proceed (Run's teardown would unblock it
		// through ctx.Done anyway).
		if !released {
			close(release)
		}
		backend.events <- event.KeyPress(event.KeyEscape, 0)
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(hangTimeout):
			t.Error("catalog did not quit after the gate opened")
		}
	}()

	// View #1 reports the initial state from inside the View — before the
	// writer, parked inside its gated first Present, has run any commit
	// callback at all.
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 })
	backend.events <- event.KeyPress(event.KeyDown, 0)

	// Views #2 and #3 prove the keystroke was consumed while nothing was
	// committed: after View #1 the run loop arms the frame timer and
	// selects, the events channel is the only ready case, and the loop
	// renders two more Views from its remaining surfaces while the writer
	// stays gated.
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 })
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 })

	// Open the gate: the first Present finishes, its callbacks commit the
	// table's focus, and repaints resume. The keystroke was routed against
	// the empty snapshot and dropped — not queued — so the selection must
	// stay put through the drain.
	close(release)
	released = true
	deadline := time.After(300 * time.Millisecond)
drain:
	for {
		select {
		case state := <-app.observe:
			if state.selected != 0 {
				t.Fatalf("the pre-commit keystroke was delivered late: selection moved to %d", state.selected)
			}
		case err := <-done:
			t.Fatalf("run ended during the drain: %v", err)
		case <-deadline:
			break drain
		}
	}

	// After the commit a keystroke routes to the focused table again.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 1 })
}

func waitStateWhere(t *testing.T, states <-chan catalogState, done <-chan error, match func(catalogState) bool) catalogState {
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
