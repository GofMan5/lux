package ui

import (
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// CheckboxState is the persistent checked flag of one checkbox. Its zero
// value is unchecked and ready.
type CheckboxState struct {
	Checked bool
}

// CheckboxStyle defines checkbox visual states. Zero values inherit Normal.
// Marker pair: Checked/Unchecked, ASCII by default so a CJK-locale terminal
// cannot widen the control; restyle to taste.
type CheckboxStyle struct {
	Normal        render.Style
	Focused       render.Style
	Hovered       render.Style
	Pressed       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Checked       string
	Unchecked     string
}

// Apply toggles the flag for a routed checkbox action. Activation is the
// only transition: Enter, Space, or a completed click — the router delivers
// a mouse click as Activate on the release, so key releases are inert while
// mouse releases are the click itself.
func (s *CheckboxState) Apply(action Action) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	if action.Kind != Activate {
		return false, false
	}
	s.Checked = !s.Checked
	return true, true
}

// Checkbox registers and draws one checkbox with its label: a marker plus
// text on a single row. Pressing focuses it; Enter/Space/click toggles.
func (c *Context) Checkbox(id ID, canvas *render.Canvas, bounds render.Rect, state *CheckboxState, label string, style CheckboxStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Activatable
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
	checked, empty := style.Checked, style.Unchecked
	if checked == "" {
		checked = "[x]"
	}
	if empty == "" {
		empty = "[ ]"
	}
	marker := checked
	if !state.Checked {
		marker = empty
	}
	text := clipText(marker+" "+label, bounds.W, canvas.WidthPolicy())
	canvas.Text(bounds.X, bounds.Y, text, visual)
}

// RadioState is the persistent choice of one radio group. Selected indexes
// the option list; its zero value selects the first option, like a default.
type RadioState struct {
	Selected int
}

// RadioStyle defines radio row states. Zero values inherit Normal. Marker
// pair: Selected/Empty, ASCII by default ("(*)"/"( )").
type RadioStyle struct {
	Normal        render.Style
	Selected      render.Style
	Focused       render.Style
	Hovered       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Marker        string
	Empty         string
}

// Apply moves the group choice for a routed action. Arrow keys, Home/End,
// PageUp/PageDown (half the group), wheel steps, and row presses all MOVE
// the choice — a radio selects on navigation, it has nothing to commit.
// Enter/Space/click release reports activated while keeping the choice.
// A count of 0 resets to the first position. Releases are inert.
func (s *RadioState) Apply(action Action, count int) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	if count <= 0 {
		if s.Selected != 0 {
			s.Selected = 0
			return true, false
		}
		return false, false
	}
	before := s.Selected
	s.Selected = min(max(s.Selected, 0), count-1)

	switch action.Kind {
	case Activate:
		activated = true
	case Scroll:
		switch action.Event.Kind {
		case event.Key:
			step := max(count/2, 1)
			switch action.Event.Key.Code {
			case event.KeyUp:
				s.Selected--
			case event.KeyDown:
				s.Selected++
			case event.KeyHome:
				s.Selected = 0
			case event.KeyEnd:
				s.Selected = count - 1
			case event.KeyPageUp:
				s.Selected -= step
			case event.KeyPageDown:
				s.Selected += step
			}
		case event.Mouse:
			switch action.Event.Mouse.Action {
			case event.MouseWheel:
				switch action.Event.Mouse.Button {
				case event.MouseWheelUp, event.MouseWheelLeft:
					s.Selected -= max(count/4, 1)
				case event.MouseWheelDown, event.MouseWheelRight:
					s.Selected += max(count/4, 1)
				}
			case event.MousePress:
				if action.Y >= 0 && action.Y < count {
					s.Selected = action.Y
				}
			}
		}
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	return before != s.Selected, activated
}

// RadioGroup registers and draws one radio group as a single focusable
// region: each option is a row, the selected row carries the marker, and
// pressing a row picks it.
func (c *Context) RadioGroup(id ID, canvas *render.Canvas, bounds render.Rect, state *RadioState, labels []string, style RadioStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	state.Selected = min(max(state.Selected, 0), max(len(labels)-1, 0))
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{
		ContentHeight: min(bounds.H, len(labels)),
		AtTop:         state.Selected <= 0,
		AtBottom:      state.Selected >= len(labels)-1,
	})
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	canvas = canvas.Clip(bounds)
	canvas.Fill(bounds, " ", style.Normal)
	marker, empty := style.Marker, style.Empty
	if marker == "" {
		marker = "(*)"
	}
	if empty == "" {
		empty = "( )"
	}
	for row := 0; row < bounds.H; row++ {
		if row >= len(labels) {
			break
		}
		visual := style.Normal
		prefix := empty
		if style.Disabled {
			visual = fallbackStyle(style.DisabledStyle, visual)
		} else if row == state.Selected {
			visual = fallbackStyle(style.Selected, visual)
			if c.Focused(id) {
				visual = fallbackStyle(style.Focused, visual)
			} else if c.Hovered(id) {
				visual = fallbackStyle(style.Hovered, visual)
			}
			prefix = marker
		}
		y := bounds.Y + row
		canvas.Fill(render.Rect{X: bounds.X, Y: y, W: bounds.W, H: 1}, " ", visual)
		text := clipText(prefix+" "+labels[row], bounds.W, canvas.WidthPolicy())
		canvas.Text(bounds.X, y, text, visual)
	}
}
