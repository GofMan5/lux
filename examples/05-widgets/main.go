// Command widgets is the stage-8 control catalog: one screen, one live
// instance of every ui widget this stage lands. Table shipped in slice 1;
// checkbox, radio, and select/dropdown joined in slice 2; tabs, gauge, and
// spinner landed in slice 3; sparkline and tree land in slice 4. The tabs
// strip above the controls panel pages that panel: the table page explains
// the catalog, the form page holds the input widgets, the motion page the
// live display widgets, and the tree page a file tree.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The palette moved to the library after two real screens converged on
// it: ui.ReferencePalette is now the single source, and the local names
// below alias it so the chrome and glyph styles stay short.
var (
	palette    = ui.ReferencePalette()
	ink        = palette.Ink
	muted      = palette.Muted
	violet     = palette.Violet
	cyan       = palette.Cyan
	background = palette.Background
)

// tableColumns is package-level so the routed Apply and the draw call share
// the exact same slice: header hit-testing derives from the drawn layout.
var tableColumns = []ui.TableColumn{
	{Title: "WIDGET", Weight: 2},
	{Title: "STATE"},
	{Title: "SLICE", Width: 5},
}

// catalogRows is the widget inventory this catalog demos. It doubles as the
// table's data source, so the demo table documents the catalog itself.
var catalogRows = [][3]string{
	{"Table", "live", "1"},
	{"Select/Dropdown", "live", "2"},
	{"Checkbox/Radio", "live", "2"},
	{"Tabs", "live", "3"},
	{"Gauge/Progress", "live", "3"},
	{"Spinner", "live", "3"},
	{"Sparkline", "live", "4"},
	{"Tree", "live", "4"},
}

// stations feeds the radio demo; modes feeds the select demo; pages feeds
// the tabs strip, one label per controls-panel page.
var stations = []string{"ink", "violet", "amber"}
var modes = []string{"live", "drag", "paint"}
var pages = []string{"table", "form", "motion", "tree"}

// treeEntry is one node of the static demo file tree. The tree widget is
// virtualized: the catalog flattens these nodes into the rows that are
// actually visible each frame, driven by TreeState.Expanded.
type treeEntry struct {
	id, label string
	children  []treeEntry
}

// fileTree is the tree page's data: the lux source layout.
var fileTree = []treeEntry{
	{id: "ui", label: "ui", children: []treeEntry{
		{id: "ui/table.go", label: "table.go"},
		{id: "ui/tree.go", label: "tree.go"},
		{id: "ui/sparkline.go", label: "sparkline.go"},
	}},
	{id: "examples", label: "examples", children: []treeEntry{
		{id: "examples/widgets", label: "widgets", children: []treeEntry{
			{id: "examples/widgets/main.go", label: "main.go"},
		}},
	}},
}

// flattenTree expands the nodes whose parent chain is open into the rows the
// tree widget draws. It is a pure function of the expansion set, so the same
// source data renders differently between two frames.
func flattenTree(entries []treeEntry, expanded func(id string) bool, depth int, rows []ui.TreeRow) []ui.TreeRow {
	for _, entry := range entries {
		rows = append(rows, ui.TreeRow{ID: entry.id, Label: entry.label, Depth: depth, HasChildren: len(entry.children) > 0})
		if len(entry.children) > 0 && expanded(entry.id) {
			rows = flattenTree(entry.children, expanded, depth+1, rows)
		}
	}
	return rows
}

// treeRows snapshots the visible rows for the current expansion set.
func (app *catalog) treeRows() []ui.TreeRow {
	return flattenTree(fileTree, app.tree.IsExpanded, 0, nil)
}

type catalog struct {
	width, height int
	ui            ui.Context
	table         ui.TableState
	telemetry     ui.CheckboxState
	palette       ui.RadioState
	motion        ui.SelectState
	pages         ui.TabsState
	spinner       ui.SpinnerState
	tree          ui.TreeState
	// load is the motion page's live sparkline series: a slow sine wave
	// appended on a wall-clock guard, capped so the series stops growing
	// and only slides.
	load     []float64
	loadStep int
	lastLoad time.Time
	// keys counts the key events Update has seen. The report carries it
	// so a test can prove a keystroke reached the model — routed against
	// whatever routing state existed at that moment — instead of guessing
	// from clock timing.
	keys    int
	observe chan catalogState
}

type catalogState struct {
	selected int
	offset   int
	checked  bool
	station  int
	choice   int
	open     bool
	tab      int
	treeSel  int
	treeOpen int
	keys     int
}

func newCatalog() *catalog {
	// Seed the sparkline so the motion page paints a full wave even in a
	// single-frame test: half a sine cycle across sixteen points.
	app := &catalog{
		// The demo select keeps its popup small: three rows of modes inside
		// the controls panel.
		motion: ui.SelectState{Selected: 2, PopupRows: 3},
		// The spinner runs on the wall clock; its glyph is a pure function
		// of absolute time, so no frame counter is needed here.
		spinner: ui.SpinnerState{Started: time.Now()},
		observe: make(chan catalogState, 16),
	}
	for i := 0; i < 16; i++ {
		app.load = append(app.load, .5+.5*math.Sin(float64(i)*2*math.Pi/16))
	}
	// The palette is named once, in the library, as a theme: every
	// selection widget below (table, tabs, checkbox, radio, select,
	// tree) resolves its zero styles through the roles — Text body,
	// Muted structure, Accent selection, Focus focus. The glyph widgets
	// (gauge, spinner, sparkline) keep explicit styles in View: their
	// colors are data, not selection. Danger stays unset: this screen
	// has no disabled controls to show.
	app.ui.UseTheme(palette.Theme())
	return app
}

func (app *catalog) Update(next event.Event) lux.Command {
	if next.Kind == event.Key {
		app.keys++
	}
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "widgets-table":
			app.table.Apply(action, tableColumns, len(catalogRows))
		case "telemetry":
			app.telemetry.Apply(action)
		case "palette":
			app.palette.Apply(action, len(stations))
		case "motion":
			app.motion.Apply(action, len(modes))
		case "pages":
			app.pages.Apply(action, pages)
		case "files":
			rows := app.treeRows()
			app.tree.Apply(action, len(rows), func(index int) ui.TreeRow { return rows[index] })
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

func (app *catalog) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	if area.W < 62 || area.H < 16 {
		canvas.Text(0, 0, "LUX widgets - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX WIDGET CATALOG ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	body := render.Rect{X: 2, Y: 1, W: 36, H: area.H - 5}
	// Zero styles: the theme roles carry the palette. The table reads Text
	// for body, Muted (auto-underlined) for the header, Accent for the
	// selected row, Focus for the focused row.
	app.ui.Table("widgets-table", canvas, body, &app.table, tableColumns, len(catalogRows),
		func(index int) []string { return catalogRows[index][:] },
		ui.TableStyle{})

	// The controls panel is paged by the tabs strip above it: each page
	// demos one family instead of stacking all widgets in one column. The
	// form page keeps the coordinates the slice-2 tests locked in.
	panelX, panelW := 40, area.W-42
	canvas.Text(panelX, 1, "CONTROLS", render.Style{Foreground: muted, Background: background, Attrs: render.AttrBold})
	// Zero styles: the selected tab reads the Accent block, unselected
	// tabs read Muted — the strip gains structure/selection hierarchy the
	// explicit ink styles never had.
	app.ui.Tabs("pages", canvas, render.Rect{X: panelX, Y: 2, W: panelW, H: 1}, &app.pages, pages, ui.TabsStyle{})

	switch app.pages.Selected {
	case 1: // form: the input widgets at their slice-2 coordinates.
		// Zero styles: the row reads Text, focus reads Focus, and the
		// marker reads Accent — the marker IS the checked state, so it
		// paints the selection block the checkbox previously missed.
		app.ui.Checkbox("telemetry", canvas, render.Rect{X: panelX, Y: 3, W: panelW, H: 1}, &app.telemetry, "stream telemetry", ui.CheckboxStyle{})
		// Zero styles: the selected station row reads the Accent block,
		// the others read Text.
		app.ui.RadioGroup("palette", canvas, render.Rect{X: panelX, Y: 5, W: panelW, H: 3}, &app.palette, stations, ui.RadioStyle{})
		canvas.Text(panelX, 9, "MOTION", render.Style{Foreground: muted, Background: background, Attrs: render.AttrBold})
		// The select draws last so its popup paints over anything it
		// overlaps.
		// Zero styles: the closed field reads Text, the popup frame reads
		// Muted, the chosen option reads Accent, and the option under the
		// cursor reads Focus — cursor and choice finally differ.
		app.ui.Select("motion", canvas, render.Rect{X: panelX, Y: 10, W: panelW, H: 1}, &app.motion, modes, ui.SelectStyle{})
	case 2: // motion: the live display-only widgets. Unlike the selection
		// widgets, these three keep explicit styles: gauge, spinner, and
		// sparkline paint data with fg-colored glyphs, and a theme role
		// would replace fg/bg semantics (an Accent fill would draw dark
		// blocks on a violet background).
		// The gauge tracks the catalog selection (row selected+1 of 8).
		gauge := ui.GaugeState{Value: float64(app.table.Selected + 1), Max: float64(len(catalogRows))}
		app.ui.Gauge(canvas, render.Rect{X: panelX, Y: 4, W: panelW - 2, H: 1}, &gauge, ui.GaugeStyle{
			Fill:  render.Style{Foreground: violet, Background: background},
			Track: render.Style{Foreground: muted, Background: background},
		})
		app.ui.Spinner(canvas, render.Rect{X: panelX, Y: 6, W: 1, H: 1}, &app.spinner, time.Now(), ui.SpinnerStyle{
			Frames:        ui.SpinnerBraille,
			FrameDuration: 80 * time.Millisecond,
			Style:         render.Style{Foreground: cyan, Background: background},
		})
		canvas.Text(panelX+2, 6, "syncing", render.Style{Foreground: muted, Background: background})
		// The sparkline is a live sine wave: one point appended per ~100ms
		// of wall time, capped at 64 so the series slides instead of
		// growing. The time guard keeps the append rate independent of the
		// frame rate.
		now := time.Now()
		if app.lastLoad.IsZero() {
			app.lastLoad = now
		}
		if now.Sub(app.lastLoad) >= 100*time.Millisecond {
			app.loadStep++
			app.load = append(app.load, .5+.5*math.Sin(float64(app.loadStep)*.35))
			if len(app.load) > 64 {
				app.load = append(app.load[:0], app.load[1:]...)
			}
			app.lastLoad = now
		}
		load := ui.SparklineState{Values: app.load}
		app.ui.Sparkline(canvas, render.Rect{X: panelX, Y: 8, W: panelW, H: 2}, &load, ui.SparklineStyle{
			Fill: render.Style{Foreground: cyan, Background: background},
		})
		canvas.Text(panelX, 11, "load", render.Style{Foreground: muted, Background: background})
	case 3: // tree: the virtualized file tree.
		rows := app.treeRows()
		app.ui.Tree("files", canvas, render.Rect{X: panelX, Y: 3, W: panelW, H: 10}, &app.tree, len(rows),
			func(index int) ui.TreeRow { return rows[index] },
			ui.TreeStyle{})
	default: // table page: what the catalog is.
		for y, line := range []string{
			"the table on the left is",
			"the catalog itself: 8 rows,",
			"one per landed widget.",
			"",
			"pick a page above:",
			"form inputs, live motion,",
			"or a virtualized file tree.",
		} {
			canvas.Text(panelX, 4+y, line, render.Style{Foreground: muted, Background: background})
		}
	}

	mark := "[ ]"
	if app.telemetry.Checked {
		mark = "[x]"
	}
	status := fmt.Sprintf(" %d widgets  telem %s  %s  %s  %s ", len(catalogRows), mark, stations[app.palette.Selected], modes[app.motion.Selected], pages[app.pages.Selected])
	canvas.Text(2, area.H-2, status, render.Style{Foreground: muted, Background: background})
	footer := " arrows move  wheel scroll  click pick  enter open  esc quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})

	// The spinner and sparkline are live clocks, so the screen keeps
	// redrawing while motion is allowed; the runtime coalesces to the max
	// frame rate and settles to the pacer when the terminal reports reduced
	// motion.
	if frame.Capabilities.Interactive && !frame.ReducedMotion {
		frame.RequestFrame()
	}
	app.report()
}

func (app *catalog) report() {
	if app.observe == nil {
		return
	}
	open := 0
	for _, flag := range app.tree.Expanded {
		if flag {
			open++
		}
	}
	select {
	case app.observe <- catalogState{
		selected: app.table.Selected,
		offset:   app.table.Offset,
		checked:  app.telemetry.Checked,
		station:  app.palette.Selected,
		choice:   app.motion.Selected,
		open:     app.motion.Open,
		tab:      app.pages.Selected,
		treeSel:  app.tree.Selected,
		treeOpen: open,
		keys:     app.keys,
	}:
	default:
	}
}

func main() {
	if err := lux.Run(context.Background(), newCatalog()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
