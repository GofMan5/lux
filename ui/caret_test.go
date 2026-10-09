package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

func caretOf(t *testing.T, buffer *render.Buffer) (int, int, bool) {
	t.Helper()
	return buffer.Caret()
}

func TestInputPlacesTerminalCaretOnlyWhenEditable(t *testing.T) {
	buffer := render.NewBuffer(20, 3)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	input := TextInput{Value: "日本語ab"}
	input.Cursor, input.Anchor = len("日本語"), len("日本語")

	var ctx Context
	ctx.Begin(frame)
	ctx.Input("name", canvas, render.Rect{X: 4, Y: 1, W: 10, H: 1}, &input, InputStyle{})
	x, y, visible := caretOf(t, buffer)
	if !visible || x != 4+6 || y != 1 {
		t.Fatalf("focused caret = (%d,%d,%t), want (10,1,true)", x, y, visible)
	}

	// Read-only and disabled fields keep focus but must not own the cursor.
	for _, style := range []InputStyle{{ReadOnly: true}, {Disabled: true}} {
		buffer.Reset()
		var readOnly Context
		readOnly.Begin(frame)
		readOnly.Input("name", canvas, render.Rect{X: 4, Y: 1, W: 10, H: 1}, &input, style)
		if _, _, visible := caretOf(t, buffer); visible {
			t.Fatalf("style %#v must not show a caret", style)
		}
	}

	// An unfocused field is skipped even though it paints.
	buffer.Reset()
	var two Context
	two.Begin(frame)
	two.Input("first", canvas, render.Rect{Y: 0, W: 10, H: 1}, &TextInput{}, InputStyle{})
	other := TextInput{Value: "second"}
	two.Input("second", canvas, render.Rect{Y: 2, W: 10, H: 1}, &other, InputStyle{})
	if x, y, visible := caretOf(t, buffer); !visible || x != 0 || y != 0 {
		t.Fatalf("first field owns the caret, got (%d,%d,%t)", x, y, visible)
	}

	// A horizontally scrolled field must place the caret from the visible slice,
	// not from the whole value, so it stays inside its own rectangle.
	buffer.Reset()
	long := TextInput{Value: "abcdefghijklmnop"}
	long.Cursor, long.Anchor = len(long.Value), len(long.Value)
	field := render.Rect{X: 2, Y: 1, W: 6, H: 1}
	var scrolled Context
	scrolled.Begin(frame)
	scrolled.Input("name", canvas, field, &long, InputStyle{})
	x, y, visible = caretOf(t, buffer)
	if !visible || x < field.X || x >= field.Right() || y != field.Y {
		t.Fatalf("scrolled caret = (%d,%d,%t), want inside %#v", x, y, visible, field)
	}
	if long.Offset == 0 {
		t.Fatalf("the fixture must force a horizontal offset: %#v", long)
	}
}

func TestInputCaretHiddenWhenScrolledOutOfClip(t *testing.T) {
	buffer := render.NewBuffer(20, 6)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	clipped := canvas.Clip(render.Rect{X: 0, Y: 3, W: 20, H: 3})
	input := TextInput{Value: "value"}

	var ctx Context
	ctx.Begin(frame)
	// The field is painted above the clip, exactly like a row scrolled out of a
	// viewport: it must not drag the terminal cursor into view.
	ctx.Input("name", clipped, render.Rect{X: 1, Y: 1, W: 8, H: 1}, &input, InputStyle{})
	if _, _, visible := caretOf(t, buffer); visible {
		t.Fatalf("clipped caret must stay hidden")
	}
}

func TestDragExtendsSelectionWithoutHistory(t *testing.T) {
	input := TextInput{Value: "abcdef", UndoBytes: 1 << 10}
	bounds := render.Rect{X: 3, Y: 2, W: 6, H: 1}

	press := Action{Target: "name", Kind: Input, Bounds: bounds, X: 1, Event: event.Event{
		Kind: event.Mouse, Mouse: event.MouseEvent{X: 4, Y: 2, Action: event.MousePress, Button: event.MouseLeft},
	}}
	if !input.Apply(press) || input.Cursor != 1 || input.Anchor != 1 {
		t.Fatalf("press = %#v", input)
	}
	drag := Action{Target: "name", Kind: Input, Bounds: bounds, X: 4, Event: event.Event{
		Kind: event.Mouse, Mouse: event.MouseEvent{X: 7, Y: 2, Action: event.MouseMove, Button: event.MouseLeft},
	}}
	if !input.Apply(drag) || input.Cursor != 4 || input.Anchor != 1 {
		t.Fatalf("drag = %#v", input)
	}
	if start, end := input.Selection(); start != 1 || end != 4 || input.SelectedText() != "bcd" {
		t.Fatalf("selection = %d..%d %q", start, end, input.SelectedText())
	}
	// Selection is not an edit: dragging must never consume undo history.
	if input.Undo() {
		t.Fatalf("drag recorded an undo step: %#v", input)
	}

	// Motion without the left button is hover or another button's gesture, not a
	// drag: it must not move the caret even when it points somewhere else.
	input.Cursor, input.Anchor = 1, 1
	for _, button := range []event.MouseButton{event.MouseNone, event.MouseRight} {
		hover := drag
		hover.X = 5
		hover.Event.Mouse.Button = button
		if input.Apply(hover) || input.Cursor != 1 {
			t.Fatalf("motion with button %v edited state: %#v", button, input)
		}
	}
}

func TestDragPastEdgesWalksOneGraphemeAndScrolls(t *testing.T) {
	input := TextInput{Value: "日本語です", Offset: len("日本"), Cursor: len("日本語"), Anchor: len("日本")}
	bounds := render.Rect{W: 4, H: 1}
	move := func(x int) Action {
		return Action{Target: "name", Kind: Input, Bounds: bounds, X: x, Event: event.Event{
			Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseMove, Button: event.MouseLeft},
		}}
	}

	if !input.Apply(move(9)) || input.Cursor != len("日本語で") {
		t.Fatalf("drag right past the edge = %#v", input)
	}
	if !input.Apply(move(9)) || input.Cursor != len("日本語です") {
		t.Fatalf("second drag right = %#v", input)
	}
	if input.Apply(move(9)) {
		t.Fatalf("drag past the end must saturate: %#v", input)
	}
	input.Cursor = input.Offset
	if !input.Apply(move(-2)) || input.Cursor != len("日") {
		t.Fatalf("drag left past the edge = %#v", input)
	}
	// The next frame's ensureOffset is what turns a caret move into a scroll.
	input.ensureOffset(bounds.W, true, input.widthPolicy)
	if input.Offset > input.Cursor {
		t.Fatalf("offset %d must follow the caret %d", input.Offset, input.Cursor)
	}
	if input.Anchor != len("日本") {
		t.Fatalf("anchor moved during drag: %#v", input)
	}
}

func TestContextRoutesCapturedDragAndStopsAtRelease(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "input", bounds: render.Rect{X: 2, Y: 1, W: 6, H: 1}, flags: Focusable | Editable},
		{id: "button", bounds: render.Rect{X: 2, Y: 3, W: 6, H: 1}, flags: Focusable | Activatable},
	}, modal{})

	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 4, Y: 1, Action: event.MousePress, Button: event.MouseLeft}}
	if action, handled := ctx.Handle(press); !handled || action.Kind != Input || action.Target != "input" {
		t.Fatalf("press = %#v handled=%v", action, handled)
	}
	// Capture keeps the drag on the pressed field with coordinates that leave it,
	// which is what lets a narrow field autoscroll.
	outside := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 12, Y: 4, Action: event.MouseMove, Button: event.MouseLeft}}
	action, handled := ctx.Handle(outside)
	if !handled || action.Kind != Input || action.Target != "input" || action.X != 10 || action.Y != 3 {
		t.Fatalf("captured drag = %#v handled=%v", action, handled)
	}

	release := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 12, Y: 4, Action: event.MouseRelease}}
	ctx.Handle(release)
	if action, handled := ctx.Handle(outside); handled || action.Kind != None {
		t.Fatalf("drag after release = %#v handled=%v", action, handled)
	}

	// A pressed button captures too, but a drag over it is not editable input.
	buttonPress := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 4, Y: 3, Action: event.MousePress, Button: event.MouseLeft}}
	ctx.Handle(buttonPress)
	if action, handled := ctx.Handle(outside); !handled || action.Kind != None {
		t.Fatalf("button drag = %#v handled=%v", action, handled)
	}
}

func wheel(x, y int, up bool) event.Event {
	button := event.MouseWheelDown
	if up {
		button = event.MouseWheelUp
	}
	return event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: x, Y: y, Action: event.MouseWheel, Button: button}}
}

func TestWheelBubblesToEnclosingScrollableAtBoundary(t *testing.T) {
	outer := region{id: "outer", bounds: render.Rect{W: 20, H: 10}, flags: Focusable | Scrollable}
	inner := region{id: "inner", bounds: render.Rect{X: 4, Y: 2, W: 10, H: 4}, flags: Focusable | Scrollable, atTop: true}
	var ctx Context
	presented(&ctx, []region{outer, inner}, modal{})

	// The child can still scroll down, so it keeps the wheel.
	if action, handled := ctx.Handle(wheel(6, 3, false)); !handled || action.Target != "inner" {
		t.Fatalf("child scroll = %#v handled=%v", action, handled)
	}
	// At its top boundary the same child hands the wheel to its parent.
	if action, handled := ctx.Handle(wheel(6, 3, true)); !handled || action.Target != "outer" {
		t.Fatalf("bubbled scroll = %#v handled=%v", action, handled)
	}
	// Outside the child, the parent is hit directly.
	if action, handled := ctx.Handle(wheel(18, 8, true)); !handled || action.Target != "outer" {
		t.Fatalf("parent scroll = %#v handled=%v", action, handled)
	}
}

func TestWheelStopsAtInnermostWhenNoParentCanScroll(t *testing.T) {
	exhausted := region{id: "inner", bounds: render.Rect{X: 1, Y: 1, W: 6, H: 3}, flags: Focusable | Scrollable, atTop: true, atBottom: true}
	cases := []struct {
		name    string
		regions []region
		target  ID
	}{
		{
			name:    "plain parent",
			regions: []region{{id: "panel", bounds: render.Rect{W: 20, H: 10}, flags: Focusable}, exhausted},
			target:  "inner",
		},
		{
			name: "overlapping sibling is not an ancestor",
			regions: []region{
				// Overlaps the child's left half without containing it, so it is a
				// neighbour painted underneath rather than a parent.
				{id: "sibling", bounds: render.Rect{W: 4, H: 8}, flags: Focusable | Scrollable},
				exhausted,
			},
			target: "inner",
		},
		{
			name: "exhausted parent",
			regions: []region{
				{id: "outer", bounds: render.Rect{W: 20, H: 10}, flags: Focusable | Scrollable, atTop: true, atBottom: true},
				exhausted,
			},
			target: "inner",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var ctx Context
			presented(&ctx, testCase.regions, modal{})
			action, handled := ctx.Handle(wheel(3, 2, true))
			if !handled || action.Target != testCase.target || action.Kind != Scroll {
				t.Fatalf("wheel = %#v handled=%v", action, handled)
			}
		})
	}
}

func TestWheelBubblingRespectsModalAndDisabledRegions(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "background", bounds: render.Rect{W: 20, H: 10}, flags: Focusable | Scrollable},
		{id: "panel", bounds: render.Rect{X: 2, Y: 2, W: 10, H: 6}, flags: Focusable | Scrollable, modal: "dialog"},
		{id: "list", bounds: render.Rect{X: 3, Y: 3, W: 8, H: 3}, flags: Focusable | Scrollable, modal: "dialog", atTop: true},
	}, modal{id: "dialog", bounds: render.Rect{X: 2, Y: 2, W: 10, H: 6}})

	if action, handled := ctx.Handle(wheel(4, 4, true)); !handled || action.Target != "panel" {
		t.Fatalf("modal bubbling = %#v handled=%v", action, handled)
	}
	// A modal must never leak the wheel to the background it covers.
	if action, handled := ctx.Handle(wheel(18, 9, true)); action.Target != "" || !handled {
		t.Fatalf("outside modal = %#v handled=%v", action, handled)
	}

	var blocked Context
	presented(&blocked, []region{
		{id: "outer", bounds: render.Rect{W: 20, H: 10}, flags: Focusable | Scrollable},
		{id: "cover", bounds: render.Rect{X: 1, Y: 1, W: 6, H: 4}, flags: Focusable | Scrollable | Disabled},
	}, modal{})
	if action, handled := blocked.Handle(wheel(2, 2, true)); handled || action.Target != "" {
		t.Fatalf("disabled cover must block = %#v handled=%v", action, handled)
	}
}

func TestScrollViewportAndListReportScrollBoundaries(t *testing.T) {
	buffer := render.NewBuffer(20, 8)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}

	cases := []struct {
		name     string
		offset   int
		content  int
		atTop    bool
		atBottom bool
	}{
		{name: "top", offset: 0, content: 100, atTop: true},
		{name: "middle", offset: 10, content: 100},
		{name: "bottom", offset: 96, content: 100, atBottom: true},
		{name: "fits", offset: 0, content: 2, atTop: true, atBottom: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var ctx Context
			ctx.Begin(frame)
			state := ScrollState{Offset: testCase.offset}
			ctx.ScrollViewport("log", canvas, render.Rect{W: 20, H: 4}, &state, testCase.content, ScrollStyle{})
			item := ctx.building[0]
			if item.atTop != testCase.atTop || item.atBottom != testCase.atBottom {
				t.Fatalf("viewport boundaries = %t/%t, want %t/%t", item.atTop, item.atBottom, testCase.atTop, testCase.atBottom)
			}
		})
	}

	items := []string{"a", "b", "c", "d", "e", "f"}
	// Every index matters: the List wheel moves the selection, so boundaries
	// derived from the scroll offset would agree only at the extremes.
	for selected := range items {
		var ctx Context
		ctx.Begin(frame)
		state := ListState{Selected: selected}
		ctx.List("items", canvas, render.Rect{W: 20, H: 3}, &state, items, ListStyle{})
		item := ctx.building[0]
		if item.atTop != (selected == 0) || item.atBottom != (selected == len(items)-1) {
			t.Fatalf("list selected=%d boundaries = %t/%t", selected, item.atTop, item.atBottom)
		}
	}

	var empty Context
	empty.Begin(frame)
	state := ListState{}
	empty.List("items", canvas, render.Rect{W: 20, H: 3}, &state, nil, ListStyle{})
	if item := empty.building[0]; !item.atTop || !item.atBottom {
		t.Fatalf("empty list must bubble: %#v", item)
	}
}

func TestEnsureOffsetKeepsCaretVisibleAndMinimal(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		cursor int
		width  int
		caret  bool
		offset int
	}{
		{name: "fits", value: "abcdef", cursor: 6, width: 10, caret: true, offset: 0},
		{name: "caret at end reserves a cell", value: "abcdef", cursor: 6, width: 4, caret: true, offset: 3},
		{name: "no caret needs no cell", value: "abcdef", cursor: 6, width: 4, caret: false, offset: 2},
		{name: "wide graphemes never split", value: "日本語です", cursor: len("日本語です"), width: 5, caret: true, offset: len("日本語")},
		{name: "cursor inside reserves its own width", value: "日本語です", cursor: len("日本"), width: 4, caret: true, offset: len("日")},
		{name: "zero width collapses", value: "abcdef", cursor: 4, width: 0, caret: true, offset: 4},
		{name: "width smaller than the caret glyph", value: "日本", cursor: 0, width: 1, caret: true, offset: 0},
		{name: "start of text", value: "abc", cursor: 0, width: 3, caret: true, offset: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// A stale offset must not survive: the window is recomputed, not nudged.
			for _, start := range []int{0, testCase.cursor, len(testCase.value)} {
				input := TextInput{Value: testCase.value, Cursor: testCase.cursor, Anchor: testCase.cursor, Offset: start}
				input.ensureOffset(testCase.width, testCase.caret, input.widthPolicy)
				if input.Offset != testCase.offset {
					t.Fatalf("from offset %d got %d, want %d", start, input.Offset, testCase.offset)
				}
				if input.Offset > input.Cursor {
					t.Fatalf("offset %d passed the caret %d", input.Offset, input.Cursor)
				}
			}
		})
	}
}

func TestEnsureOffsetStaysLinear(t *testing.T) {
	// Measuring the candidate window inside a loop made this quadratic: a pasted
	// 3 KB value took seconds per frame. One 10 KB recomputation is a handful of
	// milliseconds when linear and seconds when quadratic, so a generous bound
	// separates them. Retrying on a slow reading keeps a loaded machine from
	// reporting scheduling noise as a regression, since noise can only add time.
	const bound = 200 * time.Millisecond
	input := TextInput{Value: strings.Repeat("ab界🙂", 1<<10)}
	input.Cursor, input.Anchor = len(input.Value), len(input.Value)
	slowest := time.Duration(0)
	for range 3 {
		input.Offset = 0
		started := time.Now()
		input.ensureOffset(20, true, input.widthPolicy)
		elapsed := time.Since(started)
		if input.Offset == 0 || input.Offset > input.Cursor {
			t.Fatalf("offset = %d for cursor %d", input.Offset, input.Cursor)
		}
		if elapsed <= bound {
			return
		}
		slowest = max(slowest, elapsed)
	}
	t.Fatalf("every window recomputation over %d bytes exceeded %s, worst %s", len(input.Value), bound, slowest)
}

func TestScrollKeysBubbleAtBoundary(t *testing.T) {
	outer := region{id: "outer", bounds: render.Rect{W: 20, H: 10}, flags: Focusable | Scrollable}
	inner := region{id: "inner", bounds: render.Rect{X: 4, Y: 2, W: 10, H: 4}, flags: Focusable | Scrollable, atTop: true}
	var ctx Context
	presented(&ctx, []region{outer, inner}, modal{})
	if !ctx.Focus("inner") {
		t.Fatal("inner viewport must be focusable")
	}

	// Down still moves the focused viewport.
	if action, handled := ctx.Handle(event.KeyPress(event.KeyDown, 0)); !handled || action.Target != "inner" || action.Kind != Scroll {
		t.Fatalf("down = %#v handled=%v", action, handled)
	}
	// Up cannot, so the enclosing viewport receives the same key.
	for _, code := range []event.KeyCode{event.KeyUp, event.KeyPageUp, event.KeyHome} {
		action, handled := ctx.Handle(event.KeyPress(code, 0))
		if !handled || action.Target != "outer" || action.Kind != Scroll {
			t.Fatalf("%v = %#v handled=%v", code, action, handled)
		}
	}
	// Focus stays where it was; only the action was redirected.
	if !ctx.Focused("inner") {
		t.Fatalf("bubbling moved focus")
	}

	// With no enclosing viewport able to move, the key stays on the focused one.
	var alone Context
	presented(&alone, []region{
		{id: "panel", bounds: render.Rect{W: 20, H: 10}, flags: Focusable},
		{id: "list", bounds: render.Rect{X: 1, Y: 1, W: 8, H: 4}, flags: Focusable | Scrollable, atTop: true},
	}, modal{})
	alone.Focus("list")
	if action, handled := alone.Handle(event.KeyPress(event.KeyUp, 0)); !handled || action.Target != "list" {
		t.Fatalf("unbubbled key = %#v handled=%v", action, handled)
	}
}

func TestScrolledOutControlStaysReachable(t *testing.T) {
	buffer := render.NewBuffer(20, 8)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	container := ScrollState{}

	// A three-row window over nine rows of content: the last button is painted
	// outside the viewport, so it clips away entirely.
	build := func() {
		ctx.Begin(frame)
		view := ctx.ScrollViewport("panel", canvas, render.Rect{Y: 1, W: 20, H: 3}, &container, 9, ScrollStyle{Passive: true})
		top := -container.Offset
		ctx.Button("first", view, render.Rect{Y: top, W: 8, H: 1}, "one", ButtonStyle{})
		ctx.Button("last", view, render.Rect{Y: top + 8, W: 8, H: 1}, "two", ButtonStyle{})
	}
	build()
	if len(ctx.building) != 3 {
		t.Fatalf("regions = %d, want the viewport and both buttons", len(ctx.building))
	}
	hidden := ctx.building[2]
	if hidden.id != "last" || !hidden.bounds.Empty() || hidden.height != 1 {
		t.Fatalf("scrolled-out button = %#v", hidden)
	}
	presented(&ctx, ctx.building, modal{})

	// Tab reaches it even though it has no hit area, and it cannot be clicked.
	ctx.Focus("first")
	if action, handled := ctx.Handle(event.KeyPress(event.KeyTab, 0)); !handled || action.Target != "last" {
		t.Fatalf("tab = %#v handled=%v", action, handled)
	}
	click := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 2, Y: 9, Action: event.MousePress, Button: event.MouseLeft}}
	if action, _ := ctx.Handle(click); action.Target == "last" {
		t.Fatalf("a scrolled-out control must not be clickable: %#v", action)
	}

	// The container is told exactly how far to scroll to reveal it.
	ctx.Focus("last")
	delta := ctx.FocusDelta("panel")
	if delta != 6 {
		t.Fatalf("focus delta = %d, want 6", delta)
	}
	container.Offset += delta
	build()
	if revealed := ctx.building[2]; revealed.bounds.Empty() {
		t.Fatalf("scrolling did not reveal the control: %#v", revealed)
	}
	presented(&ctx, ctx.building, modal{})
	ctx.Focus("last")
	if again := ctx.FocusDelta("panel"); again != 0 {
		t.Fatalf("a visible control still asks for %d rows", again)
	}
}

func TestFocusDeltaIgnoresUnrelatedContainers(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "panel", bounds: render.Rect{X: 0, Y: 0, W: 10, H: 4}, flags: Focusable | Scrollable},
		{id: "outside", bounds: render.Rect{X: 12, Y: 9, W: 6, H: 1}, originX: 12, originY: 9, height: 1, flags: Focusable | Activatable},
	}, modal{})
	ctx.Focus("outside")
	if delta := ctx.FocusDelta("panel"); delta != 0 {
		t.Fatalf("a control in another column asked for %d rows", delta)
	}
	if delta := ctx.FocusDelta("missing"); delta != 0 {
		t.Fatalf("unknown container returned %d", delta)
	}
	if delta := ctx.FocusDelta("outside"); delta != 0 {
		t.Fatalf("a non-scrollable container returned %d", delta)
	}
}

func TestRevealIgnoresNeighbourOfNestedViewport(t *testing.T) {
	buffer := render.NewBuffer(20, 10)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	panel := ScrollState{}
	inner := ScrollState{}

	// A nested viewport with a button below it, both drawn into the panel. The
	// button is the viewport's neighbour, not its child, even though it sits in
	// the same columns and below its last row.
	ctx.Begin(frame)
	view := ctx.ScrollViewport("panel", canvas, render.Rect{W: 20, H: 4}, &panel, 12, ScrollStyle{Passive: true})
	ctx.ScrollViewport("inner", view, render.Rect{Y: 0, W: 20, H: 2}, &inner, 40, ScrollStyle{})
	ctx.Button("below", view, render.Rect{Y: 9, W: 8, H: 1}, "go", ButtonStyle{})
	regions := ctx.building
	if len(regions) != 3 {
		t.Fatalf("regions = %d", len(regions))
	}
	presented(&ctx, regions, modal{})
	ctx.Focus("below")

	if delta := ctx.FocusDelta("inner"); delta != 0 {
		t.Fatalf("the nested viewport tried to scroll a neighbour into view by %d rows", delta)
	}
	if delta := ctx.FocusDelta("panel"); delta != 6 {
		t.Fatalf("panel delta = %d, want 6", delta)
	}
}

// BenchmarkWheelBubbling measures the worst case for the router: every nested
// level is exhausted, so it walks the whole enclosure chain.
func BenchmarkWheelBubbling(b *testing.B) {
	for _, depth := range []int{2, 8, 32} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			regions := make([]region, 0, depth)
			for level := range depth {
				regions = append(regions, region{
					id:       ID(fmt.Sprint("level", level)),
					bounds:   render.Rect{X: level, Y: level, W: 200 - 2*level, H: 100 - 2*level},
					flags:    Focusable | Scrollable,
					atTop:    true,
					atBottom: true,
				})
			}
			var ctx Context
			presented(&ctx, regions, modal{})
			up := wheel(100, 50, true)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, handled := ctx.Handle(up); !handled {
					b.Fatal("wheel was not routed")
				}
			}
		})
	}
}
