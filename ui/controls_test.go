package ui

import (
	"testing"

	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

func keyAction(code event.KeyCode) Action {
	return Action{Kind: Scroll, Event: event.KeyPress(code, 0)}
}

// TestListStateIgnoresKeyReleases pins the contract for direct Apply
// callers: a kitty release neither moves selection nor activates, while a
// repeat moves selection exactly like a press.
func TestListStateIgnoresKeyReleases(t *testing.T) {
	state := ListState{Selected: 1}
	release := Action{Kind: Scroll, Event: event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyDown, Action: event.ActionRelease}}}
	if changed, activated := state.Apply(release, 10, 3); changed || activated || state.Selected != 1 || state.Offset != 0 {
		t.Fatalf("release changed=%v activated=%v state=%#v", changed, activated, state)
	}
	releaseEnter := Action{Kind: Activate, Event: event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyEnter, Action: event.ActionRelease}}}
	if changed, activated := state.Apply(releaseEnter, 10, 3); changed || activated {
		t.Fatalf("enter release changed=%v activated=%v", changed, activated)
	}
	repeat := Action{Kind: Scroll, Event: event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyDown, Action: event.ActionRepeat, Repeat: 1}}}
	if changed, activated := state.Apply(repeat, 10, 3); !changed || activated || state.Selected != 2 {
		t.Fatalf("repeat changed=%v activated=%v state=%#v", changed, activated, state)
	}
}

func TestListStateKeyboardScrollAndBounds(t *testing.T) {
	state := ListState{Selected: 1}
	if changed, activated := state.Apply(keyAction(event.KeyPageDown), 10, 3); !changed || activated || state.Selected != 4 || state.Offset != 2 {
		t.Fatalf("pagedown = %#v changed=%v activated=%v", state, changed, activated)
	}
	state.Apply(keyAction(event.KeyEnd), 10, 3)
	if state.Selected != 9 || state.Offset != 7 {
		t.Fatalf("end = %#v", state)
	}
	state.Apply(keyAction(event.KeyHome), 10, 3)
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("home = %#v", state)
	}
	state.Selected, state.Offset = 99, 99
	if changed, _ := state.Apply(Action{}, 2, 0); !changed || state.Selected != 1 || state.Offset != 0 {
		t.Fatalf("tiny = %#v changed=%v", state, changed)
	}
	state.Apply(Action{}, 2, 1)
	if changed, activated := state.Apply(Action{Kind: Activate}, 2, 1); changed || !activated {
		t.Fatalf("activate changed=%v activated=%v", changed, activated)
	}
	if changed, activated := state.Apply(Action{}, 0, 0); !changed || activated || state != (ListState{}) {
		t.Fatalf("empty = %#v changed=%v activated=%v", state, changed, activated)
	}
}

func TestListStateWheelAndClick(t *testing.T) {
	state := ListState{}
	wheel := Action{Kind: Scroll, Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelDown}}}
	state.Apply(wheel, 20, 6)
	if state.Selected != 2 || state.Offset != 0 {
		t.Fatalf("wheel = %#v", state)
	}
	click := Action{Kind: Scroll, Y: 4, Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}}}
	changed, activated := state.Apply(click, 20, 6)
	if !changed || activated || state.Selected != 4 {
		t.Fatalf("click = %#v changed=%v activated=%v", state, changed, activated)
	}
	if changed, activated = state.Apply(Action{Kind: Activate}, 20, 6); changed || !activated {
		t.Fatalf("release changed=%v activated=%v", changed, activated)
	}
	outside := click
	outside.Y = 8
	if changed, activated = state.Apply(outside, 20, 6); changed || activated {
		t.Fatalf("outside changed=%v activated=%v", changed, activated)
	}
	state = ListState{}
	blank := click
	blank.Y = 5
	if changed, activated = state.Apply(blank, 2, 6); changed || activated || state != (ListState{}) {
		t.Fatalf("blank changed=%v activated=%v state=%#v", changed, activated, state)
	}
}

func TestButtonListAndModalRendering(t *testing.T) {
	buffer := render.NewBuffer(32, 12)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	ctx.viewFocus = "button"
	ctx.Button("button", canvas, render.Rect{X: 1, Y: 1, W: 10, H: 3}, "Run 界", ButtonStyle{})
	state := ListState{Selected: 1}
	ctx.List("list", canvas, render.Rect{X: 1, Y: 5, W: 12, H: 3}, &state, []string{"one", "two", "three", "four"}, ListStyle{})
	inner := ctx.Modal("help", canvas, render.Rect{X: 16, Y: 1, W: 14, H: 9}, "Help", ModalStyle{})
	ctx.Region("close", inner, render.Rect{X: 1, Y: 1, W: 8, H: 2}, Focusable|Activatable)
	ctx.EndModal()

	if len(ctx.building) != 3 || ctx.building[2].modal != "help" {
		t.Fatalf("regions = %#v", ctx.building)
	}
	if got := buffer.Cell(3, 2).Grapheme; got == " " {
		t.Fatalf("button label not drawn: %q", got)
	}
	if got := buffer.Cell(1, 6).Grapheme; got == " " {
		t.Fatal("selected list marker not drawn")
	}
	if width, height := inner.Size(); width != 12 || height != 7 {
		t.Fatalf("modal inner = %dx%d", width, height)
	}
}

// TestModalClippedDrawMatchesUnclipped pins reference equivalence: the modal
// scope keeps the full rect, while the panel fill and the title follow the
// drawable band — cells inside the clip are identical to an unclipped draw,
// cells outside stay blank, and the returned interior keeps its full
// geometry for the controls drawn inside.
func TestModalClippedDrawMatchesUnclipped(t *testing.T) {
	draw := func(canvas *render.Canvas) *render.Canvas {
		var ctx Context
		ctx.begun = true
		inner := ctx.Modal("help", canvas, render.Rect{X: 4, Y: 2, W: 14, H: 8}, "Help", ModalStyle{})
		ctx.EndModal()
		return inner
	}
	full := render.NewBuffer(24, 14)
	draw(render.NewCanvas(full))
	part := render.NewBuffer(24, 14)
	clip := render.Rect{X: 0, Y: 6, W: 24, H: 6}
	inner := draw(render.NewCanvas(part).Clip(clip))
	for y := 0; y < 14; y++ {
		for x := 0; x < 24; x++ {
			if x >= clip.X && x < clip.Right() && y >= clip.Y && y < clip.Bottom() {
				if full.Cell(x, y) != part.Cell(x, y) {
					t.Fatalf("clipped cell (%d,%d) = %#v; want %#v", x, y, part.Cell(x, y), full.Cell(x, y))
				}
				continue
			}
			if part.Cell(x, y) != render.BlankCell {
				t.Fatalf("cell outside clip (%d,%d) = %#v; want blank", x, y, part.Cell(x, y))
			}
		}
	}
	if width, height := inner.Size(); width != 12 || height != 6 {
		t.Fatalf("modal inner = %dx%d; want the full 12x6 interior", width, height)
	}
}

func TestControlsTinyAndDisabledAreSafe(t *testing.T) {
	buffer := render.NewBuffer(2, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	ctx.Button("x", canvas, render.Rect{W: 1, H: 1}, "too long", ButtonStyle{Disabled: true})
	state := ListState{Selected: 4, Offset: 4}
	ctx.List("list", canvas, render.Rect{X: 1, W: 1, H: 1}, &state, nil, ListStyle{Disabled: true})
	if state != (ListState{}) {
		t.Fatalf("empty list state = %#v", state)
	}
	if len(ctx.building) != 2 || ctx.building[0].flags&Disabled == 0 || ctx.building[1].flags&Disabled == 0 {
		t.Fatalf("disabled regions = %#v", ctx.building)
	}
}

// TestListDisabledAtRows pins the per-row disabled contract: rows the
// callback marks disabled paint DisabledStyle even when selected, while
// the selection marker stays on the selected row and enabled rows keep
// their own styling; the region itself is not flag-disabled, because
// whether an activation runs is the model's call.
func TestListDisabledAtRows(t *testing.T) {
	normal := render.Style{Foreground: render.RGBColor(1, 2, 3)}
	selected := render.Style{Foreground: render.RGBColor(4, 5, 6)}
	disabled := render.Style{Foreground: render.RGBColor(7, 8, 9)}
	buffer := render.NewBuffer(16, 4)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true

	style := ListStyle{Normal: normal, Selected: selected, DisabledStyle: disabled}
	style.DisabledAt = func(index int) bool { return index == 1 }

	state := ListState{Selected: 1}
	ctx.List("list", canvas, render.Rect{X: 0, Y: 0, W: 16, H: 4}, &state,
		[]string{"alpha", "beta", "gamma"}, style)
	if got := buffer.Cell(4, 1).Style.Foreground; got != disabled.Foreground {
		t.Fatalf("disabled selected row fg = %v; want %v", got, disabled.Foreground)
	}
	if got := buffer.Cell(0, 1).Grapheme; got != ">" {
		t.Fatalf("disabled selected row must keep its marker: %q", got)
	}
	if flags := ctx.building[0].flags; flags&Disabled != 0 {
		t.Fatalf("per-row disabled must not flag the region: %v", flags)
	}
	if got := buffer.Cell(0, 0).Style.Foreground; got != normal.Foreground {
		t.Fatalf("enabled row fg = %v; want %v", got, normal.Foreground)
	}
	if got := buffer.Cell(0, 2).Style.Foreground; got != normal.Foreground {
		t.Fatalf("unselected row fg = %v; want %v", got, normal.Foreground)
	}

	// A disabled row outside the selection paints the same way; only the
	// marker distinguishes the selected row. A fresh Context stands in
	// for the next frame.
	state = ListState{Selected: 0}
	ctx = Context{}
	ctx.begun = true
	ctx.List("list", canvas, render.Rect{X: 0, Y: 0, W: 16, H: 4}, &state,
		[]string{"alpha", "beta", "gamma"}, style)
	if got := buffer.Cell(4, 1).Style.Foreground; got != disabled.Foreground {
		t.Fatalf("disabled unselected row fg = %v; want %v", got, disabled.Foreground)
	}
	if got := buffer.Cell(0, 0).Grapheme; got != ">" {
		t.Fatalf("enabled selected row must show its marker: %q", got)
	}
}

// TestListSkipsRowsHiddenByClip pins the ClipRect-aware row band: rows the
// active clip hides are neither painted nor priced through DisabledAt, and
// the region still registers with the full logical content height.
func TestListSkipsRowsHiddenByClip(t *testing.T) {
	buffer := render.NewBuffer(16, 10)
	// Clip exposes rows y2..7, so only local rows 2..7 may price anything.
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 0, Y: 2, W: 16, H: 6})
	var ctx Context
	ctx.begun = true
	calls := 0
	style := ListStyle{DisabledAt: func(index int) bool {
		calls++
		return index%2 == 0
	}}
	state := ListState{Selected: 4}
	ctx.List("list", canvas, render.Rect{X: 0, Y: 0, W: 16, H: 10}, &state,
		[]string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa"}, style)
	if calls != 6 {
		t.Fatalf("DisabledAt calls = %d; want 6", calls)
	}
	if len(ctx.building) != 1 {
		t.Fatalf("regions = %d; want 1", len(ctx.building))
	}
	// Registration still carries the FULL logical bounds: GlobalRect clips
	// the published rect, originY keeps the unclipped origin so hit mapping
	// preserves local row coordinates, and contentHeight only caps at the
	// clipped bottom (10 → 8).
	region := ctx.building[0]
	if region.bounds != (render.Rect{X: 0, Y: 2, W: 16, H: 6}) {
		t.Fatalf("region bounds = %#v; want GlobalRect-clipped rect", region.bounds)
	}
	if region.originY != 0 || region.originX != 0 {
		t.Fatalf("region origin = %d,%d; want 0,0 (full logical origin)", region.originX, region.originY)
	}
	if region.contentHeight != 8 {
		t.Fatalf("contentHeight = %d; want 8 (capped at clipped bottom)", region.contentHeight)
	}
}

// TestListClippedDrawMatchesUnclipped pins reference equivalence: cells
// inside the active clip are identical to an unclipped draw, cells outside
// stay blank.
func TestListClippedDrawMatchesUnclipped(t *testing.T) {
	items := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	draw := func(canvas *render.Canvas) {
		var ctx Context
		ctx.begun = true
		state := ListState{Selected: 2}
		ctx.List("list", canvas, render.Rect{X: 0, Y: 0, W: 16, H: 6}, &state, items, ListStyle{})
	}
	full := render.NewBuffer(16, 6)
	draw(render.NewCanvas(full))
	part := render.NewBuffer(16, 6)
	clip := render.Rect{X: 0, Y: 2, W: 16, H: 3}
	draw(render.NewCanvas(part).Clip(clip))
	for y := 0; y < 6; y++ {
		for x := 0; x < 16; x++ {
			if x >= clip.X && x < clip.Right() && y >= clip.Y && y < clip.Bottom() {
				if full.Cell(x, y) != part.Cell(x, y) {
					t.Fatalf("clipped cell (%d,%d) = %#v; want %#v", x, y, part.Cell(x, y), full.Cell(x, y))
				}
				continue
			}
			if part.Cell(x, y) != render.BlankCell {
				t.Fatalf("cell outside clip (%d,%d) = %#v; want blank", x, y, part.Cell(x, y))
			}
		}
	}
}
