package ui

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func tableColumns() []TableColumn {
	return []TableColumn{
		{Title: "ID", Width: 6},
		{Title: "Name"},
		{Title: "State", Weight: 1},
	}
}

func TestColumnLayoutFixedAndRubber(t *testing.T) {
	tests := []struct {
		name    string
		columns []TableColumn
		width   int
		want    []int
	}{
		{name: "nil", columns: nil, width: 10, want: nil},
		{name: "fixed only wider than table", columns: []TableColumn{{Width: 10}, {Width: 10}}, width: 5, want: []int{10, 10}},
		{
			name:    "fixed plus two rubber",
			columns: tableColumns(),
			width:   30,
			want:    []int{6, 11, 11},
		},
		{
			name:    "tight width splits the leftover evenly",
			columns: tableColumns(),
			width:   12,
			want:    []int{6, 2, 2},
		},
		{
			name:    "remainder extends the last rubber column",
			columns: tableColumns(),
			width:   11,
			want:    []int{6, 1, 2},
		},
		{
			name:    "no room collapses the last rubber",
			columns: tableColumns(),
			width:   9,
			want:    []int{6, 1, 0},
		},
		{
			name:    "no room collapses both",
			columns: tableColumns(),
			width:   8,
			want:    []int{6, 0, 0},
		},
		{
			name:    "weights share by proportion with remainder to the last",
			columns: []TableColumn{{Title: "a"}, {Title: "b", Weight: 3}},
			width:   16,
			want:    []int{4, 11},
		},
		{
			name:    "weight zero behaves as one",
			columns: []TableColumn{{Title: "a"}},
			width:   10,
			want:    []int{10},
		},
	}
	for _, test := range tests {
		if got := ColumnLayout(test.columns, test.width); !equalInts(got, test.want) {
			t.Errorf("%s: ColumnLayout = %v; want %v", test.name, got, test.want)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tableAction(kind ActionKind, next event.Event, width, height, x, y int) Action {
	return Action{Kind: kind, Event: next, Bounds: render.Rect{W: width, H: height}, X: x, Y: y}
}

func TestTableStateKeyboardNavigation(t *testing.T) {
	state := TableState{}
	columns := tableColumns()
	down := tableAction(Scroll, event.KeyPress(event.KeyDown, 0), 40, 8, 0, 0)
	for range 3 {
		if changed, _, _ := state.Apply(down, columns, 100); !changed {
			t.Fatal("KeyDown reported no change")
		}
	}
	if state.Selected != 3 {
		t.Fatalf("Selected = %d; want 3", state.Selected)
	}
	if state.Offset != 0 {
		t.Fatalf("Offset = %d; want 0", state.Offset)
	}
	if changed, _, _ := state.Apply(tableAction(Scroll, event.KeyPress(event.KeyPageDown, 0), 40, 8, 0, 0), columns, 100); !changed || state.Selected != 10 {
		t.Fatalf("PageDown changed=%v Selected=%d; want true 10", changed, state.Selected)
	}
	if state.Offset != 4 {
		t.Fatalf("Offset = %d; want 4 (selected row visible)", state.Offset)
	}
	state.Apply(tableAction(Scroll, event.KeyPress(event.KeyEnd, 0), 40, 8, 0, 0), columns, 100)
	if state.Selected != 99 || state.Offset != 93 {
		t.Fatalf("End: Selected=%d Offset=%d; want 99 93", state.Selected, state.Offset)
	}
	state.Apply(tableAction(Scroll, event.KeyPress(event.KeyHome, 0), 40, 8, 0, 0), columns, 100)
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("Home: Selected=%d Offset=%d; want 0 0", state.Selected, state.Offset)
	}
	// Past the end the selection stays clamped; the first press still pulls
	// the viewport to the selected last row, the second reports no change.
	state.Selected = 99
	if changed, _, _ := state.Apply(down, columns, 100); !changed || state.Selected != 99 || state.Offset != 93 {
		t.Fatalf("boundary Down changed=%v Selected=%d Offset=%d; want true 99 93", changed, state.Selected, state.Offset)
	}
	if changed, _, _ := state.Apply(down, columns, 100); changed || state.Selected != 99 || state.Offset != 93 {
		t.Fatalf("second boundary Down changed=%v; want false", changed)
	}
}

func TestTableStateKeyReleasesAreInert(t *testing.T) {
	state := TableState{Selected: 2, Offset: 2}
	release := event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyDown, Action: event.ActionRelease}}
	changed, activated, sorted := state.Apply(tableAction(Scroll, release, 40, 8, 0, 0), tableColumns(), 100)
	if changed || activated || sorted || state != (TableState{Selected: 2, Offset: 2}) {
		t.Fatalf("release mutated state: %#v (%v %v %v)", state, changed, activated, sorted)
	}
}

func TestTableStateWheelScrollsViewportOnly(t *testing.T) {
	state := TableState{Selected: 5}
	columns := tableColumns()
	wheel := tableAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MouseWheel, Button: event.MouseWheelDown,
	}}, 40, 8, 0, 0)
	if changed, _, _ := state.Apply(wheel, columns, 100); !changed {
		t.Fatal("wheel reported no change")
	}
	if state.Offset != 2 { // max(rows/3, 1) with 7 body rows.
		t.Fatalf("Offset = %d; want 2", state.Offset)
	}
	if state.Selected != 5 {
		t.Fatalf("wheel moved selection: %d; want 5", state.Selected)
	}
	// Far past the bottom the wheel stops at the last page.
	for range 100 {
		state.Apply(wheel, columns, 100)
	}
	if state.Offset != 93 {
		t.Fatalf("Offset = %d; want 93", state.Offset)
	}
}

func TestTableStateHeaderPressRequestsSort(t *testing.T) {
	state := TableState{}
	columns := tableColumns() // col 0 fixed 6 at x 0, col 1 at x 7.
	press := func(x int) Action {
		return tableAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			Action: event.MousePress, Button: event.MouseLeft,
		}}, 40, 8, x, 0)
	}
	changed, _, sorted := state.Apply(press(7), columns, 50)
	if !changed || !sorted || !state.SortActive || state.SortColumn != 1 || state.SortDesc || !state.SortRequested {
		t.Fatalf("first header press: %#v (%v %v)", state, changed, sorted)
	}
	state.SortRequested = false
	if _, _, sorted := state.Apply(press(7), columns, 50); !sorted || !state.SortDesc {
		t.Fatalf("second press should flip to desc: %#v", state)
	}
	if _, _, sorted := state.Apply(press(0), columns, 50); !sorted || state.SortColumn != 0 || state.SortDesc {
		t.Fatalf("press on another column restarts asc: %#v", state)
	}
	// A gutter hit (x 6) maps to no column and changes nothing.
	before := state
	if changed, _, sorted := state.Apply(press(6), columns, 50); changed || sorted || state != before {
		t.Fatalf("gutter press mutated state: %#v", state)
	}
	// A body press never sorts.
	body := tableAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MousePress, Button: event.MouseLeft,
	}}, 40, 8, 0, 3)
	if _, _, sorted := state.Apply(body, columns, 50); sorted {
		t.Fatal("body press requested a sort")
	}
}

func TestTableStatePressSelectsRow(t *testing.T) {
	state := TableState{Offset: 10}
	press := tableAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MousePress, Button: event.MouseLeft,
	}}, 40, 8, 0, 3)
	if changed, _, _ := state.Apply(press, tableColumns(), 100); !changed || state.Selected != 12 {
		t.Fatalf("press: changed=%v Selected=%d; want true 12", changed, state.Selected)
	}
	// A press below the content is ignored: with rows 0..6 holding indices
	// 93..99, y 8 lands past the body.
	state = TableState{Offset: 93, Selected: 96}
	below := tableAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MousePress, Button: event.MouseLeft,
	}}, 40, 8, 0, 8)
	if changed, _, _ := state.Apply(below, tableColumns(), 100); changed || state.Selected != 96 || state.Offset != 93 {
		t.Fatalf("press beyond content: %#v", state)
	}
}

func TestTableStateCountZeroResets(t *testing.T) {
	state := TableState{Selected: 4, Offset: 4}
	if changed, _, _ := state.Apply(Action{}, tableColumns(), 0); !changed || state != (TableState{}) {
		t.Fatalf("zero count should reset scroll: %#v", state)
	}
	// Sort state survives an empty table.
	state = TableState{SortActive: true, SortColumn: 1, SortDesc: true, Selected: 3}
	state.Apply(Action{}, tableColumns(), 0)
	if !state.SortActive || state.SortColumn != 1 || !state.SortDesc || state.Selected != 0 {
		t.Fatalf("sort state lost on empty: %#v", state)
	}
}

func TestTableStateNilReceiver(t *testing.T) {
	var state *TableState
	if changed, activated, sorted := state.Apply(tableAction(Scroll, event.KeyPress(event.KeyDown, 0), 40, 8, 0, 0), tableColumns(), 10); changed || activated || sorted {
		t.Fatal("nil receiver Apply should report nothing")
	}
}

func TestTableVirtualizesRows(t *testing.T) {
	const count = 1_000_000
	calls := 0
	rowAt := func(index int) []string {
		calls++
		return []string{"row", "name"}
	}
	buffer := render.NewBuffer(40, 10)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TableState{}
	ctx.Table("table", canvas, render.Rect{W: 40, H: 10}, &state, tableColumns(), count, rowAt, TableStyle{})
	if calls != 9 { // one header row, nine body rows.
		t.Fatalf("rowAt called %d times for a 10-row viewport; want 9", calls)
	}
	// Deep selection repaints only the visible window.
	second := Context{}
	second.begun = true
	calls = 0
	state.Selected = count - 1
	second.Table("table", canvas, render.Rect{W: 40, H: 10}, &state, tableColumns(), count, rowAt, TableStyle{})
	if calls != 9 {
		t.Fatalf("rowAt called %d times at the last row; want 9", calls)
	}
	if state.Offset != count-9 {
		t.Fatalf("Offset = %d; want %d", state.Offset, count-9)
	}
	if got := buffer.Cell(0, 1).Grapheme; got != "r" {
		t.Fatalf("first body cell = %q; want the drawn row", got)
	}
}

func TestTableDrawHeaderSelectionAndEllipsis(t *testing.T) {
	buffer := render.NewBuffer(40, 8)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	ctx.viewFocus = "table"
	columns := []TableColumn{
		{Title: "ID", Width: 6},
		{Title: "Name"},
	}
	rows := [][]string{
		{"1", "alpha"},
		{"2", "beta"},
		{"3", "gamma gamma gamma gamma gamma gamma gamma"},
	}
	state := TableState{Selected: 1, SortActive: true, SortColumn: 1}
	ctx.Table("table", canvas, render.Rect{W: 40, H: 8}, &state, columns, len(rows), func(i int) []string { return rows[i] }, TableStyle{})

	if attrs := buffer.Cell(0, 0).Style.Attrs; attrs&render.AttrUnderline == 0 {
		t.Fatalf("header not underlined: %v", attrs)
	}
	if got := buffer.Cell(0, 0).Grapheme; got != "I" {
		t.Fatalf("header cell = %q; want I", got)
	}
	// Name column starts after ID (6 cells) plus a gutter, so the sort
	// indicator sits at x 7+4+1 = 12.
	if got := buffer.Cell(12, 0).Grapheme; got != "▲" {
		t.Fatalf("sort indicator = %q; want ▲", got)
	}
	// The selected row carries the focused fill (zero-value styles leave the
	// marker as the empty style, so assert the drawn text moved down instead).
	if got := buffer.Cell(7, 2).Grapheme; got != "b" {
		t.Fatalf("selected row cell = %q; want b", got)
	}
	// The long third row is elided inside the rubber column (33 cells).
	if got := buffer.Cell(39, 3).Grapheme; got != "…" {
		t.Fatalf("elided cell = %q; want …", got)
	}
	// A tiny table draws nothing but keeps the region registered.
	tiny := render.NewBuffer(4, 2)
	var tinyCtx Context
	tinyCtx.begun = true
	tinyState := TableState{}
	tinyCtx.Table("tiny", render.NewCanvas(tiny), render.Rect{W: 4, H: 2}, &tinyState, tableColumns(), 100, func(int) []string { return nil }, TableStyle{})
	if len(tinyCtx.building) != 1 {
		t.Fatalf("tiny table regions = %d; want 1", len(tinyCtx.building))
	}
}

func TestTableEGCAndWideClipping(t *testing.T) {
	buffer := render.NewBuffer(24, 4)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	columns := []TableColumn{{Title: "W", Width: 3}}
	row := "世界世界世界"
	ctx.Table("table", canvas, render.Rect{W: 24, H: 4}, &TableState{}, columns, 1, func(int) []string { return []string{row} }, TableStyle{})
	// Three cells hold one whole CJK grapheme (with its continuation cell)
	// plus the ellipsis; no grapheme is ever split.
	if got := buffer.Cell(0, 1).Grapheme; got != "世" {
		t.Fatalf("cell 0 = %q; want 世", got)
	}
	if got, ok := buffer.CellOK(1, 1); ok && got.Grapheme != "" {
		t.Fatalf("cell 1 = %q; want the wide continuation blank", got.Grapheme)
	}
	if got := buffer.Cell(2, 1).Grapheme; got != "…" {
		t.Fatalf("cell 2 = %q; want …", got)
	}
	// Combining pairs stay whole under clipping.
	if got := clipTextEllipsis("é\u0301é\u0301é\u0301", 2, render.WidthPolicy{}, "…"); got != "é\u0301…" {
		t.Fatalf("combining clip = %q", got)
	}
	// An ellipsis wider than the column degrades to a plain clip.
	if got := clipTextEllipsis("ab", 1, render.WidthPolicy{}, ".."); got != "a" {
		t.Fatalf("wide ellipsis clip = %q", got)
	}
	// A multi-grapheme ellipsis is measured as a whole.
	if got := clipTextEllipsis("abcd", 3, render.WidthPolicy{}, ".."); got != "a.." {
		t.Fatalf("multi-grapheme ellipsis clip = %q", got)
	}
	// Ambiguous-width terminals keep the ellipsis at two cells: clipping
	// eight cells to five leaves one CJK grapheme plus the wide ellipsis.
	if got := clipTextEllipsis("世界世界", 5, render.WidthPolicy{AmbiguousWidth: 2}, "…"); got != "世…" {
		t.Fatalf("ambiguous clip = %q", got)
	}
}

func TestTableZeroColumnsAndNilRowAt(t *testing.T) {
	buffer := render.NewBuffer(20, 6)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TableState{Selected: 3}
	ctx.Table("table", canvas, render.Rect{W: 20, H: 6}, &state, nil, 5, nil, TableStyle{})
	if state.Selected != 3 {
		t.Fatalf("no-header table clamped selection: %#v", state)
	}
	region := ctx.building[0]
	if region.contentHeight != 5 || !region.atTop || !region.atBottom {
		t.Fatalf("no-header geometry: %#v", region)
	}
	if got := ColumnLayout(nil, 10); got != nil {
		t.Fatalf("ColumnLayout(nil) = %v; want nil", got)
	}
}

func TestTableDisabledResetsAndBlocks(t *testing.T) {
	buffer := render.NewBuffer(20, 6)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TableState{Selected: 4, Offset: 4}
	ctx.Table("table", canvas, render.Rect{W: 20, H: 6}, &state, tableColumns(), 0, nil, TableStyle{Disabled: true})
	if state != (TableState{}) {
		t.Fatalf("empty disabled table state = %#v", state)
	}
	if ctx.building[0].flags&Disabled == 0 {
		t.Fatalf("disabled region flags = %v", ctx.building[0].flags)
	}
}

func TestColumnAtMapsCells(t *testing.T) {
	columns := tableColumns()
	if got := columnAt(columns, 40, 0); got != 0 {
		t.Fatalf("columnAt(0) = %d; want 0", got)
	}
	if got := columnAt(columns, 40, 5); got != 0 {
		t.Fatalf("columnAt(5) = %d; want 0", got)
	}
	if got := columnAt(columns, 40, 7); got != 1 {
		t.Fatalf("columnAt(7) = %d; want 1", got)
	}
	if got := columnAt(columns, 40, 6); got != -1 {
		t.Fatalf("columnAt gutter = %d; want -1", got)
	}
	if got := columnAt(columns, 40, -1); got != -1 {
		t.Fatalf("columnAt(-1) = %d; want -1", got)
	}
}

// BenchmarkTableDraw prices a full repaint of a million-row virtualized
// table in a 120×40 viewport: layout plus 39 visible rows only. Like
// BenchmarkTextAreaDraw it draws without registering regions, so the
// number is the paint cost, not frame bookkeeping.
func BenchmarkTableDraw(b *testing.B) {
	buffer := render.NewBuffer(120, 40)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "table"}
	state := TableState{Selected: 500_000}
	columns := []TableColumn{{Title: "ID", Width: 8}, {Title: "Name", Weight: 2}, {Title: "State"}}
	rows := func(index int) []string {
		return []string{"00000000", "a moderately long process name", "running"}
	}
	b.ReportAllocs()
	for range b.N {
		ctx.Table("table", canvas, render.Rect{W: 120, H: 40}, &state, columns, 1_000_000, rows, TableStyle{})
	}
}
