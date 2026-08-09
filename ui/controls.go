package ui

import (
	"strings"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/rivo/uniseg"
)

// ButtonStyle defines the four visual states of a button. Zero values inherit
// Normal; Disabled also disables interaction.
type ButtonStyle struct {
	Normal        render.Style
	Focused       render.Style
	Hovered       render.Style
	Pressed       render.Style
	DisabledStyle render.Style
	Border        render.Border
	Disabled      bool
}

// Button registers and draws one keyboard/pointer activatable control.
func (c *Context) Button(id ID, canvas *render.Canvas, bounds render.Rect, label string, style ButtonStyle) {
	if canvas == nil || bounds.Empty() {
		return
	}
	bounds = bounds.Intersect(canvas.Bounds())
	if bounds.Empty() {
		return
	}
	flags := Focusable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	c.Region(id, canvas, bounds, flags)
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
	canvas.Fill(bounds, " ", visual)
	border := style.Border
	if border == (render.Border{}) {
		border = render.RoundedBorder
	}
	canvas.Border(bounds, border, visual)
	inner := bounds.Inset(1, 1)
	if inner.Empty() {
		inner = bounds
	}
	label = clipText(label, inner.W)
	canvas.Text(inner.X+max((inner.W-render.TextWidth(label))/2, 0), inner.Y+max((inner.H-1)/2, 0), label, visual)
}

// ListState is the persistent selection and scroll position for a List.
type ListState struct {
	Selected int
	Offset   int
}

// Apply mutates selection/offset for a routed List action. rows is the number
// of visible item rows. Enter/space activates without moving selection.
func (s *ListState) Apply(action Action, count, rows int) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	beforeSelected, beforeOffset := s.Selected, s.Offset
	count, rows = max(count, 0), max(rows, 0)
	if count == 0 {
		s.Selected, s.Offset = 0, 0
		return beforeSelected != 0 || beforeOffset != 0, false
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	s.Offset = min(max(s.Offset, 0), max(count-rows, 0))
	beforeSelected, beforeOffset = s.Selected, s.Offset

	switch action.Kind {
	case Activate:
		activated = true
	case Scroll:
		switch action.Event.Kind {
		case event.Key:
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
				s.Selected -= max(rows, 1)
			case event.KeyPageDown:
				s.Selected += max(rows, 1)
			}
		case event.Mouse:
			switch action.Event.Mouse.Action {
			case event.MouseWheel:
				delta := max(rows/3, 1)
				switch action.Event.Mouse.Button {
				case event.MouseWheelUp, event.MouseWheelLeft:
					s.Selected -= delta
				case event.MouseWheelDown, event.MouseWheelRight:
					s.Selected += delta
				}
			case event.MousePress:
				if index := s.Offset + action.Y; action.Y >= 0 && action.Y < rows && index < count {
					s.Selected = index
				}
			}
		}
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	s.ensureVisible(count, rows)
	return beforeSelected != s.Selected || beforeOffset != s.Offset, activated
}

func (s *ListState) ensureVisible(count, rows int) {
	if rows <= 0 || count <= 0 {
		s.Offset = 0
		return
	}
	if s.Selected < s.Offset {
		s.Offset = s.Selected
	}
	if s.Selected >= s.Offset+rows {
		s.Offset = s.Selected - rows + 1
	}
	s.Offset = min(max(s.Offset, 0), max(count-rows, 0))
}

// ListStyle defines list row states. Zero values inherit Normal.
type ListStyle struct {
	Normal        render.Style
	Selected      render.Style
	Focused       render.Style
	Hovered       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Marker        string
}

// List registers and draws a focusable, scrollable list. Apply its routed
// actions in Model.Update; View is side-effect free except for bounds repair.
func (c *Context) List(id ID, canvas *render.Canvas, bounds render.Rect, state *ListState, items []string, style ListStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	bounds = bounds.Intersect(canvas.Bounds())
	if bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{ContentHeight: min(bounds.H, max(len(items)-state.Offset, 0))})
	state.Selected = min(max(state.Selected, 0), max(len(items)-1, 0))
	state.ensureVisible(len(items), bounds.H)
	canvas = canvas.Clip(bounds)
	canvas.Fill(bounds, " ", style.Normal)
	marker := style.Marker
	if marker == "" {
		marker = "> "
	}
	for row := 0; row < bounds.H; row++ {
		index := state.Offset + row
		if index >= len(items) {
			break
		}
		visual := style.Normal
		if style.Disabled {
			visual = fallbackStyle(style.DisabledStyle, visual)
		} else if index == state.Selected {
			visual = fallbackStyle(style.Selected, visual)
			if c.Focused(id) {
				visual = fallbackStyle(style.Focused, visual)
			} else if c.Hovered(id) {
				visual = fallbackStyle(style.Hovered, visual)
			}
		}
		y := bounds.Y + row
		canvas.Fill(render.Rect{X: bounds.X, Y: y, W: bounds.W, H: 1}, " ", visual)
		prefix := ""
		if index == state.Selected {
			prefix = marker
		}
		text := clipText(prefix+items[index], bounds.W)
		canvas.Text(bounds.X, y, text, visual)
	}
}

// ModalStyle defines the panel, border, and title styling of a modal.
type ModalStyle struct {
	Panel  render.Style
	Border render.Style
	Title  render.Style
	Frame  render.Border
}

// Modal draws a modal panel, begins its input scope, and returns its interior.
// Call EndModal after registering all modal controls.
func (c *Context) Modal(id ID, canvas *render.Canvas, bounds render.Rect, title string, style ModalStyle) *render.Canvas {
	if canvas == nil {
		return nil
	}
	bounds = bounds.Intersect(canvas.Bounds())
	c.BeginModal(id, canvas, bounds)
	canvas.Fill(bounds, " ", style.Panel)
	border := style.Frame
	if border == (render.Border{}) {
		border = render.RoundedBorder
	}
	canvas.Border(bounds, border, fallbackStyle(style.Border, style.Panel))
	if bounds.W > 4 && title != "" {
		canvas.Text(bounds.X+2, bounds.Y, " "+clipText(title, bounds.W-4)+" ", fallbackStyle(style.Title, style.Border))
	}
	return canvas.Subview(bounds.Inset(1, 1))
}

func fallbackStyle(preferred, fallback render.Style) render.Style {
	if preferred == (render.Style{}) {
		return fallback
	}
	return preferred
}

func clipText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if render.TextWidth(text) <= width {
		return text
	}
	var result strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		cluster := graphemes.Str()
		cells := render.GraphemeWidth(cluster)
		if used+cells > width {
			break
		}
		result.WriteString(cluster)
		used += cells
	}
	return result.String()
}
