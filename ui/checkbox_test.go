package ui

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func checkAction(kind ActionKind, next event.Event, x, y int) Action {
	return Action{Kind: kind, Event: next, X: x, Y: y}
}

func TestCheckboxStateTogglesOnActivationOnly(t *testing.T) {
	state := CheckboxState{}
	activate := checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0)
	for range 2 {
		changed, activated := state.Apply(activate)
		if !changed || !activated {
			t.Fatalf("activation changed=%v activated=%v; want true true", changed, activated)
		}
	}
	if state.Checked {
		t.Fatalf("double activation left the box checked")
	}
	// A plain activation leaves nothing half-committed: the toggle is atomic.
	changed, activated := state.Apply(activate)
	if !changed || !activated || !state.Checked {
		t.Fatalf("third activation changed=%v activated=%v checked=%v; want true true true", changed, activated, state.Checked)
	}
}

func TestCheckboxStateReleasesAndForeignActionsAreInert(t *testing.T) {
	state := CheckboxState{}
	if changed, activated := state.Apply(checkAction(Activate, event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyEnter, Action: event.ActionRelease}}, 0, 0)); changed || activated {
		t.Fatalf("key release changed=%v activated=%v; want false false", changed, activated)
	}
	if changed, activated := state.Apply(checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0)); changed || activated {
		t.Fatalf("scroll changed=%v activated=%v; want false false", changed, activated)
	}
	if changed, activated := state.Apply(checkAction(Focus, event.KeyPress(event.KeyTab, 0), 0, 0)); changed || activated {
		t.Fatalf("focus changed=%v activated=%v; want false false", changed, activated)
	}
	if state.Checked {
		t.Fatalf("inert actions toggled the box")
	}
	// A mouse release carries Activate from the router and IS the click.
	if changed, activated := state.Apply(checkAction(Activate, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft}}, 2, 0)); !changed || !activated {
		t.Fatalf("click release changed=%v activated=%v; want true true", changed, activated)
	}
	if !state.Checked {
		t.Fatalf("click release did not check the box")
	}
}

func TestCheckboxStateNilReceiver(t *testing.T) {
	var state *CheckboxState
	if changed, activated := state.Apply(checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0)); changed || activated {
		t.Fatalf("nil receiver changed=%v activated=%v; want false false", changed, activated)
	}
}

func TestCheckboxDrawsMarkersAndClips(t *testing.T) {
	buffer := render.NewBuffer(24, 3)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	checked := CheckboxState{Checked: true}
	ctx.Checkbox("on", canvas, render.Rect{X: 0, Y: 0, W: 24, H: 1}, &checked, "stream telemetry", CheckboxStyle{})
	unchecked := CheckboxState{}
	ctx.Checkbox("off", canvas, render.Rect{X: 0, Y: 1, W: 12, H: 1}, &unchecked, "a very long label beyond twelve", CheckboxStyle{})
	if len(ctx.building) != 2 {
		t.Fatalf("regions = %d; want 2", len(ctx.building))
	}
	if got, ok := buffer.CellOK(0, 0); ok && got.Grapheme != "[" {
		t.Fatalf("checked marker starts with %q; want [", got.Grapheme)
	}
	if got := bufferRow(buffer, 0); got != "[x] stream telemetry" {
		t.Fatalf("checked row = %q", got)
	}
	if got := bufferRow(buffer, 1); got != "[ ] a very l" {
		t.Fatalf("clipped unchecked row = %q", got)
	}
}

func TestCheckboxRestyledMarkersAndDisabled(t *testing.T) {
	buffer := render.NewBuffer(20, 2)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := CheckboxState{Checked: true}
	ctx.Checkbox("on", canvas, render.Rect{X: 0, Y: 0, W: 20, H: 1}, &state, "done", CheckboxStyle{
		Checked: "(yes)", Unchecked: "(no)",
	})
	if got := bufferRow(buffer, 0); got != "(yes) done" {
		t.Fatalf("restyled row = %q", got)
	}
	state.Checked = false
	ctx.Checkbox("off", canvas, render.Rect{X: 0, Y: 1, W: 20, H: 1}, &state, "nope", CheckboxStyle{
		Checked: "(yes)", Unchecked: "(no)", Disabled: true,
	})
	if got := bufferRow(buffer, 1); got != "(no) nope" {
		t.Fatalf("disabled row = %q", got)
	}
	if ctx.building[1].flags&Disabled == 0 {
		t.Fatalf("disabled checkbox region flags = %v", ctx.building[1].flags)
	}
}

func TestRadioStateMovesSelection(t *testing.T) {
	state := RadioState{}
	down := checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0)
	for range 2 {
		if changed, _ := state.Apply(down, 3); !changed {
			t.Fatal("KeyDown reported no change")
		}
	}
	if state.Selected != 2 {
		t.Fatalf("Selected = %d; want 2", state.Selected)
	}
	if changed, _ := state.Apply(down, 3); changed || state.Selected != 2 {
		t.Fatalf("clamped Down changed=%v Selected=%d", changed, state.Selected)
	}
	state.Apply(checkAction(Scroll, event.KeyPress(event.KeyHome, 0), 0, 0), 3)
	if state.Selected != 0 {
		t.Fatalf("Home Selected = %d; want 0", state.Selected)
	}
	state.Apply(checkAction(Scroll, event.KeyPress(event.KeyEnd, 0), 0, 0), 3)
	if state.Selected != 2 {
		t.Fatalf("End Selected = %d; want 2", state.Selected)
	}
	state.Apply(checkAction(Scroll, event.KeyPress(event.KeyUp, 0), 0, 0), 3)
	if state.Selected != 1 {
		t.Fatalf("Up Selected = %d; want 1", state.Selected)
	}
	// Pages step half the group.
	state.Apply(checkAction(Scroll, event.KeyPress(event.KeyPageUp, 0), 0, 0), 3)
	if state.Selected != 0 {
		t.Fatalf("PageUp Selected = %d; want 0", state.Selected)
	}
}

func TestRadioStatePressWheelActivateAndReset(t *testing.T) {
	state := RadioState{Selected: 2}
	press := checkAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}}, 0, 0)
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: press.Event, Y: 0}, 4); !changed || state.Selected != 0 {
		t.Fatalf("press row 0 changed=%v Selected=%d; want true 0", changed, state.Selected)
	}
	state.Apply(Action{Kind: Scroll, Event: press.Event, Y: 3}, 4)
	if state.Selected != 3 {
		t.Fatalf("press row 3 Selected = %d; want 3", state.Selected)
	}
	// Out-of-range presses are inert.
	if changed, _ := state.Apply(Action{Kind: Scroll, Event: press.Event, Y: 4}, 4); changed || state.Selected != 3 {
		t.Fatalf("out-of-range press changed=%v", changed)
	}
	wheel := checkAction(Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelUp}}, 0, 0)
	if changed, _ := state.Apply(wheel, 4); !changed || state.Selected != 2 {
		t.Fatalf("wheel up changed=%v Selected=%d; want true 2", changed, state.Selected)
	}
	// Activation reports without moving the choice.
	changed, activated := state.Apply(checkAction(Activate, event.KeyPress(event.KeyEnter, 0), 0, 0), 4)
	if changed || !activated {
		t.Fatalf("activate changed=%v activated=%v; want false true", changed, activated)
	}
	// Key releases are inert.
	if changed, _ := state.Apply(checkAction(Scroll, event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyDown, Action: event.ActionRelease}}, 0, 0), 4); changed {
		t.Fatalf("key release changed=%v; want false", changed)
	}
	// A zero count resets.
	state.Selected = 3
	if changed, _ := state.Apply(down3(), 0); !changed || state.Selected != 0 {
		t.Fatalf("zero count changed=%v Selected=%d", changed, state.Selected)
	}
}

func down3() Action {
	return checkAction(Scroll, event.KeyPress(event.KeyDown, 0), 0, 0)
}

func TestRadioStateClampsAndNil(t *testing.T) {
	state := RadioState{Selected: 9}
	state.Apply(down3(), 3)
	if state.Selected != 2 {
		t.Fatalf("clamped Selected = %d; want 2", state.Selected)
	}
	state.Selected = -4
	// Entry healing clamps the stale negative choice to 0 before the action,
	// so one Down lands on 1: bounded state survives bad persistence.
	state.Apply(down3(), 3)
	if state.Selected != 1 {
		t.Fatalf("healed Selected = %d; want 1", state.Selected)
	}
	var nilState *RadioState
	if changed, activated := nilState.Apply(down3(), 3); changed || activated {
		t.Fatalf("nil receiver changed=%v activated=%v", changed, activated)
	}
}

func TestRadioGroupDrawsMarkersAndBounds(t *testing.T) {
	buffer := render.NewBuffer(20, 4)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.begun = true
	state := RadioState{Selected: 1}
	ctx.RadioGroup("palette", canvas, render.Rect{X: 0, Y: 0, W: 20, H: 3}, &state, []string{"ink", "violet", "muted"}, RadioStyle{})
	if len(ctx.building) != 1 {
		t.Fatalf("regions = %d; want 1", len(ctx.building))
	}
	region := ctx.building[0]
	if region.flags&(Focusable|Scrollable|Activatable) == 0 {
		t.Fatalf("radio flags = %v", region.flags)
	}
	if region.contentHeight != 3 {
		t.Fatalf("radio contentHeight = %d; want 3", region.contentHeight)
	}
	// The middle choice of three can scroll both ways: neither bound is set.
	if region.atTop || region.atBottom {
		t.Fatalf("radio bounds flags: atTop=%v atBottom=%v; want false false", region.atTop, region.atBottom)
	}
	if got := bufferRow(buffer, 0); got != "( ) ink" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := bufferRow(buffer, 1); got != "(*) violet" {
		t.Fatalf("row 1 = %q", got)
	}
	// A group taller than its label list stops early: blank tail, no fourth row.
	if got := bufferRow(buffer, 3); got != "" {
		t.Fatalf("row 3 = %q; want blank", got)
	}
}

func bufferRow(buffer *render.Buffer, y int) string {
	width, _ := buffer.Size()
	row := ""
	for x := 0; x < width; x++ {
		if cell, ok := buffer.CellOK(x, y); ok {
			row += cell.Grapheme
		}
	}
	for len(row) > 0 && row[len(row)-1] == ' ' {
		row = row[:len(row)-1]
	}
	return row
}

// BenchmarkCheckboxDraw prices the checkbox paint path: one fill, one
// clipped text. Drawn without registering regions, like BenchmarkTableDraw.
func BenchmarkCheckboxDraw(b *testing.B) {
	buffer := render.NewBuffer(24, 1)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "check"}
	state := CheckboxState{Checked: true}
	b.ReportAllocs()
	for range b.N {
		ctx.Checkbox("check", canvas, render.Rect{W: 24, H: 1}, &state, "stream telemetry", CheckboxStyle{})
	}
}

// BenchmarkRadioGroupDraw prices a three-option radio group repaint.
func BenchmarkRadioGroupDraw(b *testing.B) {
	buffer := render.NewBuffer(20, 3)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "palette"}
	state := RadioState{Selected: 1}
	b.ReportAllocs()
	for range b.N {
		ctx.RadioGroup("palette", canvas, render.Rect{W: 20, H: 3}, &state, []string{"ink", "violet", "muted"}, RadioStyle{})
	}
}
