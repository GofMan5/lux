package ui

import (
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// TabsState is the persistent selection and strip offset for a Tabs strip.
// The zero value selects tab 0 with the strip scrolled to the start, and is
// ready to draw. Offset is the index of the first tab drawn in the strip; it
// moves only to keep the selected tab's slot fully inside the width.
type TabsState struct {
	Selected int
	Offset   int
}

// tabSlot is one tab's drawn rectangle inside the strip: x and w are relative
// to the strip's origin, so the same layout maps both drawing and pointer
// hits.
type tabSlot struct {
	index int
	x     int
	w     int
}

// tabLayout measures the visible strip for labels flowing from offset: each
// tab is its label plus one padding cell on each side, measured with the
// default width policy — the same measurement Apply maps presses through, so
// drawn slots and hit slots are the same rectangles under the default policy.
// Slots clip to the strip width; a tab wider than the whole strip still gets
// one clipped slot, never zero.
func tabLayout(labels []string, offset, width int) []tabSlot {
	if offset < 0 {
		offset = 0
	}
	slots := make([]tabSlot, 0, len(labels))
	x := 0
	for i := offset; i < len(labels) && x < width; i++ {
		w := render.TextWidth(labels[i]) + 2
		if x+w > width {
			w = width - x
		}
		slots = append(slots, tabSlot{index: i, x: x, w: w})
		x += w
	}
	return slots
}

// tabAt maps a strip-local x to a tab index, or -1 when x is outside every
// slot (the gap-free strip means this only happens past the last slot).
func tabAt(labels []string, offset, width, x int) int {
	for _, slot := range tabLayout(labels, offset, width) {
		if x >= slot.x && x < slot.x+slot.w {
			return slot.index
		}
	}
	return -1
}

// keepTabVisible minimally advances Offset until the selected tab's label
// fits fully inside width, or the offset reaches the tab (a tab wider than
// the strip then starts at the left edge and clips). Visibility is measured
// against the unclipped label width, because a clipped trailing slot always
// "fits" numerically while showing no label at all. Without geometry
// (width 0) there is nothing to keep visible, so it is a no-op.
func (s *TabsState) keepTabVisible(labels []string, width int) {
	if width <= 0 || s.Selected >= len(labels) || s.Selected < 0 {
		return
	}
	if s.Selected < s.Offset {
		s.Offset = s.Selected
		return
	}
	for s.Offset < s.Selected {
		x := 0
		for i := s.Offset; i < s.Selected; i++ {
			x += render.TextWidth(labels[i]) + 2
		}
		if x == 0 || x+render.TextWidth(labels[s.Selected])+2 <= width {
			break
		}
		s.Offset++
	}
}

// Apply mutates the selected tab for a routed Tabs action. Left/Right, Home,
// End, and wheel steps move the selection — a tab strip selects on
// navigation like a radio group, it has nothing to commit — and a press on a
// tab selects it directly; the closing release reports activated while
// keeping the choice, which is the app's page-switch signal. Vertical keys
// and page keys are inert (a one-row strip has no rows to move through), key
// releases are inert, and Apply registers the same labels the strip draws so
// pointer hits map through the identical layout. A label count of 0 resets
// the strip to tab 0.
func (s *TabsState) Apply(action Action, labels []string) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	before := *s
	count := len(labels)
	width := max(action.Bounds.W, 0)
	if count == 0 {
		s.Selected, s.Offset = 0, 0
		return before.Selected != 0 || before.Offset != 0, false
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	s.Offset = min(max(s.Offset, 0), count-1)

	switch action.Kind {
	case Activate:
		activated = true
	case Scroll:
		switch action.Event.Kind {
		case event.Key:
			switch action.Event.Key.Code {
			case event.KeyLeft:
				s.Selected--
			case event.KeyRight:
				s.Selected++
			case event.KeyHome:
				s.Selected = 0
			case event.KeyEnd:
				s.Selected = count - 1
			}
		case event.Mouse:
			switch action.Event.Mouse.Action {
			case event.MouseWheel:
				delta := 1
				switch action.Event.Mouse.Button {
				case event.MouseWheelUp, event.MouseWheelLeft:
					s.Selected -= delta
				case event.MouseWheelDown, event.MouseWheelRight:
					s.Selected += delta
				}
			case event.MousePress:
				if action.Y == 0 {
					if index := tabAt(labels, s.Offset, width, action.X); index >= 0 {
						s.Selected = index
					}
				}
			}
		}
	}
	s.Selected = min(max(s.Selected, 0), count-1)
	// Visibility healing runs for the actions that carry the strip's
	// geometry: routed Scrolls always do, while an Activate is a report and
	// must not move the strip.
	if action.Kind == Scroll {
		s.keepTabVisible(labels, width)
	}
	changed = *s != before
	return changed, activated
}

// TabsStyle defines tab strip states. Zero values inherit Normal. Visual
// states apply to the selected tab only; the other tabs always draw Normal,
// and hover styling is not claimed (the router reports hover per region, not
// per tab).
type TabsStyle struct {
	Normal        render.Style
	Selected      render.Style
	Focused       render.Style
	Hovered       render.Style
	Pressed       render.Style
	DisabledStyle render.Style
	Disabled      bool
}

// Tabs registers and draws one row of tabs. Each tab is its label padded by
// one cell on each side and clipped to the strip width; the selected tab
// draws with Selected (Focused while the strip owns focus), so the fill is
// the active-tab indicator. The strip is a Focusable+Scrollable+Activatable
// region: a press routes as a strip-local Scroll that selects the pressed
// tab, and the release activates. Not claimed: multi-row tab strips, a
// separate underline row, overflow markers, drag reordering.
func (c *Context) Tabs(id ID, canvas *render.Canvas, bounds render.Rect, state *TabsState, labels []string, style TabsStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	count := len(labels)
	state.Selected = min(max(state.Selected, 0), max(count-1, 0))
	if count == 0 {
		state.Selected, state.Offset = 0, 0
	} else {
		state.Offset = min(max(state.Offset, 0), count-1)
		state.keepTabVisible(labels, bounds.W)
	}
	slots := tabLayout(labels, state.Offset, bounds.W)
	atBottom := len(slots) == 0 || slots[len(slots)-1].index == count-1
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{
		ContentHeight: min(len(slots), 1),
		AtTop:         state.Offset <= 0,
		AtBottom:      atBottom,
	})
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	visual := style.Selected
	switch {
	case style.Disabled:
		visual = fallbackStyle(style.DisabledStyle, style.Normal)
	case c.Pressed(id):
		visual = fallbackStyle(style.Pressed, visual)
	case c.Focused(id):
		visual = fallbackStyle(style.Focused, visual)
	case c.Hovered(id):
		visual = fallbackStyle(style.Hovered, visual)
	}
	canvas = canvas.Clip(bounds)
	for _, slot := range slots {
		fill := style.Normal
		if slot.index == state.Selected {
			fill = visual
		}
		canvas.Fill(render.Rect{X: bounds.X + slot.x, Y: bounds.Y, W: slot.w, H: min(bounds.H, 1)}, " ", fill)
		if labelWidth := slot.w - 2; labelWidth > 0 {
			label := clipText(labels[slot.index], labelWidth, canvas.WidthPolicy())
			canvas.Text(bounds.X+slot.x+1, bounds.Y, label, fill)
		}
	}
}
