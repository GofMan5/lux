package ui

import (
	"strings"
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func selectOptions() []string {
	return []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
}

func selectKey(kind ActionKind, code event.KeyCode) Action {
	return Action{Kind: kind, Event: event.KeyPress(code, 0)}
}

func selectMouse(kind ActionKind, action event.MouseAction, button event.MouseButton, x, y int) Action {
	return Action{
		Kind:  kind,
		Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: action, Button: button, X: x, Y: y}},
		X:     x, Y: y,
	}
}

func TestSelectStateOpensAndCommits(t *testing.T) {
	options := selectOptions()
	state := SelectState{}
	// Activation on a closed box opens, centring the viewport on the choice.
	changed, activated := state.Apply(selectKey(Activate, event.KeyEnter), len(options))
	if !changed || activated || !state.Open {
		t.Fatalf("open: changed=%v activated=%v open=%v; want true false true", changed, activated, state.Open)
	}
	// A second activation commits and closes.
	changed, activated = state.Apply(selectKey(Activate, event.KeyEnter), len(options))
	if !changed || !activated || state.Open {
		t.Fatalf("commit: changed=%v activated=%v open=%v; want true true false", changed, activated, state.Open)
	}
	// Opening from a late choice centres it, bounded by the list end.
	state = SelectState{Selected: 7}
	state.Apply(selectKey(Activate, event.KeyEnter), len(options))
	if state.Offset != 2 {
		t.Fatalf("centred Offset = %d; want 2 (selected 7, only 2 rows of scroll left)", state.Offset)
	}
	// Navigation while closed opens too.
	state = SelectState{}
	changed, activated = state.Apply(selectKey(Scroll, event.KeyDown), len(options))
	if !changed || activated || !state.Open || state.Selected != 1 {
		t.Fatalf("closed Down: changed=%v activated=%v open=%v selected=%d", changed, activated, state.Open, state.Selected)
	}
}

func TestSelectStateNavigationAndViewport(t *testing.T) {
	options := selectOptions()
	state := SelectState{}
	state.Apply(selectKey(Activate, event.KeyEnter), len(options))
	state.Apply(selectKey(Scroll, event.KeyDown), len(options))
	if state.Selected != 1 || state.Offset != 0 {
		t.Fatalf("down: selected=%d offset=%d", state.Selected, state.Offset)
	}
	state.Apply(selectKey(Scroll, event.KeyEnd), len(options))
	if state.Selected != 9 || state.Offset != 2 {
		t.Fatalf("end: selected=%d offset=%d; want 9 2 (viewport follows)", state.Selected, state.Offset)
	}
	state.Apply(selectKey(Scroll, event.KeyHome), len(options))
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("home: selected=%d offset=%d", state.Selected, state.Offset)
	}
	state.Apply(selectKey(Scroll, event.KeyPageDown), len(options))
	if state.Selected != 8 {
		t.Fatalf("page down selected=%d; want 8", state.Selected)
	}
	state.Apply(selectKey(Scroll, event.KeyPageUp), len(options))
	if state.Selected != 0 {
		t.Fatalf("page up selected=%d; want 0", state.Selected)
	}
	// Pages step by the popup height even when restyled smaller.
	state.PopupRows = 3
	state.Apply(selectKey(Scroll, event.KeyPageDown), len(options))
	if state.Selected != 3 {
		t.Fatalf("small popup page selected=%d; want 3", state.Selected)
	}
	state.PopupRows = 0
	// The wheel scrolls the viewport without moving the choice.
	state.Selected, state.Offset = 0, 0
	state.Apply(selectMouse(Scroll, event.MouseWheel, event.MouseWheelDown, 0, 0), len(options))
	if state.Offset != 2 || state.Selected != 0 {
		t.Fatalf("wheel: offset=%d selected=%d; want 2 0", state.Offset, state.Selected)
	}
	state.Apply(selectMouse(Scroll, event.MouseWheel, event.MouseWheelUp, 0, 0), len(options))
	state.Apply(selectMouse(Scroll, event.MouseWheel, event.MouseWheelUp, 0, 0), len(options))
	if state.Offset != 0 {
		t.Fatalf("wheel clamped up: offset=%d; want 0", state.Offset)
	}
	state.Apply(selectMouse(Scroll, event.MouseWheel, event.MouseWheelDown, 0, 0), len(options))
	for range 4 {
		state.Apply(selectMouse(Scroll, event.MouseWheel, event.MouseWheelDown, 0, 0), len(options))
	}
	if state.Offset != 2 {
		t.Fatalf("wheel clamped down: offset=%d; want 2 (count-rows)", state.Offset)
	}
}

func TestSelectStatePopupPressesPickOptions(t *testing.T) {
	options := selectOptions()
	state := SelectState{}
	state.Apply(selectKey(Activate, event.KeyEnter), len(options))
	// Row 0 is the inert value anchor: pressing it moves nothing.
	changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 0), len(options))
	if changed || state.Selected != 0 {
		t.Fatalf("anchor press changed=%v selected=%d; want false 0", changed, state.Selected)
	}
	// Popup row 1 picks the option under Offset; pressing the already
	// current option stays honest and reports no change.
	state.Selected = 5
	if changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 1), len(options)); !changed || state.Selected != 0 {
		t.Fatalf("row 1 press: changed=%v selected=%d; want true 0", changed, state.Selected)
	}
	if changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 1), len(options)); changed || state.Selected != 0 {
		t.Fatalf("repeat row 1 press: changed=%v; want false", changed)
	}
	// A scrolled viewport maps deeper rows through Offset (max offset 2 here).
	state.Offset = 2
	if changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 2), len(options)); !changed || state.Selected != 3 {
		t.Fatalf("row 2 at offset 2: changed=%v selected=%d; want true 3", changed, state.Selected)
	}
	// Beyond the option list the press is inert.
	if changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 9), len(options)); changed || state.Selected != 3 {
		t.Fatalf("past-end press: changed=%v", changed)
	}
	// Pressing the closed box moves nothing: the release opens it.
	state.Open = false
	if changed, _ := state.Apply(selectMouse(Scroll, event.MousePress, event.MouseLeft, 0, 0), len(options)); changed {
		t.Fatalf("closed press changed=%v; want false", changed)
	}
	// And that release arrives as Activate and re-opens.
	if changed, activated := state.Apply(selectMouse(Activate, event.MouseRelease, event.MouseLeft, 0, 0), len(options)); !changed || activated || !state.Open {
		t.Fatalf("release after closed press: changed=%v activated=%v open=%v; want true false true", changed, activated, state.Open)
	}
}

func TestSelectStateGuardsAndResets(t *testing.T) {
	options := selectOptions()
	// Key releases are inert even while open.
	state := SelectState{Open: true, Selected: 2, Offset: 0}
	if changed, activated := state.Apply(Action{Kind: Scroll, Event: event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyDown, Action: event.ActionRelease}}}, len(options)); changed || activated || state.Selected != 2 {
		t.Fatalf("key release: changed=%v activated=%v", changed, activated)
	}
	// A zero option count closes and resets the choice.
	state = SelectState{Open: true, Selected: 5, Offset: 2}
	if changed, activated := state.Apply(selectKey(Scroll, event.KeyDown), 0); !changed || activated || state.Open || state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("zero count: changed=%v activated=%v state=%#v", changed, activated, state)
	}
	// Stale state heals on the next action: entry clamps first, then Down.
	state = SelectState{Selected: 99, Offset: 99}
	state.Apply(selectKey(Scroll, event.KeyDown), len(options))
	if state.Selected != 9 || state.Offset != 2 {
		t.Fatalf("stale healed selected=%d offset=%d; want 9 2 (clamped to the list, then visible)", state.Selected, state.Offset)
	}
	var nilState *SelectState
	if changed, activated := nilState.Apply(selectKey(Activate, event.KeyEnter), 4); changed || activated {
		t.Fatalf("nil receiver changed=%v activated=%v", changed, activated)
	}
	// popupRows defaults and clamps to the count.
	state = SelectState{}
	if got := state.popupRows(3); got != 3 {
		t.Fatalf("popupRows(3) = %d; want 3", got)
	}
	if got := state.popupRows(30); got != 8 {
		t.Fatalf("popupRows(30) = %d; want 8", got)
	}
	state.PopupRows = 2
	if got := state.popupRows(30); got != 2 {
		t.Fatalf("popupRows override = %d; want 2", got)
	}
}

func TestSelectDrawClosedAndOpen(t *testing.T) {
	options := selectOptions()
	buffer := render.NewBuffer(30, 12)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := SelectState{Selected: 1}
	ctx.Select("motion", canvas, render.Rect{X: 0, Y: 0, W: 30, H: 1}, &state, options, SelectStyle{})
	if len(ctx.building) != 1 {
		t.Fatalf("regions = %d; want 1", len(ctx.building))
	}
	region := ctx.building[0]
	if region.flags&(Focusable|Scrollable|Activatable) == 0 {
		t.Fatalf("closed select flags = %v", region.flags)
	}
	if region.contentHeight != 1 {
		t.Fatalf("closed select contentHeight = %d; want 1", region.contentHeight)
	}
	if got := bufferRow(buffer, 0); !strings.HasPrefix(got, " bravo ") || !strings.HasSuffix(got, "▾") {
		t.Fatalf("closed row = %q; want ` bravo ` prefix with ▾ at the last cell", got)
	}
	if got := buffer.Cell(29, 0).Grapheme; got != "▾" {
		t.Fatalf("marker cell = %q; want ▾", got)
	}

	// Open: the region grows to anchor plus popup rows and the option press
	// rows map one-to-one onto the drawn rows.
	openBuffer := render.NewBuffer(30, 12)
	openCanvas := render.NewCanvas(openBuffer)
	var openCtx Context
	openCtx.begun = true
	state = SelectState{Selected: 2, Open: true}
	openCtx.Select("motion", openCanvas, render.Rect{X: 0, Y: 3, W: 30, H: 1}, &state, options, SelectStyle{})
	openRegion := openCtx.building[0]
	if openRegion.bounds.H != 1+8 {
		t.Fatalf("open bounds = %#v; want height 9", openRegion.bounds)
	}
	if openRegion.contentHeight != 1+8 {
		t.Fatalf("open contentHeight = %d", openRegion.contentHeight)
	}
	if got := bufferRow(openBuffer, 3); !strings.HasPrefix(got, " charlie ") || !strings.HasSuffix(got, "▾") {
		t.Fatalf("open anchor row = %q", got)
	}
	if got := bufferRow(openBuffer, 4); got != "   alpha" {
		t.Fatalf("option row 0 = %q; want `   alpha` (fill + two-space prefix)", got)
	}
	if got := bufferRow(openBuffer, 6); got != " > charlie" {
		t.Fatalf("option row 2 = %q", got)
	}
}

func TestSelectDrawClipsPopupAndOptions(t *testing.T) {
	options := []string{"世界世界世界", "окно"}
	buffer := render.NewBuffer(10, 6)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := SelectState{Open: true, Selected: 1}
	ctx.Select("clip", canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &state, options, SelectStyle{})
	// The region never grows past the option count: 1 + 2 rows.
	if got := ctx.building[0].bounds.H; got != 3 {
		t.Fatalf("popup height = %d; want 3", got)
	}
	// Wide graphemes stay whole inside the option clip and overflow reads
	// as an ellipsis, never a split grapheme.
	if got := bufferRow(buffer, 1); got != "   世界…" {
		t.Fatalf("clipped option = %q", got)
	}
	// The second option is the current choice: selected prefix and style.
	if got := bufferRow(buffer, 2); got != " > окно" {
		t.Fatalf("second option = %q", got)
	}
	// A select wider than the canvas registers only the reachable cells.
	narrow := render.NewBuffer(6, 6)
	narrowCanvas := render.NewCanvas(narrow)
	var narrowCtx Context
	narrowCtx.begun = true
	state = SelectState{}
	narrowCtx.Select("narrow", narrowCanvas, render.Rect{X: 2, Y: 0, W: 12, H: 1}, &state, options, SelectStyle{})
	if got := narrowCtx.building[0].bounds.W; got != 4 {
		t.Fatalf("clipped width = %d; want 4", got)
	}
	// An empty option list draws an empty closed row and resets state.
	state = SelectState{Selected: 3, Open: true}
	emptyOptions := []string{}
	ctx.Select("empty", canvas, render.Rect{X: 0, Y: 4, W: 10, H: 1}, &state, emptyOptions, SelectStyle{})
	if state.Selected != 0 || state.Open {
		t.Fatalf("empty draw state = %#v", state)
	}
}

// BenchmarkSelectDrawOpen prices the open dropdown repaint: anchor row plus
// eight option rows with prefix clipping.
func BenchmarkSelectDrawOpen(b *testing.B) {
	options := selectOptions()
	buffer := render.NewBuffer(30, 12)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "motion"}
	state := SelectState{Selected: 2}
	b.ReportAllocs()
	for range b.N {
		ctx.Select("motion", canvas, render.Rect{X: 0, Y: 0, W: 30, H: 1}, &state, options, SelectStyle{})
	}
}

// BenchmarkSelectDrawClosed prices the closed box repaint.
func BenchmarkSelectDrawClosed(b *testing.B) {
	options := selectOptions()
	buffer := render.NewBuffer(30, 1)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "motion"}
	state := SelectState{}
	b.ReportAllocs()
	for range b.N {
		ctx.Select("motion", canvas, render.Rect{X: 0, Y: 0, W: 30, H: 1}, &state, options, SelectStyle{})
	}
}
