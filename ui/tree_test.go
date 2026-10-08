package ui

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// benchTreeRows is a flat sample of the file tree the widget tests use: two
// expandable roots whose children appear only when the state says so.
func treeRows(state *TreeState) []TreeRow {
	rows := []TreeRow{
		{ID: "ui", Label: "ui", HasChildren: true},
	}
	if state.IsExpanded("ui") {
		rows = append(rows,
			TreeRow{ID: "ui/table.go", Label: "table.go", Depth: 1},
			TreeRow{ID: "ui/tree.go", Label: "tree.go", Depth: 1},
		)
	}
	rows = append(rows, TreeRow{ID: "examples", Label: "examples", HasChildren: true})
	if state.IsExpanded("examples") {
		rows = append(rows,
			TreeRow{ID: "examples/widgets", Label: "widgets", Depth: 1, HasChildren: true},
		)
		if state.IsExpanded("examples/widgets") {
			rows = append(rows,
				TreeRow{ID: "examples/widgets/main.go", Label: "main.go", Depth: 2},
			)
		}
	}
	return rows
}

func rowAt(state *TreeState) func(index int) TreeRow {
	return func(index int) TreeRow { return treeRows(state)[index] }
}

func TestTreeIsExpandedZeroValue(t *testing.T) {
	state := TreeState{}
	if state.IsExpanded("ui") {
		t.Fatalf("zero-value tree reports a node expanded")
	}
	var nilState *TreeState
	if nilState.IsExpanded("ui") {
		t.Fatalf("nil receiver reports a node expanded")
	}
}

func TestTreeApplyMovesSelectionLikeAList(t *testing.T) {
	state := TreeState{}
	rows := treeRows(&state) // collapsed: ui, examples
	count := len(rows)
	down := checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0)
	down.Bounds = render.Rect{W: 10, H: 10}
	if changed, activated := state.Apply(down, count, rowAt(&state)); !changed || activated {
		t.Fatalf("down changed=%v activated=%v; want true false", changed, activated)
	}
	if state.Selected != 1 {
		t.Fatalf("selected = %d; want 1", state.Selected)
	}
	up := checkAction(Scroll, event.KeyPress(event.KeyUp, 0), 0, 0)
	up.Bounds = render.Rect{W: 10, H: 10}
	state.Apply(up, count, rowAt(&state))
	if state.Selected != 0 {
		t.Fatalf("selected after up = %d; want 0", state.Selected)
	}
	// Home and End jump to the extremes.
	state.Selected = 1
	end := checkAction(Scroll, event.KeyPress(event.KeyEnd, 0), 0, 0)
	end.Bounds = render.Rect{W: 10, H: 10}
	state.Apply(end, count, rowAt(&state))
	if state.Selected != count-1 {
		t.Fatalf("selected after end = %d; want %d", state.Selected, count-1)
	}
	// Page keys move by the visible row count.
	pageUp := checkAction(Scroll, event.KeyPress(event.KeyPageUp, 0), 0, 0)
	pageUp.Bounds = render.Rect{W: 10, H: 2}
	state.Apply(pageUp, count, rowAt(&state))
	if state.Selected != 0 {
		t.Fatalf("selected after page up = %d; want 0", state.Selected)
	}
}

func TestTreeApplyWheelMovesSelection(t *testing.T) {
	state := TreeState{}
	count := len(treeRows(&state))
	wheel := checkAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MouseWheel, Button: event.MouseWheelDown,
	}}, 0, 0)
	wheel.Bounds = render.Rect{W: 10, H: 10}
	if changed, _ := state.Apply(wheel, count, rowAt(&state)); !changed || state.Selected != 1 {
		t.Fatalf("wheel down changed=%v selected=%d; want true 1", changed, state.Selected)
	}
}

func TestTreeApplyPressSelectsPressedRow(t *testing.T) {
	state := TreeState{Offset: 1}
	count := len(treeRows(&state))
	press := checkAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MousePress, Button: event.MouseLeft,
	}}, 0, 0)
	press.Bounds = render.Rect{W: 10, H: 1}
	// The press lands on viewport row 0, which is content row 1.
	if changed, activated := state.Apply(press, count, rowAt(&state)); !changed || activated {
		t.Fatalf("press changed=%v activated=%v; want true false", changed, activated)
	}
	if state.Selected != 1 {
		t.Fatalf("selected after press = %d; want 1", state.Selected)
	}
	// A press outside the content rows selects nothing.
	state.Offset, state.Selected = 0, 0
	press.Y = 5
	state.Apply(press, count, rowAt(&state))
	if state.Selected != 0 {
		t.Fatalf("out-of-range press selected %d", state.Selected)
	}
}

func TestTreeActivateTogglesExpansion(t *testing.T) {
	state := TreeState{}
	enter := checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0)
	// ui is selected and expandable: the toggle opens it.
	if changed, activated := state.Apply(enter, len(treeRows(&state)), rowAt(&state)); !changed || !activated {
		t.Fatalf("enter changed=%v activated=%v; want true true", changed, activated)
	}
	if !state.IsExpanded("ui") {
		t.Fatalf("enter did not expand ui")
	}
	if got := len(treeRows(&state)); got != 4 {
		t.Fatalf("visible rows after expanding ui = %d; want 4", got)
	}
	// The same activation closes it again and the map entry is false, not
	// deleted, so the application can rely on the key existing either way.
	if changed, _ := state.Apply(enter, 4, rowAt(&state)); !changed || state.IsExpanded("ui") {
		t.Fatalf("second enter changed=%v expanded=%v; want true false", changed, state.IsExpanded("ui"))
	}
	if _, ok := state.Expanded["ui"]; !ok {
		t.Fatalf("collapsed node lost its map entry")
	}
	// Space works the same way (it arrives as a rune key), and a completed
	// click (a routed release) is the mouse equivalent.
	space := checkAction(Activate, event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: ' '}}, 0, 0)
	if changed, _ := state.Apply(space, 2, rowAt(&state)); !changed || !state.IsExpanded("ui") {
		t.Fatalf("space changed=%v expanded=%v; want true true", changed, state.IsExpanded("ui"))
	}
	click := checkAction(Activate, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MouseRelease, Button: event.MouseLeft,
	}}, 0, 0)
	if changed, activated := state.Apply(click, 4, rowAt(&state)); !changed || !activated || state.IsExpanded("ui") {
		t.Fatalf("click changed=%v activated=%v expanded=%v", changed, activated, state.IsExpanded("ui"))
	}
}

func TestTreeActivateOnLeafAndWithoutRowAt(t *testing.T) {
	state := TreeState{Selected: 1}
	enter := checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0)
	// With ui expanded, row 1 is the leaf ui/table.go: activation reports
	// itself but toggles nothing and changes nothing.
	state.Expanded = map[string]bool{"ui": true}
	if changed, activated := state.Apply(enter, len(treeRows(&state)), rowAt(&state)); changed || !activated {
		t.Fatalf("leaf activation changed=%v activated=%v; want false true", changed, activated)
	}
	if state.IsExpanded("ui") != true {
		t.Fatalf("leaf activation mutated the expansion set")
	}
	// A nil rowAt is tolerated: activation still reports, nothing toggles.
	bare := TreeState{}
	if changed, activated := bare.Apply(enter, 5, nil); changed || !activated {
		t.Fatalf("nil rowAt changed=%v activated=%v; want false true", changed, activated)
	}
	if bare.Expanded != nil {
		t.Fatalf("nil rowAt created a map")
	}
}

func TestTreeApplyInertAndClamps(t *testing.T) {
	state := TreeState{}
	// Key releases are inert.
	release := checkAction(Activate, event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyEnter, Action: event.ActionRelease}}, 0, 0)
	if changed, activated := state.Apply(release, 2, rowAt(&state)); changed || activated {
		t.Fatalf("release changed=%v activated=%v; want false false", changed, activated)
	}
	// Horizontal arrows route to the focused tree but do nothing: the tree
	// is vertical only.
	right := checkAction(Scroll, event.KeyPress(event.KeyRight, 0), 0, 0)
	right.Bounds = render.Rect{W: 10, H: 10}
	if changed, activated := state.Apply(right, 2, rowAt(&state)); changed || activated {
		t.Fatalf("right arrow changed=%v activated=%v; want false false", changed, activated)
	}
	// Count 0 resets selection and offset.
	reset := TreeState{Selected: 1, Offset: 1}
	if changed := func() bool {
		changed, _ := reset.Apply(enter(0), 0, nil)
		return changed
	}(); !changed || reset.Selected != 0 || reset.Offset != 0 {
		t.Fatalf("count 0 changed=%v selected=%d offset=%d", changed, reset.Selected, reset.Offset)
	}
	// A selection past the count clamps back inside.
	clamped := TreeState{Selected: 9}
	clamped.Apply(down(10), 2, nil)
	if clamped.Selected != 1 {
		t.Fatalf("clamped selected = %d; want 1", clamped.Selected)
	}
	var nilState *TreeState
	if changed, activated := nilState.Apply(enter(0), 5, nil); changed || activated {
		t.Fatalf("nil receiver changed=%v activated=%v; want false false", changed, activated)
	}
}

func TestTreeApplyFollowsViewportAndKeepsToggleHonest(t *testing.T) {
	state := TreeState{}
	// Everything expanded gives six visible rows; a viewport of two rows
	// cannot hold them, so navigation pulls the offset along.
	state.Expanded = map[string]bool{"ui": true, "examples": true, "examples/widgets": true}
	rows := len(treeRows(&state)) // 6
	state.Selected = rows - 1     // the last row
	down := down(2)
	state.Apply(down, rows, rowAt(&state))
	if state.Selected != rows-1 || state.Offset != rows-2 {
		t.Fatalf("end navigation selected=%d offset=%d; want %d %d", state.Selected, state.Offset, rows-1, rows-2)
	}
	// Walking back up pulls the offset up with the selection.
	up := checkAction(Scroll, event.KeyPress(event.KeyUp, 0), 0, 0)
	up.Bounds = render.Rect{W: 10, H: 2}
	state.Apply(up, rows, rowAt(&state))
	if state.Selected != rows-2 || state.Offset != rows-2 {
		t.Fatalf("up navigation selected=%d offset=%d; want %d %d", state.Selected, state.Offset, rows-2, rows-2)
	}
}

func TestTreeDrawsMarkersIndentAndSelection(t *testing.T) {
	buffer := render.NewBuffer(28, 5)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TreeState{Selected: 1}
	state.Expanded = map[string]bool{"ui": true}
	rows := treeRows(&state)
	ctx.Tree("files", canvas, render.Rect{X: 0, Y: 0, W: 28, H: 5}, &state, len(rows),
		func(index int) TreeRow { return rows[index] }, TreeStyle{})
	want := []string{
		"- ui",
		"   table.go",
		"   tree.go",
		"+ examples",
		"",
	}
	for y, expected := range want {
		if got := bufferRow(buffer, y); got != expected {
			t.Fatalf("tree row %d = %q; want %q", y, got, expected)
		}
	}
}

func TestTreeDrawsCustomMarkersAndClips(t *testing.T) {
	buffer := render.NewBuffer(16, 3)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	rows := []TreeRow{
		{ID: "a", Label: "source tree", HasChildren: true},
		{ID: "a/b", Label: "truncated-beyond-the-edge", Depth: 1},
	}
	state := TreeState{}
	// Custom markers: ASCII here because the test compares cell-exact rows
	// and the canvas's width policy makes ambiguous glyphs terminal-dependent.
	ctx.Tree("files", canvas, render.Rect{X: 0, Y: 0, W: 16, H: 3}, &state, len(rows),
		func(index int) TreeRow { return rows[index] }, TreeStyle{Collapsed: ">", Expanded: "v", Leaf: "-"})
	if got := bufferRow(buffer, 0); got != "> source tree" {
		t.Fatalf("custom marker row = %q", got)
	}
	if got := bufferRow(buffer, 1); got != "  - truncated-be" {
		t.Fatalf("leaf row clipped = %q", got)
	}
	// Empty bounds, nil canvas, nil state, and no rowAt draw nothing. Each
	// guard call uses a fresh ID: a begun context panics on duplicate
	// registration, and these guards assert draw-side only.
	ctx.Tree("empty", canvas, render.Rect{W: 0, H: 3}, &state, len(rows), nil, TreeStyle{})
	ctx.Tree("nil-canvas", nil, render.Rect{W: 16, H: 3}, &state, len(rows), nil, TreeStyle{})
	ctx.Tree("nil-state", canvas, render.Rect{W: 16, H: 3}, nil, len(rows), nil, TreeStyle{})
	ctx.Tree("no-rows", canvas, render.Rect{W: 16, H: 3}, &state, 0, nil, TreeStyle{})
}

// enter and down build routed actions with a viewport, the shape the router
// always delivers.
func enter(x int) Action {
	return checkAction(Activate, event.KeyPress(event.KeyEnter, 0), x, 0)
}

func down(rows int) Action {
	action := checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0)
	action.Bounds = render.Rect{W: 10, H: rows}
	return action
}

func BenchmarkTreeDraw(b *testing.B) {
	// Matches BenchmarkTableDraw: no Begin, so region registration is
	// outside the measured loop and the benchmark measures pure painting.
	rows := make([]TreeRow, 30)
	for i := range rows {
		rows[i] = TreeRow{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Label: "node-with-a-longer-name", Depth: i % 4}
	}
	buffer := render.NewBuffer(40, 10)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "tree"}
	state := TreeState{Selected: 5}
	b.ReportAllocs()
	for range b.N {
		ctx.Tree("tree", canvas, render.Rect{X: 0, Y: 0, W: 40, H: 10}, &state, len(rows),
			func(index int) TreeRow { return rows[index] }, TreeStyle{})
	}
}
