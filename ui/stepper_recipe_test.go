package ui

// This file is the executable half of docs/CUSTOM-CONTROL.md ("your own
// control in an evening"): the Stepper below is exactly the control that
// document walks through, kept here so the recipe's code can never rot.
// If you edit one, edit the other.

import (
	"strconv"
	"strings"
	"testing"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// StepperState is the persistent value of one stepper control. The zero
// value is 0 and unbounded; Min < Max bounds the value to that range.
type StepperState struct {
	Value int
	Min   int
	Max   int
	Step  int
}

// StepperStyle defines stepper visual states. Zero values inherit Normal.
type StepperStyle struct {
	Normal        render.Style
	Focused       render.Style
	Hovered       render.Style
	Pressed       render.Style
	DisabledStyle render.Style
	Disabled      bool
}

// step resolves the increment, honoring a zero Step as 1.
func (s *StepperState) step() int {
	if s == nil || s.Step <= 0 {
		return 1
	}
	return s.Step
}

// clamp pulls Value into [Min, Max] when the range is bounded.
func (s *StepperState) clamp() {
	if s.Max > s.Min {
		s.Value = min(max(s.Value, s.Min), s.Max)
	}
}

// Apply mutates the state for one routed stepper action and reports whether
// the value changed and whether a step key or half was used. The layout
// width comes from Action.Bounds: cell 0 is the minus half, the last cell
// is the plus half, and a press between them does nothing — seeking by
// drag is not claimed. Arrows, pages, Home/End, and the wheel step the
// value; a mouse press on a half steps immediately (a stepper is not a
// commit-style control, so the release is inert); key releases are inert;
// Enter/Space are inert.
func (s *StepperState) Apply(action Action) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	if action.Kind != Scroll {
		return false, false
	}
	before := s.Value
	switch action.Event.Kind {
	case event.Key:
		switch action.Event.Key.Code {
		case event.KeyLeft, event.KeyDown:
			s.Value -= s.step()
			activated = true
		case event.KeyRight, event.KeyUp:
			s.Value += s.step()
			activated = true
		case event.KeyPageUp:
			s.Value -= 10 * s.step()
			activated = true
		case event.KeyPageDown:
			s.Value += 10 * s.step()
			activated = true
		case event.KeyHome:
			if s.Max > s.Min {
				s.Value = s.Min
				activated = true
			}
		case event.KeyEnd:
			if s.Max > s.Min {
				s.Value = s.Max
				activated = true
			}
		}
	case event.Mouse:
		switch action.Event.Mouse.Action {
		case event.MouseWheel:
			switch action.Event.Mouse.Button {
			case event.MouseWheelUp, event.MouseWheelLeft:
				s.Value += s.step()
				activated = true
			case event.MouseWheelDown, event.MouseWheelRight:
				s.Value -= s.step()
				activated = true
			}
		case event.MousePress:
			width := action.Bounds.W
			if action.X == 0 && width > 1 {
				s.Value -= s.step()
				activated = true
			} else if action.X == width-1 && width > 1 {
				s.Value += s.step()
				activated = true
			}
		}
	}
	s.clamp()
	return s.Value != before, activated
}

// Stepper registers and draws one stepper: a minus cell, the centered
// value, a plus cell. The same width math drives drawing and pointer hits.
func (c *Context) Stepper(id ID, canvas *render.Canvas, bounds render.Rect, state *StepperState, style StepperStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable
	if style.Disabled {
		flags |= Disabled
	}
	c.Region(id, canvas, bounds, flags)
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	visual := style.Normal
	switch {
	case style.Disabled:
		visual = fallbackStyle(style.DisabledStyle, visual)
	case c.Pressed(id):
		visual = fallbackStyle(style.Pressed, visual)
	case c.Focused(id):
		visual = fallbackStyle(style.Focused, visual)
	case c.Hovered(id):
		visual = fallbackStyle(style.Hovered, visual)
	}
	canvas = canvas.Clip(bounds)
	canvas.Fill(render.Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1}, " ", visual)
	if bounds.W >= 2 {
		canvas.Text(bounds.X, bounds.Y, "-", visual)
		canvas.Text(bounds.X+bounds.W-1, bounds.Y, "+", visual)
	}
	if bounds.W >= 4 {
		value := strconv.Itoa(state.Value)
		x := bounds.X + (bounds.W-len(value))/2
		if x > bounds.X {
			canvas.Text(x, bounds.Y, value, visual)
		}
	}
}

func TestStepperApplyKeys(t *testing.T) {
	var s StepperState
	right := Action{Kind: Scroll, Event: event.KeyPress(event.KeyRight, 0), Bounds: render.Rect{W: 9, H: 1}}
	if changed, activated := s.Apply(right); !changed || !activated {
		t.Fatalf("right = %v %v", changed, activated)
	}
	if s.Value != 1 {
		t.Fatalf("value = %d", s.Value)
	}
	s.Apply(right)
	left := Action{Kind: Scroll, Event: event.KeyPress(event.KeyLeft, 0), Bounds: render.Rect{W: 9, H: 1}}
	if changed, _ := s.Apply(left); !changed || s.Value != 1 {
		t.Fatalf("left = %v value=%d", changed, s.Value)
	}
	page := Action{Kind: Scroll, Event: event.KeyPress(event.KeyPageDown, 0), Bounds: render.Rect{W: 9, H: 1}}
	if changed, activated := s.Apply(page); !changed || !activated || s.Value != 11 {
		t.Fatalf("page down = %v %v value=%d", changed, activated, s.Value)
	}
	release := Action{Kind: Scroll, Event: event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRight, Action: event.ActionRelease}}, Bounds: render.Rect{W: 9, H: 1}}
	if changed, activated := s.Apply(release); changed || activated {
		t.Fatal("key release was not inert")
	}
	activate := Action{Kind: Activate, Event: event.KeyPress(event.KeyEnter, 0)}
	if changed, activated := s.Apply(activate); changed || activated {
		t.Fatal("enter was not inert")
	}
}

func TestStepperApplyBounds(t *testing.T) {
	s := StepperState{Value: 5, Min: 0, Max: 10}
	home := Action{Kind: Scroll, Event: event.KeyPress(event.KeyHome, 0), Bounds: render.Rect{W: 9, H: 1}}
	if changed, activated := s.Apply(home); !changed || !activated || s.Value != 0 {
		t.Fatalf("home = %v %v value=%d", changed, activated, s.Value)
	}
	end := Action{Kind: Scroll, Event: event.KeyPress(event.KeyEnd, 0), Bounds: render.Rect{W: 9, H: 1}}
	s.Apply(end)
	if s.Value != 10 {
		t.Fatalf("end value = %d", s.Value)
	}
	right := Action{Kind: Scroll, Event: event.KeyPress(event.KeyRight, 0), Bounds: render.Rect{W: 9, H: 1}}
	if changed, _ := s.Apply(right); changed {
		t.Fatal("stepped past Max")
	}
	var free StepperState
	if changed, activated := free.Apply(home); changed || activated {
		t.Fatal("home on an unbounded stepper was not inert")
	}
}

func TestStepperApplyPointer(t *testing.T) {
	s := StepperState{Value: 40}
	pressMinus := Action{Kind: Scroll, Bounds: render.Rect{W: 9, H: 1}, X: 0, Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}}}
	if changed, activated := s.Apply(pressMinus); !changed || !activated || s.Value != 39 {
		t.Fatalf("minus half = %v %v value=%d", changed, activated, s.Value)
	}
	pressPlus := pressMinus
	pressPlus.X = 8
	if changed, activated := s.Apply(pressPlus); !changed || !activated || s.Value != 40 {
		t.Fatalf("plus half = %v %v value=%d", changed, activated, s.Value)
	}
	pressMiddle := pressMinus
	pressMiddle.X = 4
	if changed, activated := s.Apply(pressMiddle); changed || activated {
		t.Fatal("middle press was not inert")
	}
	wheel := Action{Kind: Scroll, Bounds: render.Rect{W: 9, H: 1}, Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelUp}}}
	if changed, activated := s.Apply(wheel); !changed || !activated || s.Value != 41 {
		t.Fatalf("wheel up = %v %v value=%d", changed, activated, s.Value)
	}
	release := Action{Kind: Scroll, Bounds: render.Rect{W: 9, H: 1}, X: 8, Event: event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease}}}
	if changed, activated := s.Apply(release); changed || activated {
		t.Fatal("mouse release was not inert")
	}
	var nilState *StepperState
	if changed, activated := nilState.Apply(pressMinus); changed || activated {
		t.Fatal("nil state was not inert")
	}
}

func TestStepperPaint(t *testing.T) {
	buffer := render.NewBuffer(20, 3)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	ctx.Begin(frame)
	s := StepperState{Value: 42}
	ctx.Stepper("count", canvas, render.Rect{X: 4, Y: 1, W: 9, H: 1}, &s, StepperStyle{})
	ctx.End(frame)
	var row strings.Builder
	for x := range 20 {
		if cell, ok := buffer.CellOK(x, 1); ok && cell.Grapheme != "" {
			row.WriteString(cell.Grapheme)
		}
	}
	if text := strings.TrimRight(row.String(), " "); text != "    -  42   +" {
		t.Fatalf("row = %q", text)
	}
}

func TestStepperRoutingEndToEnd(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "stepper", bounds: render.Rect{X: 4, Y: 1, W: 9, H: 1}, flags: Focusable | Scrollable},
	}, modal{})
	var s StepperState
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 12, Y: 1, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled := ctx.Handle(press)
	if !handled || action.Target != "stepper" || action.Kind != Scroll || action.X != 8 {
		t.Fatalf("press = %#v handled=%v", action, handled)
	}
	if changed, activated := s.Apply(action); !changed || !activated || s.Value != 1 {
		t.Fatalf("routed press = %v %v value=%d", changed, activated, s.Value)
	}
	arrow := event.KeyPress(event.KeyRight, 0)
	action, handled = ctx.Handle(arrow)
	if !handled || action.Target != "stepper" || action.Kind != Scroll {
		t.Fatalf("arrow = %#v handled=%v", action, handled)
	}
	if changed, activated := s.Apply(action); !changed || !activated || s.Value != 2 {
		t.Fatalf("routed arrow = %v %v value=%d", changed, activated, s.Value)
	}
}
