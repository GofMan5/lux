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
	settle(t, backend)

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
	settle(t, backend)

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
	settle(t, backend)

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
	settle(t, backend)
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

// TestCatalogIdlePagesGoQuiet pins the idle contract for the three static
// pages: the table, form, and tree pages have nothing that moves, so once
// the open sequence and the page switch settle, the catalog must not ask
// for a single frame between input — a parked terminal presents nothing
// instead of repainting identical bytes at the frame cap.
func TestCatalogIdlePagesGoQuiet(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newCatalog()
	app.observe = make(chan catalogState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 && state.tab == 0 })
	settle(t, backend)
	// Warm-up absorbs the open-sequence repaints; then the idle window
	// must be silent. Two frames, not zero, keep the assertion honest
	// about a deadline straggler without licensing a stream.
	countFrames(backend.frames, 300*time.Millisecond)
	if frames := countFrames(backend.frames, 400*time.Millisecond); frames > 2 {
		t.Fatalf("static table page kept presenting: %d frames in 400ms of idle", frames)
	} else {
		t.Logf("table page idle: %d frames in 400ms", frames)
	}

	// The form tab (slot 2 spans panel-local X 7-12, absolute X 48): one
	// repaint for the page switch, then quiet again.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 48, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 1 })
	countFrames(backend.frames, 200*time.Millisecond)
	if frames := countFrames(backend.frames, 400*time.Millisecond); frames > 2 {
		t.Fatalf("static form page kept presenting: %d frames in 400ms of idle", frames)
	} else {
		t.Logf("form page idle: %d frames in 400ms", frames)
	}

	// The strip holds focus after the click, so the arrows page through
	// motion (2) into tree (3): leaving the live page must stop the
	// animation with it, and tree is static again.
	backend.events <- event.KeyPress(event.KeyRight, 0)
	backend.events <- event.KeyPress(event.KeyRight, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 3 })
	countFrames(backend.frames, 300*time.Millisecond)
	if frames := countFrames(backend.frames, 400*time.Millisecond); frames > 2 {
		t.Fatalf("static tree page kept presenting: %d frames in 400ms of idle", frames)
	} else {
		t.Logf("tree page idle: %d frames in 400ms", frames)
	}

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

// TestMotionPageWaitsForGlyphDeadlines pins the frame-request contract on
// the motion page: the spinner flips its glyph every 80ms and the sparkline
// gains a point every 100ms, so View must ask for the next frame exactly at
// the next visible change — the glyph deadline — never at the pacer cap.
// Before the fix the View tail requested a frame unconditionally, so the
// catalog composed at ~240fps here (the cap this test sets) to animate an
// 80ms glyph. The band stays honest on a starved CI runner: a late frame
// still paints the correct absolute-phase glyph and asks for the NEXT flip,
// so overload lowers the count without ever raising it toward the cap.
func TestMotionPageWaitsForGlyphDeadlines(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newCatalog()
	app.observe = make(chan catalogState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 && state.tab == 0 })
	settle(t, backend)
	// The motion tab is the strip's slot 3: panel-local X 14-19, so
	// absolute X 55 lands inside it with the strip at X 40.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 55, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.tab == 2 })
	// Warm-up: the page-switch repaint and the first glyph deadline land.
	countFrames(backend.frames, 250*time.Millisecond)
	// 1.6s at glyph deadlines is ~20 frames/s (12.5 spinner flips plus 10
	// sparkline points, minus the overlaps); at the 240fps cap it would
	// be ~240 frames/s.
	frames := countFrames(backend.frames, 1600*time.Millisecond)
	t.Logf("motion page: %d frames in 1.6s (~%d frames/s)", frames, frames*10/16)
	if frames > 60 {
		t.Fatalf("motion page composed %d frames in 1.6s; the glyph deadlines ask for ~32", frames)
	}
	if frames < 12 {
		t.Fatalf("motion page composed only %d frames in 1.6s; the spinner glyph must keep flipping every 80ms", frames)
	}

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
	// The keystroke is queued before Run starts, and the proof that it
	// was routed is the model's own key counter, never clock timing.
	// CI run 38099373704 (commit 1444a2d) failed this test the other
	// way: the key was sent after the first wait, and a
	// scheduler-starved run loop could reach its select with the frame
	// timer already due, lose the random pick to the timer, render two
	// more Views, and leave the key queued past close(release) — so the
	// "dropped" keystroke was routed against the committed table and
	// moved the selection. Now the test cannot open the gate until a
	// report shows Update has seen exactly the pre-queued key, and the
	// writer stays parked in its gated first Present until then: the
	// routing happened against the empty snapshot, which drops the key,
	// no matter how the runner schedules goroutines.
	backend.events <- event.KeyPress(event.KeyDown, 0)
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

	// The decisive wait, straight away: a report must show the key reached
	// Update (keys == 1) while the selection has not moved. Reports arrive
	// from inside View, before the writer — parked inside its gated first
	// Present — has run any commit callback at all, and Run's first select
	// may dequeue the key either before or after the initial compose, so
	// the keys == 1 report is the first or the second one; either way it
	// is the earliest state the gate can ever let the test observe. The
	// gate is still closed, so the key was routed against the empty
	// snapshot and dropped. Waiting on the counter, not on the clock, is
	// what makes the drop provable: the test cannot proceed past this
	// point until the model itself says the keystroke was consumed.
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 && state.keys == 1 })

	// One more report settles while gated — the writer still has not
	// committed anything, and the dropped key stays dropped. Static pages
	// no longer self-animate (that is the idle contract now), so the
	// report is forced the same way settle forces its proof frame: an
	// inert F5 keys the run loop into exactly one more View without
	// touching the guarded model state; keys == 2 identifies the report
	// that View produced.
	backend.events <- event.KeyPress(event.KeyF5, 0)
	waitStateWhere(t, app.observe, done, func(state catalogState) bool { return state.selected == 0 && state.keys == 2 })

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
// runs immediately after a successful Present, and the writer presents in
// FIFO order with the commits between presents, so the arrival of a frame
// forced after a drain proves every earlier commit already ran. The catalog
// no longer self-animates on static pages, so the proof frame is forced
// with an inert keypress: F5 routes through the committed snapshot, no
// widget consumes it, and the run loop repaints once. It returns the proof
// frame.
func settle(t *testing.T, backend *interactiveBackend) *render.Buffer {
	t.Helper()
	for {
		select {
		case <-backend.frames:
		default:
			backend.events <- event.KeyPress(event.KeyF5, 0)
			return waitFrame(t, backend.frames)
		}
	}
}

// countFrames drains presented frames for the window and reports how many
// arrived: an app that keeps requesting composes at its frame cap, an app
// on glyph deadlines composes at its content's rate, and a settled app
// presents nothing between input.
func countFrames(frames <-chan *render.Buffer, window time.Duration) int {
	deadline := time.After(window)
	count := 0
	for {
		select {
		case <-frames:
			count++
		case <-deadline:
			return count
		}
	}
}

// TestWidgetsCatalogPaletteFlowsThroughTheme pins the stage-3 recipe on the
// second real screen. newCatalog installs the palette once through
// Context.UseTheme; every selection widget (table, tabs, checkbox, radio,
// select, tree) resolves through zero styles, while the glyph widgets
// (gauge, spinner, sparkline) keep their explicit styles — their colors are
// data, not selection. The first paint pins the roles page 0 and the form
// page actually show; the swap repaint proves the resolution is live. Two
// visible changes are pinned on purpose: unselected tabs demote to the Muted
// role (structure) now that the selected tab owns the Accent block, and the
// checkbox marker resolves Accent — the marker IS the state.
func TestWidgetsCatalogPaletteFlowsThroughTheme(t *testing.T) {
	app := newCatalog()
	draw := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	// Page 0: the table carries the same colors the explicit style used
	// to, resolved through the roles now.
	painted := draw()
	if got := painted.Cell(2, 2).Style; got.Foreground != background || got.Background != cyan {
		t.Fatalf("selected table row must resolve the Focus role: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(2, 3).Style; got.Foreground != ink {
		t.Fatalf("table body must resolve the Text role: fg=%v", got.Foreground)
	}
	if got := painted.Cell(2, 1).Style; got.Foreground != muted || got.Attrs&render.AttrUnderline == 0 {
		t.Fatalf("table header must resolve the Muted role underlined: fg=%v attrs=%v", got.Foreground, got.Attrs)
	}
	if got := painted.Cell(40, 2).Style; got.Foreground != background || got.Background != violet {
		t.Fatalf("selected tab must resolve the Accent role: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(47, 2).Style; got.Foreground != muted {
		t.Fatalf("unselected tab must resolve the Muted role: fg=%v", got.Foreground)
	}

	// Form page: the input widgets resolve their rows through the roles.
	app.pages.Selected = 1
	painted = draw()
	if got := painted.Cell(40, 3).Style; got.Foreground != background || got.Background != violet {
		t.Fatalf("checkbox marker must resolve the Accent role: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(40, 5).Style; got.Foreground != background || got.Background != violet {
		t.Fatalf("selected radio row must resolve the Accent role: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(40, 10).Style; got.Foreground != ink {
		t.Fatalf("closed select must resolve the Text role: fg=%v", got.Foreground)
	}

	// Swap the palette with zero Focus — the zero-role opt-out. Every
	// selection surface restyles live; nothing but the theme changed.
	red := render.RGBColor(248, 113, 113)
	app.ui.UseTheme(ui.Theme{
		Text:   render.Style{Foreground: muted},
		Muted:  render.Style{Foreground: cyan},
		Accent: render.Style{Foreground: background, Background: red},
	})
	painted = draw()
	if got := painted.Cell(40, 3).Style; got.Background != red {
		t.Fatalf("checkbox marker must restyle through the swapped Accent role: bg=%v", got.Background)
	}
	if got := painted.Cell(40, 5).Style; got.Background != red {
		t.Fatalf("selected radio row must restyle through the swapped Accent role: bg=%v", got.Background)
	}
	if got := painted.Cell(40, 10).Style; got.Foreground != muted {
		t.Fatalf("closed select must restyle through the swapped Text role: fg=%v", got.Foreground)
	}

	// Page 0 again: the focused selected row falls back to the swapped
	// Accent role through the zero Focus opt-out, and the tabs restyle.
	app.pages.Selected = 0
	painted = draw()
	if got := painted.Cell(2, 2).Style; got.Background != red {
		t.Fatalf("zero Focus role must fall back to Accent for the selected row: bg=%v", got.Background)
	}
	if got := painted.Cell(2, 3).Style; got.Foreground != muted {
		t.Fatalf("table body must restyle through the swapped Text role: fg=%v", got.Foreground)
	}
	if got := painted.Cell(47, 2).Style; got.Foreground != cyan {
		t.Fatalf("unselected tab must restyle through the swapped Muted role: fg=%v", got.Foreground)
	}
	if got := painted.Cell(2, 1).Style; got.Foreground != cyan || got.Attrs&render.AttrUnderline == 0 {
		t.Fatalf("table header must restyle through the swapped Muted role: fg=%v attrs=%v", got.Foreground, got.Attrs)
	}

	// Motion page: the glyph widgets keep their explicit styles — the
	// gauge fill, spinner glyph, and sparkline wave carry data colors,
	// not selection colors, so the theme swap must not reach them.
	app.pages.Selected = 2
	painted = draw()
	if got := painted.Cell(40, 4).Style; got.Foreground != violet || got.Background != background {
		t.Fatalf("gauge fill must keep its explicit style: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(40, 6).Style; got.Foreground != cyan {
		t.Fatalf("spinner glyph must keep its explicit style: fg=%v", got.Foreground)
	}
	spark := false
	for x := 40; x < 62; x++ {
		for _, y := range []int{8, 9} {
			if c := painted.Cell(x, y); c.Grapheme != " " && c.Style.Foreground == cyan {
				spark = true
			}
		}
	}
	if !spark {
		t.Fatal("sparkline wave must keep its explicit cyan style")
	}
}
