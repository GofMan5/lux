package ui

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func keyAction(code event.KeyCode) Action {
	return Action{Kind: Scroll, Event: event.KeyPress(code, 0)}
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
