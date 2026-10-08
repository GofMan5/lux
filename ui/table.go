package ui

import (
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// TableColumn declares one table column. Width >= 1 pins its cell count; a
// zero Width makes the column rubber: the width left after fixed columns and
// gutters is split across rubber columns in proportion to Weight (default 1,
// also for Weight 0). A rubber column collapses to zero cells when no space
// is left, and the canvas clips columns that together exceed the table.
type TableColumn struct {
	Title  string
	Width  int
	Weight int
}

// ColumnLayout resolves the cell width of every column at the given total
// table width. One cell is reserved as a gutter between each pair of columns.
// The slice entries may be zero for collapsed rubber columns; the layout is a
// pure function, so the same numbers drive drawing and pointer hit mapping.
func ColumnLayout(columns []TableColumn, width int) []int {
	count := len(columns)
	if count == 0 {
		return nil
	}
	widths := make([]int, count)
	fixed, weightTotal := 0, 0
	for i, column := range columns {
		if column.Width >= 1 {
			widths[i] = column.Width
			fixed += column.Width
		} else {
			weightTotal += max(column.Weight, 1)
		}
	}
	budget := width - fixed - (count - 1)
	if budget <= 0 || weightTotal == 0 {
		// Rubber columns collapse when the fixed columns and gutters already
		// fill the width.
		return widths
	}
	for i := range widths {
		if columns[i].Width < 1 && budget > 0 {
			widths[i] = 1
			budget--
		}
	}
	if budget > 0 {
		spent, last := 0, -1
		for i := range widths {
			if columns[i].Width < 1 && widths[i] >= 1 {
				extra := budget * max(columns[i].Weight, 1) / weightTotal
				widths[i] += extra
				spent += extra
				last = i
			}
		}
		// The floor division above can leave cells unassigned; they extend the
		// last rubber column so the row still spans the full width.
		if last >= 0 {
			widths[last] += budget - spent
		}
	}
	return widths
}

// TableState is the persistent selection, viewport, and sort request of a
// Table. The zero value is ready: nothing selected, viewport at the top, no
// sort requested. The table itself never reorders rows — it records a header
// press in SortColumn/SortDesc and sets SortRequested; the application owns
// the data, re-slices it, and clears SortRequested when the request is served.
type TableState struct {
	Selected      int
	Offset        int
	SortActive    bool
	SortColumn    int
	SortDesc      bool
	SortRequested bool
}

// Apply mutates state for a routed Table action and reports whether anything
// changed, whether the current row was activated (Enter/Space), and whether a
// header press requested a sort. count is the total row count; the visible
// body rows derive from Action.Bounds, which routed actions always carry.
//
// Keys move the selection (arrows, pages, Home/End); the wheel moves only the
// viewport, so a table over a large data set scrolls without dragging the
// selection along. A pointer press on the body selects that row; on the header
// it toggles the sort request for the hit column (asc, then desc, then asc
// again). Key releases are inert. Clicks on the header need at least one body
// row to be routed, because the router drops presses below the content.
func (s *TableState) Apply(action Action, columns []TableColumn, count int) (changed, activated, sortRequested bool) {
	if s == nil {
		return false, false, false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false, false, false
	}
	before := *s
	count = max(count, 0)
	rows := max(action.Bounds.H-tableHeaderRows(columns), 0)
	if count == 0 {
		s.Selected, s.Offset = 0, 0
	} else {
		s.Selected = min(max(s.Selected, 0), count-1)
		s.Offset = min(max(s.Offset, 0), max(count-rows, 0))
	}
	viewportOnly := false

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
				// The wheel scrolls the viewport only; the selection stays on
				// its row, and the viewport is not pulled back to it.
				viewportOnly = true
				delta := max(rows/3, 1)
				switch action.Event.Mouse.Button {
				case event.MouseWheelUp, event.MouseWheelLeft:
					s.Offset -= delta
				case event.MouseWheelDown, event.MouseWheelRight:
					s.Offset += delta
				}
			case event.MousePress:
				if tableHeaderRows(columns) > 0 && action.Y == 0 {
					if column := columnAt(columns, action.Bounds.W, action.X); column >= 0 {
						if s.SortActive && s.SortColumn == column {
							s.SortDesc = !s.SortDesc
						} else {
							s.SortActive, s.SortColumn, s.SortDesc = true, column, false
						}
						s.SortRequested = true
						sortRequested = true
					}
				} else if body := action.Y - tableHeaderRows(columns); body >= 0 && body < rows {
					if index := s.Offset + body; index < count {
						s.Selected = index
					}
				}
			}
		}
	}
	if count == 0 {
		s.Selected, s.Offset = 0, 0
	} else {
		s.Selected = min(max(s.Selected, 0), count-1)
		if viewportOnly {
			s.Offset = min(max(s.Offset, 0), max(count-rows, 0))
		} else {
			s.Offset = keepRowVisible(s.Selected, s.Offset, count, rows)
		}
	}
	changed = *s != before
	return changed, activated, sortRequested
}

// keepRowVisible minimally adjusts offset until the selected row is inside the
// viewport, then clamps it to the content.
func keepRowVisible(selected, offset, count, rows int) int {
	if rows <= 0 || count <= 0 {
		return 0
	}
	selected = min(max(selected, 0), count-1)
	if selected < offset {
		offset = selected
	}
	if selected >= offset+rows {
		offset = selected - rows + 1
	}
	return min(max(offset, 0), max(count-rows, 0))
}

func tableHeaderRows(columns []TableColumn) int {
	for _, column := range columns {
		if column.Title != "" {
			return 1
		}
	}
	return 0
}

// columnAt maps a pointer x position (region-local cells) to a column index,
// or -1 when the press lands on a gutter or a collapsed column.
func columnAt(columns []TableColumn, width, x int) int {
	if x < 0 {
		return -1
	}
	position := 0
	for i, cells := range ColumnLayout(columns, width) {
		if cells > 0 && x >= position && x < position+cells {
			return i
		}
		position += cells + 1
	}
	return -1
}

// TableStyle defines table row states. Zero values resolve through the
// context theme: Normal reads Text, Header reads Muted (plus an underline
// either way), Selected reads Accent, DisabledStyle reads Danger; explicit
// styles always win. Ellipsis defaults to the single ellipsis codepoint,
// clipped to the column like any other text.
type TableStyle struct {
	Normal        render.Style
	Header        render.Style
	Selected      render.Style
	Focused       render.Style
	Hovered       render.Style
	DisabledStyle render.Style
	Disabled      bool
	Ellipsis      string
}

// Table registers and draws a virtualized table. rowAt supplies the cells of
// one row and is called only for the rows currently inside the viewport, so a
// million-row table costs the same as a full one; count is the total row
// count. Cells are clipped to their column at grapheme boundaries and
// elided with Ellipsis when they do not fit. Register the same columns in
// Apply so pointer hits map identically. View is side-effect free except for
// bounds repair (the selection and offset clamps).
func (c *Context) Table(id ID, canvas *render.Canvas, bounds render.Rect, state *TableState, columns []TableColumn, count int, rowAt func(index int) []string, style TableStyle) {
	if canvas == nil || state == nil || bounds.Empty() {
		return
	}
	flags := Focusable | Scrollable | Activatable
	if style.Disabled {
		flags |= Disabled
	}
	header := tableHeaderRows(columns)
	rows := max(bounds.H-header, 0)
	state.Selected = min(max(state.Selected, 0), max(count-1, 0))
	if count == 0 {
		state.Selected, state.Offset = 0, 0
	} else {
		state.Offset = keepRowVisible(state.Selected, state.Offset, count, rows)
	}
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{
		ContentHeight: min(rows, max(count-state.Offset, 0)),
		AtTop:         state.Offset <= 0,
		AtBottom:      state.Offset >= max(count-rows, 0),
	})
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	theme := c.Theme()
	body := themedStyle(style.Normal, theme.Text, style.Normal)
	canvas = canvas.Clip(bounds)
	canvas.Fill(bounds, " ", body)
	policy := canvas.WidthPolicy()
	ellipsis := style.Ellipsis
	if ellipsis == "" {
		ellipsis = "…"
	}
	widths := ColumnLayout(columns, bounds.W)

	if header > 0 {
		// The header row is structure: it always underlines, and a zero
		// Header style resolves through the theme's Muted before falling
		// back to Normal.
		headerVisual := style.Header
		if headerVisual == (render.Style{}) {
			if muted := theme.Muted; muted != (render.Style{}) {
				muted.Attrs |= render.AttrUnderline
				headerVisual = muted
			} else {
				headerVisual = body
				headerVisual.Attrs |= render.AttrUnderline
			}
		}
		headerRect := render.Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1}
		canvas.Fill(headerRect, " ", headerVisual)
		x := bounds.X
		for i, column := range columns {
			if i >= len(widths) {
				break
			}
			cells := widths[i]
			if cells > 0 {
				title := column.Title
				if state.SortActive && i == state.SortColumn {
					indicator := "▲"
					if state.SortDesc {
						indicator = "▼"
					}
					title += " " + indicator
				}
				canvas.Text(x, bounds.Y, clipTextEllipsis(title, cells, policy, ellipsis), headerVisual)
			}
			x += cells + 1
		}
	}

	for row := 0; row < rows; row++ {
		index := state.Offset + row
		if index >= count {
			break
		}
		visual := body
		if style.Disabled {
			visual = themedStyle(style.DisabledStyle, theme.Danger, fallbackStyle(style.DisabledStyle, visual))
		} else if index == state.Selected {
			visual = themedStyle(style.Selected, theme.Accent, fallbackStyle(style.Selected, visual))
			if c.Focused(id) {
				visual = fallbackStyle(style.Focused, visual)
			} else if c.Hovered(id) {
				visual = fallbackStyle(style.Hovered, visual)
			}
		}
		y := bounds.Y + header + row
		canvas.Fill(render.Rect{X: bounds.X, Y: y, W: bounds.W, H: 1}, " ", visual)
		if rowAt == nil {
			continue
		}
		cells := rowAt(index)
		x := bounds.X
		for i := range columns {
			if i >= len(widths) || i >= len(cells) {
				break
			}
			columnCells := widths[i]
			if columnCells > 0 {
				canvas.Text(x, y, clipTextEllipsis(cells[i], columnCells, policy, ellipsis), visual)
			}
			x += columnCells + 1
		}
	}
}
