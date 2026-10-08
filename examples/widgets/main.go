// Command widgets is the stage-8 control catalog: one screen, one live
// instance of every ui widget this stage lands. Table shipped in slice 1;
// checkbox, radio, and select/dropdown joined in slice 2; tabs, gauge, and
// spinner land in slice 3.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
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
	{"Sparkline", "planned", "4"},
	{"Tree", "planned", "4"},
}

// stations feeds the radio demo; modes feeds the select demo; pages feeds
// the tabs strip.
var stations = []string{"ink", "violet", "amber"}
var modes = []string{"live", "drag", "paint"}
var pages = []string{"table", "form", "motion"}

type catalog struct {
	width, height int
	ui            ui.Context
	table         ui.TableState
	telemetry     ui.CheckboxState
	palette       ui.RadioState
	motion        ui.SelectState
	pages         ui.TabsState
	spinner       ui.SpinnerState
	observe       chan catalogState
}

type catalogState struct {
	selected int
	offset   int
	checked  bool
	station  int
	choice   int
	open     bool
	tab      int
}

func newCatalog() *catalog {
	return &catalog{
		// The demo select keeps its popup small: three rows of modes inside
		// the controls panel.
		motion: ui.SelectState{Selected: 2, PopupRows: 3},
		// The spinner runs on the wall clock; its glyph is a pure function
		// of absolute time, so no frame counter is needed here.
		spinner: ui.SpinnerState{Started: time.Now()},
		observe: make(chan catalogState, 16),
	}
}

func (app *catalog) Update(next event.Event) lux.Command {
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
	app.ui.Table("widgets-table", canvas, body, &app.table, tableColumns, len(catalogRows),
		func(index int) []string { return catalogRows[index][:] },
		ui.TableStyle{
			Normal:   render.Style{Foreground: ink, Background: background},
			Header:   render.Style{Foreground: muted, Background: background, Attrs: render.AttrUnderline},
			Selected: render.Style{Foreground: background, Background: violet},
			Focused:  render.Style{Foreground: background, Background: cyan},
		})

	// The controls panel demos the stage widgets: a tabs strip, one
	// checkbox, one radio group, one dropdown. The select draws last so its
	// popup paints over anything it overlaps.
	panelX, panelW := 40, area.W-42
	canvas.Text(panelX, 1, "CONTROLS", render.Style{Foreground: muted, Background: background, Attrs: render.AttrBold})
	app.ui.Tabs("pages", canvas, render.Rect{X: panelX, Y: 2, W: panelW, H: 1}, &app.pages, pages, ui.TabsStyle{
		Normal:   render.Style{Foreground: ink, Background: background},
		Selected: render.Style{Foreground: background, Background: violet},
		Focused:  render.Style{Foreground: background, Background: cyan},
	})
	app.ui.Checkbox("telemetry", canvas, render.Rect{X: panelX, Y: 3, W: panelW, H: 1}, &app.telemetry, "stream telemetry", ui.CheckboxStyle{
		Normal:  render.Style{Foreground: ink, Background: background},
		Focused: render.Style{Foreground: background, Background: cyan},
	})
	app.ui.RadioGroup("palette", canvas, render.Rect{X: panelX, Y: 5, W: panelW, H: 3}, &app.palette, stations, ui.RadioStyle{
		Normal:   render.Style{Foreground: ink, Background: background},
		Selected: render.Style{Foreground: background, Background: violet},
		Focused:  render.Style{Foreground: background, Background: cyan},
	})
	canvas.Text(panelX, 9, "MOTION", render.Style{Foreground: muted, Background: background, Attrs: render.AttrBold})

	// The row under the table demos the display-only slice-3 widgets: the
	// gauge tracks the catalog selection (row selected+1 of 8), and the
	// spinner runs on the wall clock beside it. Neither registers a region,
	// so they take no input by design.
	gauge := ui.GaugeState{Value: float64(app.table.Selected + 1), Max: float64(len(catalogRows))}
	app.ui.Gauge(canvas, render.Rect{X: 2, Y: 12, W: 20, H: 1}, &gauge, ui.GaugeStyle{
		Fill:  render.Style{Foreground: violet, Background: background},
		Track: render.Style{Foreground: muted, Background: background},
	})
	app.ui.Spinner(canvas, render.Rect{X: 24, Y: 12, W: 1, H: 1}, &app.spinner, time.Now(), ui.SpinnerStyle{
		Frames:        ui.SpinnerBraille,
		FrameDuration: 80 * time.Millisecond,
		Style:         render.Style{Foreground: cyan, Background: background},
	})
	canvas.Text(26, 12, "syncing", render.Style{Foreground: muted, Background: background})

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

	app.ui.Select("motion", canvas, render.Rect{X: panelX, Y: 10, W: panelW, H: 1}, &app.motion, modes, ui.SelectStyle{
		Normal:         render.Style{Foreground: ink, Background: background},
		Focused:        render.Style{Foreground: background, Background: cyan},
		OptionNormal:   render.Style{Foreground: ink, Background: background},
		OptionSelected: render.Style{Foreground: background, Background: violet},
	})
	// The spinner is a live clock, so the screen keeps redrawing while
	// motion is allowed; the runtime coalesces to the max frame rate and
	// settles to the pacer when the terminal reports reduced motion.
	if frame.Capabilities.Interactive && !frame.ReducedMotion {
		frame.RequestFrame()
	}
	app.report()
}

func (app *catalog) report() {
	if app.observe == nil {
		return
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
