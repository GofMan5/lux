package ui

import (
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// defaultSelectPopupRows bounds the open dropdown's height.
const defaultSelectPopupRows = 8

// SelectState is the persistent choice, viewport, and open flag of one
// select. Its zero value is closed with the first option current, ready.
// PopupRows (0 meaning defaultSelectPopupRows) is the option-row count of
// the open popup; the widget never grows taller than the option count.
type SelectState struct {
	Selected  int
	Offset    int
	Open      bool
	PopupRows int
}

// SelectStyle defines the closed box and the open popup states. Zero values
// resolve through the context theme: Normal and OptionNormal read Text,
// OptionSelected reads Accent, Popup and DisabledStyle read Muted and Danger
// respectively; explicit styles always win. The popup is intentionally
// borderless so its drawn cells and its press rows stay the same rectangle;
// style the Popup fill against the surrounding panel instead of a border.
type SelectStyle struct {
	Normal        render.Style
	Focused       render.Style
	Hovered       render.Style
	Pressed       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Marker        string

	Popup          render.Style
	OptionNormal   render.Style
	OptionSelected render.Style
	OptionFocused  render.Style
}

// popupRows resolves the effective popup height for a count of options.
func (s *SelectState) popupRows(count int) int {
	rows := s.PopupRows
	if rows <= 0 {
		rows = defaultSelectPopupRows
	}
	return min(rows, max(count, 0))
}

// Apply handles one routed action. The closed box opens on activation or on
// any navigation key; while open, arrows/Home/End/pages move the choice,
// the wheel scrolls the popup viewport without moving the choice, an option
// press (popup row 1 or deeper; row 0 is the inert value anchor) picks it,
// and the next activation commits and closes, reporting activated. A count
// of 0 closes and resets the choice. Key releases are inert.
func (s *SelectState) Apply(action Action, count int) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	count = max(count, 0)
	beforeSelected, beforeOffset, beforeOpen := s.Selected, s.Offset, s.Open
	if count == 0 {
		s.Selected, s.Offset, s.Open = 0, 0, false
		return beforeSelected != 0 || beforeOffset != 0 || beforeOpen, false
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	rows := s.popupRows(count)
	s.Offset = min(max(s.Offset, 0), max(count-rows, 0))

	// viewportOnly marks the wheel: it scrolls the popup viewport without
	// moving the choice, so the follow-the-selection pass must leave it be.
	viewportOnly := false
	switch action.Kind {
	case Activate:
		if s.Open {
			s.Open = false
			activated = true
		} else {
			s.Open = true
			s.Offset = min(max(s.Selected-rows/2, 0), max(count-rows, 0))
		}
	case Scroll:
		switch action.Event.Kind {
		case event.Key:
			switch action.Event.Key.Code {
			case event.KeyUp:
				s.Open = true
				s.Selected--
			case event.KeyDown:
				s.Open = true
				s.Selected++
			case event.KeyHome:
				s.Open = true
				s.Selected = 0
			case event.KeyEnd:
				s.Open = true
				s.Selected = count - 1
			case event.KeyPageUp:
				s.Open = true
				s.Selected -= max(rows, 1)
			case event.KeyPageDown:
				s.Open = true
				s.Selected += max(rows, 1)
			}
		case event.Mouse:
			switch action.Event.Mouse.Action {
			case event.MouseWheel:
				// The wheel scrolls the viewport only, like Table: a long
				// option list is browseable without moving the choice.
				viewportOnly = true
				delta := max(rows/3, 1)
				switch action.Event.Mouse.Button {
				case event.MouseWheelUp, event.MouseWheelLeft:
					s.Offset -= delta
				case event.MouseWheelDown, event.MouseWheelRight:
					s.Offset += delta
				}
			case event.MousePress:
				if !s.Open {
					// The closed box press lands on the anchor row and waits
					// for its release, which arrives as Activate and opens.
					break
				}
				if action.Y >= 1 {
					if index := s.Offset + action.Y - 1; action.Y-1 < rows && index < count {
						s.Selected = index
					}
				}
			}
		}
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	s.Offset = min(max(s.Offset, 0), max(count-rows, 0))
	if s.Open && !viewportOnly {
		if s.Selected < s.Offset {
			s.Offset = s.Selected
		}
		if s.Selected >= s.Offset+rows {
			s.Offset = s.Selected - rows + 1
		}
	}
	return beforeSelected != s.Selected || beforeOffset != s.Offset || beforeOpen != s.Open, activated
}

// Select registers and draws a dropdown. Closed, it is one row showing the
// current option; open, the same id registers the anchor row plus the popup
// below it, drawn over whatever it overlaps — register the select after its
// neighbours so the popup wins pointer hits. The popup never auto-flips
// above the anchor; Escape and click-outside close are the application's
// to handle (plain Escape is not routed to regions).
func (c *Context) Select(id ID, canvas *render.Canvas, bounds render.Rect, state *SelectState, options []string, style SelectStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	count := len(options)
	if count == 0 {
		state.Selected, state.Offset, state.Open = 0, 0, false
	}
	state.Selected = min(max(state.Selected, 0), max(count-1, 0))
	rows := state.popupRows(count)
	if state.Open {
		state.Offset = min(max(state.Offset, 0), max(count-rows, 0))
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}

	visible := bounds
	if state.Open {
		visible = render.Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1 + rows}
	}
	visible = visible.Intersect(canvas.Bounds())
	if state.Open {
		c.RegionWithOptions(id, canvas, visible, flags, RegionOptions{
			ContentHeight: 1 + min(rows, max(count-state.Offset, 0)),
			AtTop:         state.Offset <= 0,
			AtBottom:      state.Offset >= count-rows,
		})
	} else {
		c.RegionWithOptions(id, canvas, visible, flags, RegionOptions{ContentHeight: 1})
	}
	if visible.Empty() {
		return
	}
	canvas = canvas.Clip(visible)

	current := ""
	if count > 0 {
		current = options[state.Selected]
	}
	marker := style.Marker
	if marker == "" {
		marker = "▾"
	}
	theme := c.Theme()
	visual := themedStyle(style.Normal, theme.Text, style.Normal)
	switch {
	case style.Disabled:
		visual = themedStyle(style.DisabledStyle, theme.Danger, fallbackStyle(style.DisabledStyle, visual))
	case c.Pressed(id) || state.Open:
		visual = fallbackStyle(style.Pressed, visual)
	case c.Focused(id):
		visual = fallbackStyle(style.Focused, visual)
	case c.Hovered(id):
		visual = fallbackStyle(style.Hovered, visual)
	}
	canvas.Fill(render.Rect{X: visible.X, Y: visible.Y, W: visible.W, H: 1}, " ", visual)
	text := clipTextEllipsis(current, max(visible.W-2, 0), canvas.WidthPolicy(), "…")
	canvas.Text(visible.X+1, visible.Y, text, visual)
	if visible.W > 1 {
		canvas.Text(visible.X+visible.W-1, visible.Y, marker, visual)
	}

	if !state.Open {
		return
	}
	popup := render.Rect{X: visible.X, Y: visible.Y + 1, W: visible.W, H: rows}
	if popup.Empty() {
		return
	}
	popupStyle := themedStyle(style.Popup, theme.Muted, fallbackStyle(style.Popup, style.OptionNormal))
	canvas.Fill(popup, " ", popupStyle)
	optionNormal := themedStyle(style.OptionNormal, theme.Text, fallbackStyle(style.OptionNormal, style.Normal))
	for row := 0; row < popup.H; row++ {
		index := state.Offset + row
		if index >= count {
			break
		}
		optionVisual := optionNormal
		if index == state.Selected {
			optionVisual = themedStyle(style.OptionSelected, theme.Accent, fallbackStyle(style.OptionSelected, optionNormal))
			if c.Focused(id) {
				optionVisual = fallbackStyle(style.OptionFocused, optionVisual)
			}
		}
		y := popup.Y + row
		canvas.Fill(render.Rect{X: popup.X, Y: y, W: popup.W, H: 1}, " ", optionVisual)
		prefix := "  "
		if index == state.Selected {
			prefix = "> "
		}
		optionText := clipTextEllipsis(prefix+options[index], popup.W-2, canvas.WidthPolicy(), "…")
		canvas.Text(popup.X+1, y, optionText, optionVisual)
	}
}
