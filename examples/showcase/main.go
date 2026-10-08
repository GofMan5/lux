// Command showcase demonstrates responsive layout, absolute-time motion, and
// the portable CPU 3D renderer in one small application.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/layout"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/scene3d"
	"github.com/lux-tui/lux/ui"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	cyan       = render.RGBColor(34, 211, 238)
	background = render.RGBColor(8, 10, 18)
	panel      = render.RGBColor(15, 18, 31)
)

type showcase struct {
	width, height int
	paused        bool
	angle         float32
	renderer      scene3d.Renderer
	mesh          scene3d.Mesh
	panels        [2]render.Rect
	ui            ui.Context
	name          ui.TextInput
	scenes        ui.ListState
	logs          ui.ScrollState
	controls      ui.ScrollState
	tasks         ui.TableState
	spin          ui.CheckboxState
	palette       ui.RadioState
	motion        ui.SelectState
	pages         ui.TabsState
	sync          ui.SpinnerState
	showHelp      bool
	observe       chan showcaseState
	clipboard     memoryClipboard
}

type showcaseState struct {
	name     string
	showHelp bool
	controls int
	logs     int
	spin     bool
	palette  int
	motion   int
	page     int
	open     bool
}

// memoryClipboard demonstrates the explicit port without touching the host
// clipboard or emitting OSC 52. Production applications inject their own
// bounded, consent-aware adapter.
type memoryClipboard struct{ text string }

func (c *memoryClipboard) Read(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.text, nil
}

func (c *memoryClipboard) Write(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.text = text
	return nil
}

func newShowcase() *showcase {
	app := &showcase{mesh: scene3d.Cube(1.45)}
	app.name.SetValue("LUX explorer")
	app.palette.Selected = 1
	app.motion.PopupRows = 4
	app.sync.Started = time.Now()
	return app
}

func (app *showcase) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "pause":
			if action.Kind == ui.Activate {
				app.paused = !app.paused
			}
		case "name":
			app.name.Apply(action)
		case "name-copy":
			_ = app.name.Copy(context.Background(), &app.clipboard)
		case "name-cut":
			_ = app.name.Cut(context.Background(), &app.clipboard)
		case "name-paste":
			_ = app.name.Paste(context.Background(), &app.clipboard)
		case "scenes":
			_, _ = app.scenes.Apply(action, len(sceneNames), max(action.Bounds.H, 1))
		case "help":
			if action.Kind == ui.Activate {
				app.showHelp = true
			}
		case "help-modal", "close-help":
			if action.Kind == ui.Dismiss || action.Kind == ui.Activate {
				app.showHelp = false
			}
		case "quit":
			return lux.Quit()
		case "logs":
			app.logs.Apply(action, virtualLogCount, max(action.Bounds.H, 1))
		case "controls":
			app.controls.Apply(action, controlsContent, max(action.Bounds.H, 1))
		case "tasks":
			app.tasks.Apply(action, taskColumns, len(taskRows))
		case "spin":
			app.spin.Apply(action)
		case "palette":
			app.palette.Apply(action, len(paletteNames))
		case "motion":
			app.motion.Apply(action, len(motionModes))
		case "pages":
			app.pages.Apply(action, pageNames)
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Frame:
		if !app.paused {
			delta := next.Delta
			if delta > 100*time.Millisecond {
				delta = 100 * time.Millisecond
			}
			app.angle += float32(delta.Seconds()) * .8
		}
	case event.Key:
		switch {
		case next.Key.Code == event.KeyEscape, next.Key.Rune == 'q':
			return lux.Quit()
		}
	}
	return nil
}

func (app *showcase) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyF1}, Target: "help", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'q', Mod: event.ModCtrl}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'Q', Mod: event.ModCtrl}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	switch layout.Classify(area) {
	case layout.Tiny:
		canvas.Text(0, 0, "LUX TUI - enlarge terminal", render.Style{Foreground: violet, Attrs: render.AttrBold})
		return
	case layout.Compact:
		app.drawHeader(canvas, area, frame.Capabilities.Unicode)
		toolbar := render.Rect{X: 1, Y: 3, W: max(area.W-2, 1), H: 3}
		app.drawToolbar(canvas, toolbar)
		viewport := render.Rect{X: 1, Y: 6, W: max(area.W-2, 1), H: max(area.H-8, 1)}
		if viewport.H >= 8 {
			logHeight := min(4, max(viewport.H/3, 2))
			app.drawVirtualLog(canvas, render.Rect{X: viewport.X, Y: viewport.Bottom() - logHeight, W: viewport.W, H: logHeight})
			viewport.H -= logHeight + 1
		}
		app.drawScene(frame, viewport)
	default:
		header, body := area.SplitTop(3)
		body.H = max(body.H-2, 0)
		app.drawHeader(canvas, header, frame.Capabilities.Unicode)
		panels := layout.Flex(app.panels[:], body.Inset(1, 0), layout.Horizontal, 2,
			layout.Constraint{Basis: 29, Min: 23, Max: 38, Grow: 1, Shrink: 1},
			layout.Constraint{Basis: 70, Min: 28, Grow: 3, Shrink: 2},
		)
		app.drawControls(canvas, panels[0], frame)
		app.drawScene(frame, panels[1])
	}
	if app.showHelp {
		app.drawHelp(canvas, area)
	}
	footer := " [tab] focus  [enter] activate  [F1] help  [ctrl+q] quit "
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), max(area.H-1, 0), footer, render.Style{Foreground: muted})
	if frame.Capabilities.Interactive && !app.paused && !frame.ReducedMotion {
		frame.RequestFrame()
	}
	if app.observe != nil {
		select {
		case app.observe <- showcaseState{name: app.name.Value, showHelp: app.showHelp, controls: app.controls.Offset, logs: app.logs.Offset, spin: app.spin.Checked, palette: app.palette.Selected, motion: app.motion.Selected, page: app.pages.Selected, open: app.motion.Open}:
		default:
		}
	}
}

var sceneNames = []string{"Aurora cube", "Wire pulse", "Solar drift", "Mono matrix", "Calm orbit"}

// paletteNames is the live radio-group demo: the pick recolors the scene
// panel border and its status tag.
var paletteNames = []string{"ink", "violet", "cyan"}

// motionModes is the live select demo: the commit retunes the cube rotation
// speed, and the popup is drawn last so it overlaps the panel below it.
var motionModes = []string{"orbit", "pulse", "drift", "spin"}

// pageNames is the live tabs demo: the strip selects on navigation like a
// radio group, and the gauge on the next row reads the selection as page
// progress.
var pageNames = []string{"edit", "list", "table", "form"}

const virtualLogCount = 1_000_000

// controlsContent is the panel's natural row count: heading, editor, scene
// list, nested log, the action row, widget table, the form controls (checkbox,
// radio group, select with its popup gap), the display widgets (tabs, gauge,
// spinner), and frame telemetry. The panel scrolls instead of dropping
// controls.
const controlsContent = 48

// taskColumns is package-level so the routed Apply and the draw call share
// the exact same slice: header hit-testing derives from the drawn layout.
var taskColumns = []ui.TableColumn{{Title: "TASK", Weight: 2}, {Title: "DONE", Width: 4}}

// taskRows is the live Table demo data: a small task board whose rows are
// clickable and keyboard-navigable like every other stage-8 control.
var taskRows = [][2]string{
	{"render pipeline", "yes"},
	{"input router", "yes"},
	{"widget toolkit", "wip"},
	{"theme system", "no"},
	{"docs pass", "no"},
}

func (app *showcase) drawToolbar(canvas *render.Canvas, area render.Rect) {
	buttonWidth := min(14, max(area.W/3, 6))
	app.ui.Button("pause", canvas, render.Rect{X: area.X, Y: area.Y, W: buttonWidth, H: min(area.H, 3)}, pauseLabel(app.paused), buttonStyle())
	if area.W > buttonWidth+8 {
		app.ui.Button("help", canvas, render.Rect{X: area.Right() - 8, Y: area.Y, W: 8, H: min(area.H, 3)}, "Help", buttonStyle())
	}
}

func (app *showcase) drawVirtualLog(canvas *render.Canvas, area render.Rect) {
	if area.Empty() {
		return
	}
	canvas.Fill(area, " ", render.Style{Background: background})
	view := app.ui.ScrollViewport("logs", canvas, area, &app.logs, virtualLogCount, ui.ScrollStyle{})
	visible := app.logs.Range(virtualLogCount, area.H)
	for index := visible.First; index < visible.Last; index++ {
		view.Text(0, index-visible.First, fmt.Sprintf("%06d event ready", index+1), render.Style{Foreground: muted, Background: background})
	}
}

func (app *showcase) drawControls(canvas *render.Canvas, area render.Rect, frame *lux.Frame) {
	canvas.Fill(area, " ", render.Style{Background: panel})
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: violet, Background: panel})
	inner := area.Inset(2, 1)
	if inner.Empty() {
		return
	}
	canvas.Text(inner.X, inner.Y, "INTERACTION LAB", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	body := render.Rect{X: inner.X, Y: inner.Y + 2, W: inner.W, H: max(inner.Bottom()-inner.Y-2, 0)}
	if body.Empty() {
		return
	}
	// The panel scrolls as one document, so a short terminal hides nothing, and
	// the nested log viewport hands the wheel back once it reaches its own end.
	view := app.ui.ScrollViewport("controls", canvas, body, &app.controls, controlsContent, ui.ScrollStyle{Passive: true})
	top := -app.controls.Offset
	label := render.Style{Foreground: muted, Background: panel}

	view.Text(0, top, "Project name", label)
	app.ui.Input("name", view, render.Rect{Y: top + 1, W: body.W, H: 1}, &app.name, inputStyle())
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'c', Mod: event.ModCtrl}, Owner: "name", Target: "name-copy", Kind: ui.Activate, Scope: ui.ScopeFocused})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x', Mod: event.ModCtrl}, Owner: "name", Target: "name-cut", Kind: ui.Activate, Scope: ui.ScopeFocused})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'v', Mod: event.ModCtrl}, Owner: "name", Target: "name-paste", Kind: ui.Activate, Scope: ui.ScopeFocused})

	view.Text(0, top+3, "Scene", label)
	app.ui.List("scenes", view, render.Rect{Y: top + 4, W: body.W, H: 5}, &app.scenes, sceneNames, listStyle())

	view.Text(0, top+10, "Virtual log (1M)", label)
	app.drawVirtualLog(view, render.Rect{Y: top + 11, W: body.W, H: 4})

	app.ui.Button("pause", view, render.Rect{Y: top + 16, W: min(body.W, 14), H: 3}, pauseLabel(app.paused), buttonStyle())
	if body.W >= 24 {
		app.ui.Button("help", view, render.Rect{X: body.W - 8, Y: top + 16, W: 8, H: 3}, "Help", buttonStyle())
	}

	view.Text(0, top+20, "WIDGET TABLE", render.Style{Foreground: muted, Background: panel})
	app.ui.Table("tasks", view, render.Rect{Y: top + 21, W: body.W, H: 6}, &app.tasks, taskColumns, len(taskRows),
		func(index int) []string { return taskRows[index][:] },
		ui.TableStyle{
			Normal:   render.Style{Foreground: ink, Background: panel},
			Header:   render.Style{Foreground: muted, Background: panel, Attrs: render.AttrUnderline},
			Selected: render.Style{Foreground: background, Background: violet},
			Focused:  render.Style{Foreground: background, Background: cyan},
		})

	view.Text(0, top+27, "FORM CONTROLS", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	app.ui.Checkbox("spin", view, render.Rect{Y: top + 28, W: body.W, H: 1}, &app.spin, "slow spin", checkboxStyle())
	app.ui.RadioGroup("palette", view, render.Rect{Y: top + 29, W: body.W, H: len(paletteNames)}, &app.palette, paletteNames, radioStyle())
	view.Text(0, top+32, "Motion", label)
	// The popup rows below the anchor stay blank when closed, so the open
	// popup overpaints nothing that matters, and the select is drawn last so
	// its grown region wins pointer hits over everything drawn before it.
	view.Text(0, top+38, "DISPLAY WIDGETS", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	app.ui.Tabs("pages", view, render.Rect{Y: top + 39, W: body.W, H: 1}, &app.pages, pageNames, tabsStyle())
	// The gauge reads the tab selection as page progress and the spinner
	// ticks on absolute time from the runtime's continuous frames; both are
	// display-only, so they register no regions and steal no focus or hits.
	gaugeW := max(body.W-13, 1)
	app.ui.Gauge(view, render.Rect{Y: top + 40, W: gaugeW, H: 1},
		&ui.GaugeState{Value: float64(app.pages.Selected + 1), Max: float64(len(pageNames))},
		ui.GaugeStyle{Fill: render.Style{Foreground: violet, Background: panel}, Track: render.Style{Foreground: muted, Background: panel}})
	app.ui.Spinner(view, render.Rect{X: gaugeW + 2, Y: top + 40, W: 1, H: 1}, &app.sync, time.Now(),
		ui.SpinnerStyle{Frames: ui.SpinnerBraille, FrameDuration: 80 * time.Millisecond, Style: render.Style{Foreground: cyan, Background: panel}})
	if body.W >= gaugeW+9 {
		view.Text(gaugeW+4, top+40, "loading", label)
	}
	view.Text(0, top+41, "FRAME TELEMETRY", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	app.drawTelemetry(view, render.Rect{Y: top + 42, W: body.W, H: 6}, frame)
	app.ui.Select("motion", view, render.Rect{Y: top + 33, W: body.W, H: 1}, &app.motion, motionModes, selectStyle())
}

// drawTelemetry lists the runtime's own presentation metrics, which is the only
// way an application sees the adaptive pacing decisions it is subject to.
func (app *showcase) drawTelemetry(canvas *render.Canvas, area render.Rect, frame *lux.Frame) {
	rows := []struct{ label, value string }{
		{"viewport", fmt.Sprintf("%d x %d", app.width, app.height)},
		{"target", fmt.Sprintf("%d FPS", frame.Metrics.TargetFPS)},
		{"present", frame.Metrics.PresentTime.Round(100 * time.Microsecond).String()},
		{"dropped", fmt.Sprint(frame.Metrics.DroppedFrames)},
		{"color", colorName(frame.Capabilities.Color)},
		{"renderer", "CPU / half-block"},
	}
	for index, row := range rows {
		if index >= area.H {
			break
		}
		y := area.Y + index
		canvas.Text(area.X, y, row.label, render.Style{Foreground: muted, Background: panel})
		canvas.Text(area.X+min(11, max(area.W-1, 0)), y, row.value, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
	}
}

func (app *showcase) drawHelp(canvas *render.Canvas, area render.Rect) {
	width, height := min(max(area.W-8, 24), 58), min(max(area.H-6, 10), 18)
	rect := render.Rect{X: max((area.W-width)/2, 0), Y: max((area.H-height)/2, 0), W: width, H: height}
	inner := app.ui.Modal("help-modal", canvas, rect, "Interactive controls", ui.ModalStyle{Panel: render.Style{Background: panel}, Border: render.Style{Foreground: violet, Background: panel}, Title: render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold}})
	if inner != nil {
		lines := []string{"Tab / Shift+Tab  move focus", "Enter / Space     activate", "Arrows / wheel    select or scroll", "Type / paste       edit project name", "Ctrl+Z / Shift+Z  undo / redo", "Esc / outside      close this dialog", "Ctrl+Q             quit application"}
		for index, line := range lines {
			if index >= inner.Bounds().H-4 {
				break
			}
			inner.Text(1, 1+index, line, render.Style{Foreground: ink, Background: panel})
		}
		app.ui.Button("close-help", inner, render.Rect{X: max(inner.Bounds().W-12, 0), Y: max(inner.Bounds().H-3, 0), W: min(10, inner.Bounds().W), H: min(3, inner.Bounds().H)}, "Close", buttonStyle())
	}
	app.ui.EndModal()
}

func pauseLabel(paused bool) string {
	if paused {
		return "Resume"
	}
	return "Pause"
}

func buttonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		Normal:  render.Style{Foreground: ink, Background: panel},
		Focused: render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Hovered: render.Style{Foreground: cyan, Background: panel},
		Pressed: render.Style{Foreground: background, Background: violet, Attrs: render.AttrBold},
	}
}

func inputStyle() ui.InputStyle {
	return ui.InputStyle{Normal: render.Style{Foreground: ink, Background: background}, Placeholder: render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim}, Selection: render.Style{Foreground: background, Background: cyan}, Cursor: render.Style{Foreground: background, Background: violet}}
}

func listStyle() ui.ListStyle {
	return ui.ListStyle{Normal: render.Style{Foreground: muted, Background: panel}, Selected: render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold}, Focused: render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold}, Hovered: render.Style{Foreground: cyan, Background: panel}, Marker: "> "}
}

func checkboxStyle() ui.CheckboxStyle {
	return ui.CheckboxStyle{Normal: render.Style{Foreground: ink, Background: panel}, Focused: render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold}, Checked: "[x]", Unchecked: "[ ]"}
}

func radioStyle() ui.RadioStyle {
	return ui.RadioStyle{Normal: render.Style{Foreground: muted, Background: panel}, Selected: render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold}, Focused: render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold}, Marker: "(*)", Empty: "( )"}
}

func tabsStyle() ui.TabsStyle {
	return ui.TabsStyle{
		Normal:   render.Style{Foreground: muted, Background: panel},
		Selected: render.Style{Foreground: background, Background: violet, Attrs: render.AttrBold},
		Focused:  render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
	}
}

func selectStyle() ui.SelectStyle {
	return ui.SelectStyle{
		Normal:         render.Style{Foreground: ink, Background: background},
		Focused:        render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Hovered:        render.Style{Foreground: cyan, Background: background},
		Marker:         "▾",
		Popup:          render.Style{Foreground: ink, Background: background},
		OptionNormal:   render.Style{Foreground: muted, Background: background},
		OptionSelected: render.Style{Foreground: ink, Background: background, Attrs: render.AttrBold},
	}
}

func (app *showcase) drawHeader(canvas *render.Canvas, area render.Rect, unicode bool) {
	style := render.Style{Foreground: violet, Attrs: render.AttrBold}
	name, tagline := "LUX", "terminal graphics, without the weight"
	if !unicode {
		name, tagline = "LUX TUI", "fast terminal graphics"
	}
	canvas.Text(2, area.Y, name, style)
	if name == "LUX" {
		canvas.Text(6, area.Y, "TUI", render.Style{Foreground: ink, Attrs: render.AttrBold})
	}
	canvas.Text(2, area.Y+1, tagline, render.Style{Foreground: muted})
	if area.W > 55 {
		status := "* LIVE / CELL ENGINE"
		canvas.Text(area.W-22, area.Y, status, render.Style{Foreground: cyan, Attrs: render.AttrBold})
	}
}

func (app *showcase) drawScene(frame *lux.Frame, area render.Rect) {
	if area.Empty() {
		return
	}
	canvas := frame.Canvas
	canvas.Fill(area, " ", render.Style{Background: panel})
	// The form controls own the scene's look: the radio recolors the frame,
	// the checkbox halves the pace, and the select retunes the rotation.
	accent := cyan
	switch app.palette.Selected {
	case 0:
		accent = ink
	case 1:
		accent = violet
	}
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: accent, Background: panel})
	inner := area.Inset(1, 1)
	if inner.Empty() {
		return
	}
	speed := (.65 + float64(app.scenes.Selected)*.14) * (.75 + .18*float64(app.motion.Selected))
	if app.spin.Checked {
		speed *= .35
	}
	object := scene3d.Object{Mesh: app.mesh, Transform: scene3d.RotateY(float64(app.angle) * speed).Mul(scene3d.RotateX(float64(app.angle) * (.45 + float64(app.scenes.Selected)*.08)))}
	scene := scene3d.Scene{
		Objects: []scene3d.Object{object},
		Camera:  scene3d.Camera{Position: scene3d.V3(0, 0, 4.1), Target: scene3d.V3(0, 0, 0), Up: scene3d.V3(0, 1, 0), FOVY: math.Pi / 3, Near: .1, Far: 100},
		Light:   scene3d.Light{Direction: scene3d.V3(-.5, -.7, -1), Ambient: .16, Diffuse: .84},
	}
	switch frame.Capabilities.Color {
	case capability.Monochrome:
		app.renderer.ColorMode = scene3d.Monochrome
	case capability.ANSI16:
		app.renderer.ColorMode = scene3d.ANSI16
	default:
		app.renderer.ColorMode = scene3d.TrueColor
	}
	app.renderer.Render(canvas.Subview(inner), scene)
	status := " ROTATING CUBE "
	if app.paused || frame.ReducedMotion {
		status = " PAUSED "
	}
	canvas.Text(area.X+2, area.Y, status, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
}

func colorName(depth capability.ColorDepth) string {
	return [...]string{"mono", "ANSI 16", "ANSI 256", "truecolor"}[min(int(depth), 3)]
}

func main() {
	if err := lux.Run(context.Background(), newShowcase()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
