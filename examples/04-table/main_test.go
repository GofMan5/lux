package main

import (
	"context"
	"fmt"
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
	// The filter row sits between the title and the table header: an empty
	// query shows its placeholder.
	if !strings.Contains(rowText(painted, 1), "filter") {
		t.Fatalf("filter row missing: %q", rowText(painted, 1))
	}
	if !strings.Contains(rowText(painted, 2), "NAME") {
		t.Fatalf("header missing: %q", rowText(painted, 2))
	}
	// The CPU column is sorted ascending, so the first body row is the
	// lowest observed CPU value (a multiple of 0.1 under 10.0).
	if body := rowText(painted, 3); !containsAny(body, "running", "sleep", "idle", "stopped") {
		t.Fatalf("body row missing state: %q", body)
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

// TestTableSelectionSurvivesSort pins the stable row identity contract: the
// cursor's identity is the data row it selected, not the view index it
// happens to occupy. Sorting re-orders the view; the cursor must follow its
// row to that row's new index instead of silently selecting whatever data
// inherits the old index.
func TestTableSelectionSurvivesSort(t *testing.T) {
	app := newTabledemo(50)
	for i := 0; i < 2; i++ {
		app.applyTable(ui.Action{
			Kind:   ui.Scroll,
			Bounds: render.Rect{W: 60, H: 10},
			Event:  event.KeyPress(event.KeyDown, 0),
		})
	}
	if app.table.Selected != 2 {
		t.Fatalf("selected = %d; want 2", app.table.Selected)
	}
	anchor := app.rows[app.order[app.table.Selected]].id

	// Sort by NAME ascending (header x 8 at width 60), then descending,
	// then numeric CPU (x 48): in every order the row under the cursor
	// must stay the same data row.
	app.applyTable(pressAt(60, 10, 8, 0))
	if got := app.rows[app.order[app.table.Selected]].id; got != anchor {
		t.Fatalf("ascending sort moved the cursor: row id %d became %d", anchor, got)
	}
	app.applyTable(pressAt(60, 10, 8, 0))
	if got := app.rows[app.order[app.table.Selected]].id; got != anchor {
		t.Fatalf("descending sort moved the cursor: row id %d became %d", anchor, got)
	}
	app.applyTable(pressAt(60, 10, 48, 0))
	if got := app.rows[app.order[app.table.Selected]].id; got != anchor {
		t.Fatalf("cpu sort moved the cursor: row id %d became %d", anchor, got)
	}
}

// TestTableFilterNarrowsAndWidensView pins the filter as a view over the
// data: only matching rows survive, a narrower query only removes rows,
// backspace widens the view back, and the standing sort survives every
// rebuild.
func TestTableFilterNarrowsAndWidensView(t *testing.T) {
	app := newTabledemo(50)
	app.typeFilter("run")
	if len(app.order) == 0 || len(app.order) == 50 {
		t.Fatalf("query matched %d rows; want strictly between 0 and 50", len(app.order))
	}
	for _, idx := range app.order {
		if !filterMatches(app.rows[idx], "run") {
			t.Fatalf("row %d does not match the query: %+v", idx, app.rows[idx])
		}
	}
	narrowed := len(app.order)

	// One more rune narrows further — every row matching the longer query
	// matches the shorter one, so the view can only shrink.
	app.typeFilter("n")
	if len(app.order) > narrowed {
		t.Fatalf("narrowing grew the view: %d > %d", len(app.order), narrowed)
	}
	for _, idx := range app.order {
		if !filterMatches(app.rows[idx], "runn") {
			t.Fatalf("row %d does not match the narrowed query: %+v", idx, app.rows[idx])
		}
	}

	// Backspace widens the view back one rune at a time.
	app.backspaceFilter()
	if len(app.order) != narrowed {
		t.Fatalf("backspace changed the match count: %d; want %d", len(app.order), narrowed)
	}
	app.setFilter("")
	if len(app.order) != 50 {
		t.Fatalf("clearing the query left %d rows; want 50", len(app.order))
	}

	// The standing sort survives a rebuild: filter after sorting and the
	// narrowed view comes out still ordered.
	app.applyTable(pressAt(60, 10, 8, 0)) // NAME header, ascending
	if !app.table.SortActive {
		t.Fatalf("sort not active: %#v", app.table)
	}
	app.typeFilter("run")
	for i := 1; i < len(app.order); i++ {
		if app.rows[app.order[i-1]].name > app.rows[app.order[i]].name {
			t.Fatalf("filter lost the standing sort at index %d", i)
		}
	}
}

// TestTableSelectionSurvivesFilter pins the anchor contract against the
// filter: a surviving row keeps the cursor wherever it moves, an excluded
// row clamps the cursor and re-anchors onto the row it lands on, and an
// empty view keeps the anchor so widening restores the row.
func TestTableSelectionSurvivesFilter(t *testing.T) {
	app := newTabledemo(50)
	target := -1
	for i, r := range app.rows {
		if filterMatches(r, "run") {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no row matches the query")
	}
	for i := 0; i < target; i++ {
		app.applyTable(keyDownAction())
	}
	anchor := app.rows[app.order[app.table.Selected]].id

	// The anchor survives the query and keeps the cursor.
	app.typeFilter("run")
	if got := app.rows[app.order[app.table.Selected]].id; got != anchor {
		t.Fatalf("filter moved the cursor off its row: id %d became %d", anchor, got)
	}

	// An empty view drops the cursor but keeps the anchor: no row sits
	// under the cursor to promote, and widening must restore the row.
	app.typeFilter("zzz") // "run" + "zzz" matches nothing
	if len(app.order) != 0 {
		t.Fatalf("unexpected matches: %d", len(app.order))
	}
	if app.anchorID != anchor {
		t.Fatalf("empty view dropped the anchor: %d became %d", anchor, app.anchorID)
	}
	app.setFilter("run")
	if got := app.rows[app.order[app.table.Selected]].id; got != anchor {
		t.Fatalf("widened query did not restore the anchored row: id %d became %d", anchor, got)
	}

	// A row the query excludes cannot hold the cursor: it clamps to a
	// valid index and the anchor becomes the row under the cursor.
	app.setFilter("")
	app.table = ui.TableState{}
	excluded := -1
	for i, r := range app.rows {
		if !filterMatches(r, "run") {
			excluded = i
			break
		}
	}
	if excluded < 0 {
		t.Fatal("no row fails the query")
	}
	for i := 0; i < excluded; i++ {
		app.applyTable(keyDownAction())
	}
	app.typeFilter("run")
	if app.table.Selected >= len(app.order) || app.table.Selected < 0 {
		t.Fatalf("cursor out of bounds after exclusion: %d of %d", app.table.Selected, len(app.order))
	}
	if got := app.rows[app.order[app.table.Selected]].id; got != app.anchorID {
		t.Fatalf("anchor did not follow the clamped cursor: %d vs %d", app.anchorID, got)
	}
	if !filterMatches(app.rows[app.order[app.table.Selected]], "run") {
		t.Fatal("cursor sits on a row the query excludes")
	}
}

// TestTableFilterPaintsQueryAndEmptyState pins the paint side: a live query
// shows in the input row, the empty view carries an honest hint, and a
// narrowed view paints matches with a live cursor.
func TestTableFilterPaintsQueryAndEmptyState(t *testing.T) {
	app := newTabledemo(50)
	app.typeFilter("zzz")
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	painted := frame.Canvas.Buffer()
	if !strings.Contains(rowText(painted, 1), "zzz") {
		t.Fatalf("query missing from input row: %q", rowText(painted, 1))
	}
	if !strings.Contains(rowText(painted, 2), "no rows match") {
		t.Fatalf("empty hint missing: %q", rowText(painted, 2))
	}
	if status := rowText(painted, 14); !strings.Contains(status, "0 match") || !strings.Contains(status, "row -") {
		t.Fatalf("empty status missing: %q", status)
	}

	app.setFilter("run")
	frame = &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	painted = frame.Canvas.Buffer()
	if !strings.Contains(rowText(painted, 1), "run") {
		t.Fatalf("query missing from input row: %q", rowText(painted, 1))
	}
	if !strings.Contains(rowText(painted, 2), "NAME") {
		t.Fatalf("header missing: %q", rowText(painted, 2))
	}
	if status := rowText(painted, 14); !strings.Contains(status, "match") || !strings.Contains(status, fmt.Sprintf(" %d ", len(app.order))) {
		t.Fatalf("match status missing: %q", status)
	}
}

// typeFilter feeds runes into the filter exactly as the router delivers
// them — one Input action per rune, each followed by a rebuild — without
// the full run loop.
func (app *tabledemo) typeFilter(text string) {
	for _, r := range text {
		app.filter.Apply(ui.Action{Kind: ui.Input, Event: event.RuneKey(r, 0)})
		app.refilter()
	}
}

// backspaceFilter deletes one rune through the same routed path.
func (app *tabledemo) backspaceFilter() {
	app.filter.Apply(ui.Action{Kind: ui.Input, Event: event.KeyPress(event.KeyBackspace, 0)})
	app.refilter()
}

func keyDownAction() ui.Action {
	return ui.Action{
		Kind:   ui.Scroll,
		Bounds: render.Rect{W: 60, H: 10},
		Event:  event.KeyPress(event.KeyDown, 0),
	}
}

// typeRunes types text through the interactive backend one rune at a time,
// exactly as a terminal would deliver it.
func typeRunes(backend *interactiveBackend, text string) {
	for _, r := range text {
		backend.events <- event.RuneKey(r, 0)
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

	// The state above arrived from inside the first View, before the
	// writer's present callbacks committed the table's focus: ui.Context
	// routes events against the last successfully presented snapshot, so a
	// keystroke that beats the first commit is dropped on an empty focus
	// table — the 2-in-4 CI lottery this suite already lost twice. Unlike
	// the catalog, this demo paints only when an event arrives, so there
	// is no fresh frame to wait for; but every event still dirties the
	// model, so an F5 press (bound nowhere here) forces a repaint, and
	// the writer is sequential: a second frame on the wire proves the
	// first frame's commit ran.
	backend.events <- event.KeyPress(event.KeyF5, 0)
	waitFrame(t, backend.frames)

	// The focused table takes the arrow keys.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.selected == 2 })

	// A header click on NAME (absolute x 10 = body x 8) sorts ascending;
	// the header sits one row lower now that the filter line exists.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 10, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.sortActive && state.sortColumn == 1 && !state.sortDesc })

	// The same header flips to descending.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 10, Y: 2}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.sortDesc })

	// A body click selects the row under the pointer. After the descending
	// sort the cursor followed its row deep into the view, so the click's
	// screen row maps to view row offset+1: the assertion is view-relative,
	// not pinned to a frozen offset.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 12, Y: 4}}
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.selected == state.offset+1 })

	// '/' moves focus to the filter from the table without a Tab cycle.
	backend.events <- event.RuneKey('/', 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.filterFocus })

	// Typing narrows the view incrementally: every rune rebuilds the order.
	typeRunes(backend, "run")
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool {
		return state.filter == "run" && state.matches > 0 && state.matches < 50
	})

	// Enter commits the query and hands focus back to the results.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return !state.filterFocus && state.matches < 50 })

	// '/' re-enters the filter from the results. A query that matches
	// nothing leaves an honest empty view, not a dead blank: the status
	// row and the body both say so.
	backend.events <- event.RuneKey('/', 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.filterFocus })
	typeRunes(backend, "zzz")
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.filter == "runzzz" && state.matches == 0 })
	frame := waitFrameWhere(t, backend.frames, func(frame *render.Buffer) bool {
		status := rowText(frame, 14)
		return strings.Contains(status, "0 match") && strings.Contains(status, "row -")
	})
	if body := rowText(frame, 2); !strings.Contains(body, "no rows match") {
		t.Fatalf("empty body missing hint: %q", body)
	}

	// Escape over a live query clears the filter instead of quitting.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, done, func(state tabledemoState) bool { return state.filter == "" && state.matches == 50 })

	// A bare Escape on an empty query is the advertised quit.
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
	var last tabledemoState
	seen := false
	for {
		select {
		case state := <-states:
			last, seen = state, true
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			detail := "no state observed"
			if seen {
				detail = fmt.Sprintf("last state %+v", last)
			}
			t.Fatalf("matching state timeout (%s); goroutine dump follows\n%s", detail, stack)
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

// waitFrameWhere drains frames until one satisfies check. Events queue one
// frame each, so the frame for a state change sits behind stale frames from
// earlier steps; a plain waitFrame would assert against the past.
func waitFrameWhere(t *testing.T, frames <-chan *render.Buffer, check func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	deadline := time.After(hangTimeout)
	for {
		select {
		case frame := <-frames:
			if check(frame) {
				return frame
			}
		case <-deadline:
			t.Fatal("frame condition never painted")
			return nil
		}
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
