// Command table demonstrates the virtualized Table: 200k synthetic rows
// with fixed and rubber columns, column sorting driven by header clicks,
// keyboard and wheel navigation, and per-row selection.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	cyan       = render.RGBColor(34, 211, 238)
	background = render.RGBColor(8, 10, 18)
)

// tableColumns is package-level so the routed Apply and the draw call share
// the exact same slice: header hit-testing derives from the drawn layout.
var tableColumns = []ui.TableColumn{
	{Title: "ID", Width: 7},
	{Title: "NAME", Weight: 3},
	{Title: "STATE", Weight: 2},
	{Title: "CPU%", Width: 5},
	{Title: "MEM", Width: 8},
}

// record is one synthetic process row. The names mix ASCII, a combining
// pair, CJK, and an emoji so the grapheme path stays exercised in the demo.
type record struct {
	id    int
	name  string
	state string
	cpu   float64
	mem   int64
}

// rowCount is the demo default; tests shrink it.
const rowCount = 200_000

// seedRows builds a deterministic, varied dataset without a PRNG: a
// multiplicative hash of the index spreads names, states, and load.
func seedRows(count int) []record {
	names := []string{"relay", "feed\u00e9\u0301d", "\u4e16\u754c watcher", "collector", "probe", "\U0001f600 pool", "scheduler", "vault"}
	states := []string{"running", "sleep", "idle", "stopped"}
	rows := make([]record, count)
	for i := range rows {
		h := uint64(i+1) * 0x9E3779B97F4A7C15
		h ^= h >> 30
		h *= 0xBF58476D1CE4E5B9
		h ^= h >> 27
		rows[i] = record{
			id:    i + 1,
			name:  names[int(h%uint64(len(names)))],
			state: states[int((h>>8)%uint64(len(states)))],
			cpu:   float64(h%1000) / 10,
			mem:   int64(h % 9_000_000),
		}
	}
	return rows
}

type tabledemo struct {
	width, height int
	ui            ui.Context
	table         ui.TableState
	filter        ui.TextInput
	rows          []record
	order         []int
	// anchorID is the record id under the cursor. The view index is
	// transient — sorts and filters re-order it — so identity lives here
	// and every view rebuild re-anchors the cursor onto this row.
	anchorID int
	// viewRows is the body row count the last View painted; re-anchoring
	// uses it to keep a visible row visible. Zero before the first paint.
	viewRows int
	observe  chan tabledemoState
}

type tabledemoState struct {
	// rows is the total record count; matches is the live view size. They
	// diverge once a filter narrows the view, and tests watch both.
	rows        int
	matches     int
	selected    int
	offset      int
	anchor      int
	filter      string
	filterFocus bool
	sortActive  bool
	sortColumn  int
	sortDesc    bool
}

func newTabledemo(count int) *tabledemo {
	app := &tabledemo{
		rows:    seedRows(count),
		order:   make([]int, count),
		filter:  ui.TextInput{Placeholder: "filter name or state"},
		observe: make(chan tabledemoState, 16),
	}
	for i := range app.order {
		app.order[i] = i
	}
	if count > 0 {
		app.anchorID = app.rows[0].id
	}
	return app
}

// applyTable routes one action into the table state and re-sorts the view
// when the widget reports a sort request. A sort re-orders the view, so the
// cursor is re-anchored onto the row it selected; any other move keeps the
// anchor in step with the cursor.
func (app *tabledemo) applyTable(action ui.Action) {
	anchor := app.anchorID
	selected := app.table.Selected
	if _, _, sortRequested := app.table.Apply(action, tableColumns, len(app.order)); sortRequested {
		app.sortRows()
		app.table.SortRequested = false // request served: keep the state clean
		app.reanchor(anchor)
		return
	}
	if app.table.Selected != selected && len(app.order) > 0 {
		// An explicit navigation or click: identity follows the cursor.
		app.anchorID = app.rows[app.order[app.table.Selected]].id
	}
}

// reanchor restores the cursor onto the data row the user had selected —
// identity is the row, the view index is transient. A row that left the
// view cannot hold the cursor: the cursor clamps to a valid index and the
// anchor becomes whatever row it lands on, because that is the row the
// user now sees selected.
func (app *tabledemo) reanchor(anchor int) {
	for i, idx := range app.order {
		if app.rows[idx].id == anchor {
			app.table.Selected = i
			app.anchorID = anchor
			app.clampOffsetToSelection()
			return
		}
	}
	app.clampSelection()
	if len(app.order) > 0 {
		app.anchorID = app.rows[app.order[app.table.Selected]].id
	}
}

// clampSelection bounds the cursor to the view; a shrink (filter applied,
// rows leaving) can leave the old index past the end.
func (app *tabledemo) clampSelection() {
	if app.table.Selected >= len(app.order) {
		app.table.Selected = max(len(app.order)-1, 0)
	}
	if app.table.Selected < 0 {
		app.table.Selected = 0
	}
}

// clampOffsetToSelection keeps the re-anchored cursor on screen: the user
// was looking at the row they selected, and a re-sort must not scroll it
// out from under them.
func (app *tabledemo) clampOffsetToSelection() {
	if app.viewRows <= 1 {
		return
	}
	if app.table.Selected < app.table.Offset {
		app.table.Offset = app.table.Selected
	}
	if last := app.table.Offset + app.viewRows - 1; app.table.Selected > last {
		app.table.Offset = app.table.Selected - app.viewRows + 1
	}
}

// sortRows re-orders the index view. The widget never reorders data itself;
// the app owns the order, which keeps Table honest for million-row sources.
func (app *tabledemo) sortRows() {
	column, desc := app.table.SortColumn, app.table.SortDesc
	compare := func(x, y record) int {
		switch column {
		case 0:
			switch {
			case x.id < y.id:
				return -1
			case x.id > y.id:
				return 1
			}
		case 3:
			switch {
			case x.cpu < y.cpu:
				return -1
			case x.cpu > y.cpu:
				return 1
			}
		case 4:
			switch {
			case x.mem < y.mem:
				return -1
			case x.mem > y.mem:
				return 1
			}
		default:
			switch {
			case x.name < y.name:
				return -1
			case x.name > y.name:
				return 1
			case x.state < y.state:
				return -1
			case x.state > y.state:
				return 1
			}
		}
		return 0
	}
	sort.SliceStable(app.order, func(i, j int) bool {
		a, b := app.rows[app.order[i]], app.rows[app.order[j]]
		if desc {
			a, b = b, a
		}
		return compare(a, b) < 0
	})
}

// row materializes one row; Table calls it only for the visible window.
func (app *tabledemo) row(index int) []string {
	r := app.rows[app.order[index]]
	return []string{
		strconv.Itoa(r.id),
		r.name,
		r.state,
		fmt.Sprintf("%.1f", r.cpu),
		fmt.Sprintf("%.1fM", float64(r.mem)/1e6),
	}
}

// refilter rebuilds the view around the current query — the filter first,
// then the standing sort — and re-anchors the cursor onto its row, so
// narrowing the view never steals the selection.
func (app *tabledemo) refilter() {
	app.order = app.order[:0]
	query := strings.ToLower(app.filter.Value)
	for i := range app.rows {
		if filterMatches(app.rows[i], query) {
			app.order = append(app.order, i)
		}
	}
	if app.table.SortActive {
		app.sortRows()
	}
	app.reanchor(app.anchorID)
}

// filterMatches reports whether a row survives a query. An empty query is
// the absence of a filter, not a query for nothing: everything matches.
func filterMatches(r record, query string) bool {
	return query == "" ||
		strings.Contains(strings.ToLower(r.name), query) ||
		strings.Contains(strings.ToLower(r.state), query)
}

// setFilter installs a whole query at once (the Escape clear) and rebuilds
// the view around it.
func (app *tabledemo) setFilter(query string) {
	app.filter.SetValue(query)
	app.refilter()
}

func (app *tabledemo) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch {
		case action.Target == "quit" && action.Kind == ui.Activate:
			// Escape over a live query is a filter clear, not an exit —
			// the user is refining a view, not leaving the app. Only a
			// bare Escape on an empty query quits.
			if app.filter.Value != "" {
				app.setFilter("")
				return nil
			}
			return lux.Quit()
		case action.Target == "filter" && action.Kind == ui.Focus:
			// The '/' shortcut asks for the field; focus movement is the
			// app's own call, granted here.
			app.ui.Focus("filter")
		case action.Target == "filter" && action.Kind == ui.Input:
			if app.filter.Apply(action) {
				app.refilter()
			}
			if action.Event.Kind == event.Key && action.Event.Key.Code == event.KeyEnter {
				// Enter commits the query and hands focus back to the
				// results: navigation resumes.
				app.ui.Focus("table")
			}
		case action.Target == "table":
			app.applyTable(action)
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Key:
		if next.Key.Code == event.KeyEscape {
			return lux.Quit()
		}
	}
	return nil
}

func (app *tabledemo) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	// Horizontal scrolling is deliberately absent: the five-column set fits
	// the 44-cell floor this guard already enforces, so the honest layout
	// is a wider terminal, not a half-hidden row. Revisit only when a real
	// column set outgrows the width.
	if area.W < 44 || area.H < 12 {
		canvas.Text(0, 0, "LUX table - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX TABLE ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	// The table registers before the filter on purpose: the data is the
	// primary surface, so it keeps the initial focus and the first Tab
	// position, and refinement is one Tab away.
	style := ui.TableStyle{
		Normal:   render.Style{Foreground: ink, Background: background},
		Header:   render.Style{Foreground: muted, Background: background, Attrs: render.AttrUnderline},
		Selected: render.Style{Foreground: background, Background: violet},
		Focused:  render.Style{Foreground: background, Background: cyan},
	}
	body := render.Rect{X: 2, Y: 2, W: area.W - 4, H: area.H - 6}
	app.viewRows = max(body.H-1, 0) // minus the header row the table paints
	app.ui.Table("table", canvas, body, &app.table, tableColumns, len(app.order), app.row, style)

	filterStyle := ui.InputStyle{
		Normal:      render.Style{Foreground: ink, Background: background},
		Placeholder: render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim},
	}
	app.ui.Input("filter", canvas, render.Rect{X: 2, Y: 1, W: area.W - 4, H: 1}, &app.filter, filterStyle)
	// '/' jumps to the filter from anywhere the table has focus; it is an
	// app-scope binding without priority so a '/' typed into the query
	// itself still reaches the editor as text.
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: '/'}, Target: "filter", Kind: ui.Focus, Scope: ui.ScopeApp})

	// An empty view tells the truth: it is the query that matched nothing,
	// not a data outage, and the row under the cursor is gone.
	if len(app.order) == 0 && app.filter.Value != "" {
		hint := fmt.Sprintf(" no rows match %q ", app.filter.Value)
		x := max((area.W-canvas.TextWidth(hint))/2, 2)
		canvas.Text(x, 2, hint, render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim})
	}

	sortInfo := "unsorted"
	if app.table.SortActive {
		direction := "asc"
		if app.table.SortDesc {
			direction = "desc"
		}
		sortInfo = fmt.Sprintf("%s %s", tableColumns[app.table.SortColumn].Title, direction)
	}
	// The row counter reads `1` for the first row, `0` for none, and `-`
	// only when the view is empty and no cursor exists at all.
	rowInfo := fmt.Sprintf("row %d", app.table.Selected+1)
	if len(app.order) == 0 {
		rowInfo = "row -"
	}
	status := fmt.Sprintf(" %d rows  %s  sort %s ", len(app.order), rowInfo, sortInfo)
	if app.filter.Value != "" {
		status = fmt.Sprintf(" %d match %q  %s  sort %s ", len(app.order), app.filter.Value, rowInfo, sortInfo)
	}
	canvas.Text(2, area.H-2, status, render.Style{Foreground: muted, Background: background})
	footer := " up/down move  wheel scroll  click select  click header sort  / filter  esc quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})
	app.report()
}

func (app *tabledemo) report() {
	if app.observe == nil {
		return
	}
	select {
	case app.observe <- tabledemoState{
		rows:        len(app.rows),
		matches:     len(app.order),
		selected:    app.table.Selected,
		offset:      app.table.Offset,
		anchor:      app.anchorID,
		filter:      app.filter.Value,
		filterFocus: app.ui.Focused("filter"),
		sortActive:  app.table.SortActive,
		sortColumn:  app.table.SortColumn,
		sortDesc:    app.table.SortDesc,
	}:
	default:
	}
}

func main() {
	if err := lux.Run(context.Background(), newTabledemo(rowCount)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
