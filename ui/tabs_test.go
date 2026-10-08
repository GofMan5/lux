package ui

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

var tabsLabels = []string{"alpha", "beta", "gamma"}

func TestTabLayoutMeasuresSlots(t *testing.T) {
	slots := tabLayout(tabsLabels, 0, 30)
	if len(slots) != 3 {
		t.Fatalf("slots = %d; want 3", len(slots))
	}
	// Label + one padding cell on each side.
	want := []tabSlot{{0, 0, 7}, {1, 7, 6}, {2, 13, 7}}
	for i, slot := range want {
		if slots[i] != slot {
			t.Fatalf("slot %d = %+v; want %+v", i, slots[i], slot)
		}
	}
	// A narrow strip clips the last slot instead of dropping it.
	slots = tabLayout(tabsLabels, 0, 9)
	if len(slots) != 2 || slots[1].w != 2 {
		t.Fatalf("clipped slots = %+v; want alpha + beta clipped to 2", slots)
	}
	// The offset skips earlier tabs.
	slots = tabLayout(tabsLabels, 2, 30)
	if len(slots) != 1 || slots[0].index != 2 || slots[0].x != 0 {
		t.Fatalf("offset slots = %+v; want gamma at x 0", slots)
	}
}

func TestTabsStateMovesSelection(t *testing.T) {
	state := TabsState{}
	right := checkAction(Scroll, event.KeyPress(event.KeyRight, 0), 0, 0)
	for range 2 {
		if changed, _ := state.Apply(right, tabsLabels); !changed {
			t.Fatal("KeyRight reported no change")
		}
	}
	if state.Selected != 2 {
		t.Fatalf("Selected = %d; want 2", state.Selected)
	}
	if changed, _ := state.Apply(right, tabsLabels); changed || state.Selected != 2 {
		t.Fatalf("clamped Right changed=%v Selected=%d", changed, state.Selected)
	}
	state.Apply(tabsKey(event.KeyHome, 30), tabsLabels)
	if state.Selected != 0 {
		t.Fatalf("Home Selected = %d; want 0", state.Selected)
	}
	state.Apply(tabsKey(event.KeyEnd, 30), tabsLabels)
	if state.Selected != 2 {
		t.Fatalf("End Selected = %d; want 2", state.Selected)
	}
	state.Apply(tabsKey(event.KeyLeft, 30), tabsLabels)
	if state.Selected != 1 {
		t.Fatalf("Left Selected = %d; want 1", state.Selected)
	}
	// Vertical keys are inert: a one-row strip has no vertical axis.
	if changed, _ := state.Apply(checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0), tabsLabels); changed {
		t.Fatal("KeyDown changed the tab selection")
	}
	if changed, _ := state.Apply(checkAction(Scroll, event.KeyPress(event.KeyPageDown, 0), 0, 0), tabsLabels); changed {
		t.Fatal("PageDown changed the tab selection")
	}
}

func TestTabsStatePressWheelActivateAndReset(t *testing.T) {
	state := TabsState{}
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}}
	// x 8 is inside beta's slot (x 7..12) at width 30.
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: press, X: 8, Y: 0, Bounds: render.Rect{W: 30}}, tabsLabels); !changed || state.Selected != 1 {
		t.Fatalf("press x 8 changed=%v Selected=%d; want true 1", changed, state.Selected)
	}
	// Presses past the last slot are inert.
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: press, X: 25, Y: 0, Bounds: render.Rect{W: 30}}, tabsLabels); changed || state.Selected != 1 {
		t.Fatalf("out-of-range press changed=%v", changed)
	}
	// A strip is one row: presses below it never reach the widget through the
	// router's content guard, and Apply agrees.
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: press, X: 4, Y: 1, Bounds: render.Rect{W: 30}}, tabsLabels); changed || state.Selected != 1 {
		t.Fatalf("press row 1 changed=%v", changed)
	}
	wheel := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelDown}}
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: wheel, X: 0, Y: 0, Bounds: render.Rect{W: 30}}, tabsLabels); !changed || state.Selected != 2 {
		t.Fatalf("wheel down changed=%v Selected=%d; want true 2", changed, state.Selected)
	}
	wheel.Mouse.Button = event.MouseWheelUp
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: wheel, X: 0, Y: 0, Bounds: render.Rect{W: 30}}, tabsLabels); !changed || state.Selected != 1 {
		t.Fatalf("wheel up changed=%v Selected=%d; want true 1", changed, state.Selected)
	}
	// Activation reports the page switch without moving the choice.
	changed, activated := state.Apply(checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0), tabsLabels)
	if changed || !activated {
		t.Fatalf("activate changed=%v activated=%v; want false true", changed, activated)
	}
	// Key releases are inert.
	release := event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRight, Action: event.ActionRelease}}
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: release, X: 0, Y: 0}, tabsLabels); changed {
		t.Fatal("key release changed the tab selection")
	}
	// A zero count resets.
	state.Selected = 2
	if changed, _ := state.Apply(rightKey(), nil); !changed || state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("zero count changed=%v Selected=%d Offset=%d", changed, state.Selected, state.Offset)
	}
	// Bounded state heals bad persistence.
	state.Selected = -4
	state.Apply(rightKey(), tabsLabels)
	if state.Selected != 1 {
		t.Fatalf("healed Selected = %d; want 1", state.Selected)
	}
	var nilState *TabsState
	if changed, activated := nilState.Apply(rightKey(), tabsLabels); changed || activated {
		t.Fatalf("nil receiver changed=%v activated=%v", changed, activated)
	}
}

func rightKey() Action {
	// The router fills Bounds for every routed action, keyboard included, so
	// keyboard navigation in Apply carries the strip width too.
	return tabsKey(event.KeyRight, 13)
}

func tabsKey(code event.KeyCode, width int) Action {
	return Action{Kind: Scroll, Event: event.KeyPress(code, 0), Bounds: render.Rect{W: width}}
}

func TestTabsKeepTabVisibleFollowsSelection(t *testing.T) {
	labels := []string{"one", "two", "three", "four", "five"}
	state := TabsState{}
	// Strip width 13 fits one(5) and two(5) plus three's clipped tail
	// (x10..13); selecting four finds no slot for it, so the offset pulls to
	// 1 and four ends the strip.
	for range 3 {
		state.Apply(rightKey(), labels)
	}
	if state.Selected != 3 || state.Offset != 1 {
		t.Fatalf("Selected=%d Offset=%d; want 3 1", state.Selected, state.Offset)
	}
	// Selecting an earlier tab rewinds the offset to it.
	state.Apply(tabsKey(event.KeyHome, 13), labels)
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("Home Selected=%d Offset=%d; want 0 0", state.Selected, state.Offset)
	}
	// A tab wider than the whole strip starts at the left edge and clips.
	wide := []string{"magnificent", "b"}
	state = TabsState{Selected: 1, Offset: 0}
	state.Apply(rightKey(), wide)
	if state.Offset != 1 {
		t.Fatalf("wide strip Offset = %d; want 1", state.Offset)
	}
	state.Apply(rightKey(), wide)
	if state.Selected != 1 || state.Offset != 1 {
		t.Fatalf("clamped wide Selected=%d Offset=%d; want 1 1", state.Selected, state.Offset)
	}
}

func TestTabsDrawsStrip(t *testing.T) {
	buffer := render.NewBuffer(24, 2)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TabsState{Selected: 1}
	ctx.Tabs("tabs", canvas, render.Rect{X: 0, Y: 0, W: 24, H: 1}, &state, tabsLabels, TabsStyle{})
	if len(ctx.building) != 1 {
		t.Fatalf("regions = %d; want 1", len(ctx.building))
	}
	region := ctx.building[0]
	if region.flags&(Focusable|Scrollable|Activatable) == 0 {
		t.Fatalf("tabs flags = %v", region.flags)
	}
	if region.contentHeight != 1 {
		t.Fatalf("tabs contentHeight = %d; want 1", region.contentHeight)
	}
	if !region.atTop || !region.atBottom {
		t.Fatalf("short strip bounds: atTop=%v atBottom=%v; want true true (all tabs fit)", region.atTop, region.atBottom)
	}
	if got := bufferRow(buffer, 0); got != " alpha  beta  gamma" {
		t.Fatalf("strip row = %q; want %q", got, " alpha  beta  gamma")
	}
}

func TestTabsDrawsOverflowStripAndClip(t *testing.T) {
	buffer := render.NewBuffer(10, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	// Offset 2 starts gamma at x 0; the 7-cell slot clips to 10 and the label
	// clips to 8 cells inside its padding.
	state := TabsState{Selected: 2, Offset: 2}
	ctx.Tabs("tabs", canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &state, tabsLabels, TabsStyle{})
	if got := bufferRow(buffer, 0); got != " gamma" {
		t.Fatalf("overflow strip row = %q; want %q", got, " gamma")
	}
	// A label wider than its slot clips to the slot's inner width.
	buffer = render.NewBuffer(9, 1)
	canvas = render.NewCanvas(buffer)
	wide := TabsState{Selected: 0}
	ctx.Tabs("wide", canvas, render.Rect{X: 0, Y: 0, W: 9, H: 1}, &wide, []string{"magnificent", "b"}, TabsStyle{})
	if got := bufferRow(buffer, 0); got != " magnifi" {
		t.Fatalf("clipped label row = %q; want %q", got, " magnifi")
	}
}

func TestTabsDrawEmptyAndDisabled(t *testing.T) {
	buffer := render.NewBuffer(10, 2)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := TabsState{Selected: 3}
	ctx.Tabs("empty", canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &state, nil, TabsStyle{})
	if got := bufferRow(buffer, 0); got != "" {
		t.Fatalf("empty strip row = %q; want blank", got)
	}
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("empty strip Selected=%d Offset=%d; want 0 0", state.Selected, state.Offset)
	}
	state = TabsState{Selected: 1}
	ctx.Tabs("off", canvas, render.Rect{X: 0, Y: 1, W: 10, H: 1}, &state, tabsLabels, TabsStyle{Disabled: true})
	if ctx.building[len(ctx.building)-1].flags&Disabled == 0 {
		t.Fatal("disabled strip did not register the Disabled flag")
	}
	var nilState *TabsState
	ctx.Tabs("nil", canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, nilState, tabsLabels, TabsStyle{})
	if got := bufferRow(buffer, 0); got != "" {
		t.Fatalf("nil state row = %q; want blank", got)
	}
}

func BenchmarkTabsDraw(b *testing.B) {
	// Matches BenchmarkTableDraw: no Begin, so region registration is out of
	// scope and the loop measures pure painting of the strip.
	buffer := render.NewBuffer(120, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	labels := make([]string, 12)
	for i := range labels {
		labels[i] = "panel-0" + string(rune('a'+i))
	}
	state := TabsState{Selected: 5}
	b.ReportAllocs()
	for range b.N {
		ctx.Tabs("bench", canvas, render.Rect{X: 0, Y: 0, W: 120, H: 1}, &state, labels, TabsStyle{})
	}
}
