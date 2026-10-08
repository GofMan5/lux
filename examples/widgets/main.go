// Command widgets is the stage-8 control catalog: one screen, one live
// instance of every ui widget this stage lands. Table shipped in slice 1;
// checkbox, radio, and select/dropdown join in slice 2.
package main

import (
	"context"
	"fmt"
	"os"

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
	{"Tabs", "planned", "3"},
	{"Gauge/Progress", "planned", "3"},
	{"Spinner", "planned", "3"},
	{"Sparkline", "planned", "4"},
	{"Tree", "planned", "4"},
}

// stations feeds the radio demo; modes feeds the select demo.
var stations = []string{"ink", "violet", "amber"}
var modes = []string{"live", "drag", "paint"}

type catalog struct {
	width, height int
	ui            ui.Context
	table         ui.TableState
	telemetry     ui.CheckboxState
	palette       ui.RadioState
	motion        ui.SelectState
	observe       chan catalogState
}

type catalogState struct {
	selected int
	offset   int
	checked  bool
	station  int
	choice   int
	open     bool
}

func newCatalog() *catalog {
	return &catalog{
		// The demo select keeps its popup small: three rows of modes inside
		// the controls panel.
		motion:  ui.SelectState{Selected: 2, PopupRows: 3},
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

	// The controls panel demos the slice-2 widgets: one checkbox, one radio
	// group, one dropdown. The select draws last so its popup paints over
	// anything it overlaps.
	panelX, panelW := 40, area.W-42
	canvas.Text(panelX, 1, "CONTROLS", render.Style{Foreground: muted, Background: background, Attrs: render.AttrBold})
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

	mark := "[ ]"
	if app.telemetry.Checked {
		mark = "[x]"
	}
	status := fmt.Sprintf(" %d widgets  telem %s  %s  %s ", len(catalogRows), mark, stations[app.palette.Selected], modes[app.motion.Selected])
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
