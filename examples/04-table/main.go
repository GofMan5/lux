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
	rows          []record
	order         []int
	observe       chan tabledemoState
}

type tabledemoState struct {
	rows       int
	selected   int
	offset     int
	sortActive bool
	sortColumn int
	sortDesc   bool
}

func newTabledemo(count int) *tabledemo {
	app := &tabledemo{
		rows:    seedRows(count),
		order:   make([]int, count),
		observe: make(chan tabledemoState, 16),
	}
	for i := range app.order {
		app.order[i] = i
	}
	return app
}

// applyTable routes one action into the table state and re-sorts the view
// when the widget reports a sort request.
func (app *tabledemo) applyTable(action ui.Action) {
	if _, _, sortRequested := app.table.Apply(action, tableColumns, len(app.order)); sortRequested {
		app.sortRows()
		app.table.SortRequested = false // request served: keep the state clean
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

func (app *tabledemo) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "table":
			app.applyTable(action)
		case "quit":
			return lux.Quit()
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

	if area.W < 44 || area.H < 12 {
		canvas.Text(0, 0, "LUX table - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX TABLE ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	style := ui.TableStyle{
		Normal:   render.Style{Foreground: ink, Background: background},
		Header:   render.Style{Foreground: muted, Background: background, Attrs: render.AttrUnderline},
		Selected: render.Style{Foreground: background, Background: violet},
		Focused:  render.Style{Foreground: background, Background: cyan},
	}
	body := render.Rect{X: 2, Y: 1, W: area.W - 4, H: area.H - 5}
	app.ui.Table("table", canvas, body, &app.table, tableColumns, len(app.order), app.row, style)

	sortInfo := "unsorted"
	if app.table.SortActive {
		direction := "asc"
		if app.table.SortDesc {
			direction = "desc"
		}
		sortInfo = fmt.Sprintf("%s %s", tableColumns[app.table.SortColumn].Title, direction)
	}
	status := fmt.Sprintf(" %d rows  row %d  sort %s ", len(app.order), app.table.Selected+1, sortInfo)
	canvas.Text(2, area.H-2, status, render.Style{Foreground: muted, Background: background})
	footer := " up/down move  wheel scroll  click select  click header sort  esc quit "
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
		rows:       len(app.order),
		selected:   app.table.Selected,
		offset:     app.table.Offset,
		sortActive: app.table.SortActive,
		sortColumn: app.table.SortColumn,
		sortDesc:   app.table.SortDesc,
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
