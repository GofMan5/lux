package ui

import (
	"strings"

	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// treeIndents caches the two-spaces-per-depth indent prefixes so a row paint
// does not rebuild the same string every frame; deeper trees fall back to
// strings.Repeat.
var treeIndents = [9]string{
	"", "  ", "    ", "      ", "        ",
	"          ", "            ", "              ", "                ",
}

// TreeRow is one flattened, currently visible tree row. The application owns
// the source hierarchy and flattens it into rows for the frame: it emits a
// node only when its parent is expanded (see TreeState.IsExpanded), which is
// what makes the tree virtualized and honest — the widget never walks data
// it cannot see. ID keys the expansion set, Label is the drawn text, Depth
// sets two spaces of indent per level, and HasChildren marks rows whose
// activation toggles expansion instead of being a dead end.
type TreeRow struct {
	ID          string
	Label       string
	Depth       int
	HasChildren bool
}

// TreeState is the persistent selection, viewport, and expansion set of a
// Tree. The zero value is ready: nothing selected, viewport at the top,
// everything collapsed (a nil Expanded map holds no open nodes). The widget
// owns the expansion set because pointer and key activation live here; the
// application reads it back while flattening, so the same source data
// renders differently between two frames with no duplicated state.
type TreeState struct {
	Selected int
	Offset   int
	Expanded map[string]bool
}

// IsExpanded reports whether the node with the given ID is open. Always
// false for a zero-value state.
func (s *TreeState) IsExpanded(id string) bool {
	if s == nil {
		return false
	}
	return s.Expanded[id]
}

// Apply mutates state for a routed Tree action and reports whether anything
// changed and whether the selected row was activated. count is the total
// number of currently visible rows; rowAt supplies the row at an index so
// activation knows which node to toggle, and may be nil — then activation
// reports itself but toggles nothing. Visible rows derive from
// Action.Bounds, which routed actions always carry.
//
// Keys move the selection (arrows, pages, Home/End) and the viewport
// follows; the wheel moves the selection like a list, because tree rows are
// navigation targets, not a data grid. Left and Right are routed to the
// focused tree by the router's scroll keys but do nothing here: this tree
// is vertical only, and horizontal branches are not claimed. A pointer
// press selects the pressed row. Enter, Space, and a completed click
// activate the selected row: when it HasChildren, its ID is toggled in
// Expanded — the application must re-flatten its rows before the next
// frame, because the visible count may have changed. Key releases are
// inert.
func (s *TreeState) Apply(action Action, count int, rowAt func(index int) TreeRow) (changed, activated bool) {
	if s == nil {
		return false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false
	}
	beforeSelected, beforeOffset := s.Selected, s.Offset
	count, rows := max(count, 0), max(action.Bounds.H, 0)
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
		if rowAt == nil {
			break
		}
		if row := rowAt(s.Selected); row.HasChildren {
			if s.Expanded == nil {
				s.Expanded = make(map[string]bool, 1)
			}
			s.Expanded[row.ID] = !s.Expanded[row.ID]
			changed = true
		}
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
	changed = changed || beforeSelected != s.Selected || beforeOffset != s.Offset
	return changed, activated
}

// ensureVisible pulls the offset minimally toward the selected row, then
// clamps it to the content. Shared with List.
func (s *TreeState) ensureVisible(count, rows int) {
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

// TreeStyle defines tree row states. Zero values resolve through the
// context theme: Normal reads Text, Selected reads Accent, Focused reads
// Focus, DisabledStyle reads Danger; explicit styles always win. The branch
// markers default to ASCII ("+" collapsed, "-" expanded, "" leaf) so the
// tree aligns on any terminal, including CJK-ambiguous-width renderers;
// restyle them to "▸"/"▾" when the renderer is trusted.
type TreeStyle struct {
	Normal        render.Style
	Selected      render.Style
	Focused       render.Style
	Hovered       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Collapsed     string
	Expanded      string
	Leaf          string
}

// Tree registers and draws a virtualized tree. rowAt supplies the visible
// row at an index and is called only for rows inside the viewport; count is
// the total visible row count. When the canvas's clip hides rows of bounds,
// rowAt is called only for rows the clip can still paint, so a lazy row
// provider follows the visible band, not the logical bounds. Rows are drawn
// as two spaces of indent per Depth, the branch marker, one space, then the
// label, clipped to the width like any text. Apply the same rowAt in
// Model.Update so activation toggles the row the user actually sees. View is
// side-effect free except for bounds repair (the selection and offset
// clamps).
func (c *Context) Tree(id ID, canvas *render.Canvas, bounds render.Rect, state *TreeState, count int, rowAt func(index int) TreeRow, style TreeStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	state.Selected = min(max(state.Selected, 0), max(count-1, 0))
	if count == 0 {
		state.Selected, state.Offset = 0, 0
	}
	state.ensureVisible(count, bounds.H)
	collapsed, expanded, leaf := style.Collapsed, style.Expanded, style.Leaf
	if collapsed == "" {
		collapsed = "+"
	}
	if expanded == "" {
		expanded = "-"
	}
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{
		ContentHeight: min(bounds.H, max(count-state.Offset, 0)),
		AtTop:         state.Offset <= 0,
		AtBottom:      state.Offset >= max(count-bounds.H, 0),
	})
	drawable := bounds.Intersect(canvas.DrawableRect())
	if drawable.Empty() {
		return
	}
	theme := c.Theme()
	body := themedStyle(style.Normal, theme.Text, style.Normal)
	canvas = canvas.Clip(bounds)
	canvas.Fill(drawable, " ", body)
	policy := canvas.WidthPolicy()
	// Rows the clip hides cost nothing: rowAt prices drawable rows only, so
	// lazy row providers follow the viewport, not the logical bounds.
	rowFirst, rowLast := drawableRowBand(canvas, bounds)
	for row := rowFirst; row < rowLast; row++ {
		index := state.Offset + row
		if index >= count {
			break
		}
		var current TreeRow
		if rowAt != nil {
			current = rowAt(index)
		}
		visual := body
		if style.Disabled {
			visual = themedStyle(style.DisabledStyle, theme.Danger, fallbackStyle(style.DisabledStyle, visual))
		} else if index == state.Selected {
			visual = themedStyle(style.Selected, theme.Accent, fallbackStyle(style.Selected, visual))
			if c.Focused(id) {
				visual = themedStyle(style.Focused, theme.Focus, visual)
			} else if c.Hovered(id) {
				visual = fallbackStyle(style.Hovered, visual)
			}
		}
		y := bounds.Y + row
		canvas.Fill(render.Rect{X: bounds.X, Y: y, W: bounds.W, H: 1}, " ", visual)
		marker := leaf
		if current.HasChildren {
			if state.Expanded[current.ID] {
				marker = expanded
			} else {
				marker = collapsed
			}
		}
		indent := ""
		if current.Depth < len(treeIndents) {
			indent = treeIndents[current.Depth]
		} else {
			indent = strings.Repeat("  ", current.Depth)
		}
		canvas.Text(bounds.X, y, clipText(indent+marker+" "+current.Label, bounds.W, policy), visual)
	}
}
