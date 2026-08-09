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
	showHelp      bool
}

func newShowcase() *showcase {
	app := &showcase{mesh: scene3d.Cube(1.45)}
	app.name.SetValue("LUX explorer")
	return app
}

func (app *showcase) Update(next event.Event) lux.Command {
	if next.Kind == event.Key {
		if next.Key.Code == event.KeyF1 {
			app.showHelp = true
			return nil
		}
		if next.Key.Code == event.KeyRune && next.Key.Mod&event.ModCtrl != 0 && (next.Key.Rune == 'q' || next.Key.Rune == 'Q') {
			return lux.Quit()
		}
	}
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "pause":
			if action.Kind == ui.Activate {
				app.paused = !app.paused
			}
		case "name":
			app.name.Apply(action)
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

	switch layout.Classify(area) {
	case layout.Tiny:
		canvas.Text(0, 0, "LUX TUI - enlarge terminal", render.Style{Foreground: violet, Attrs: render.AttrBold})
		return
	case layout.Compact:
		app.drawHeader(canvas, area, frame.Capabilities.Unicode)
		toolbar := render.Rect{X: 1, Y: 3, W: max(area.W-2, 1), H: 3}
		app.drawToolbar(canvas, toolbar)
		viewport := render.Rect{X: 1, Y: 6, W: max(area.W-2, 1), H: max(area.H-8, 1)}
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
	canvas.Text(max((area.W-render.TextWidth(footer))/2, 0), max(area.H-1, 0), footer, render.Style{Foreground: muted})
	if frame.Capabilities.Interactive && !app.paused && !frame.ReducedMotion {
		frame.RequestFrame()
	}
}

var sceneNames = []string{"Aurora cube", "Wire pulse", "Solar drift", "Mono matrix", "Calm orbit"}

func (app *showcase) drawToolbar(canvas *render.Canvas, area render.Rect) {
	buttonWidth := min(14, max(area.W/3, 6))
	app.ui.Button("pause", canvas, render.Rect{X: area.X, Y: area.Y, W: buttonWidth, H: min(area.H, 3)}, pauseLabel(app.paused), buttonStyle())
	if area.W > buttonWidth+8 {
		app.ui.Button("help", canvas, render.Rect{X: area.Right() - 8, Y: area.Y, W: 8, H: min(area.H, 3)}, "Help", buttonStyle())
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
	y := inner.Y + 2
	if y < inner.Bottom() {
		canvas.Text(inner.X, y, "Project name", render.Style{Foreground: muted, Background: panel})
		y++
		app.ui.Input("name", canvas, render.Rect{X: inner.X, Y: y, W: inner.W, H: 1}, &app.name, inputStyle())
		y += 2
	}
	listHeight := max(inner.Bottom()-y-4, 0)
	if listHeight > 0 {
		canvas.Text(inner.X, y, "Scene", render.Style{Foreground: muted, Background: panel})
		y++
		app.ui.List("scenes", canvas, render.Rect{X: inner.X, Y: y, W: inner.W, H: listHeight}, &app.scenes, sceneNames, listStyle())
		y += listHeight + 1
	}
	if y+2 < inner.Bottom() {
		app.ui.Button("pause", canvas, render.Rect{X: inner.X, Y: y, W: min(inner.W, 14), H: 3}, pauseLabel(app.paused), buttonStyle())
		if inner.W >= 24 {
			app.ui.Button("help", canvas, render.Rect{X: inner.Right() - 8, Y: y, W: 8, H: 3}, "Help", buttonStyle())
		}
	}
	_ = frame
}

func (app *showcase) drawHelp(canvas *render.Canvas, area render.Rect) {
	width, height := min(max(area.W-8, 24), 58), min(max(area.H-6, 10), 18)
	rect := render.Rect{X: max((area.W-width)/2, 0), Y: max((area.H-height)/2, 0), W: width, H: height}
	inner := app.ui.Modal("help-modal", canvas, rect, "Interactive controls", ui.ModalStyle{Panel: render.Style{Background: panel}, Border: render.Style{Foreground: violet, Background: panel}, Title: render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold}})
	if inner != nil {
		lines := []string{"Tab / Shift+Tab  move focus", "Enter / Space     activate", "Arrows / wheel    select scene", "Type / paste       edit project name", "Esc / outside      close this dialog", "Ctrl+Q             quit application"}
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

func (app *showcase) drawInfo(canvas *render.Canvas, area render.Rect, frame *lux.Frame) {
	canvas.Fill(area, " ", render.Style{Background: panel})
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: violet, Background: panel})
	inner := area.Inset(2, 1)
	canvas.Text(inner.X, inner.Y, "FRAME TELEMETRY", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	rows := []struct{ label, value string }{
		{"viewport", fmt.Sprintf("%d × %d", app.width, app.height)},
		{"target", fmt.Sprintf("%d FPS", frame.Metrics.TargetFPS)},
		{"present", frame.Metrics.PresentTime.Round(100 * time.Microsecond).String()},
		{"dropped", fmt.Sprint(frame.Metrics.DroppedFrames)},
		{"color", colorName(frame.Capabilities.Color)},
		{"renderer", "CPU / half-block"},
	}
	for index, row := range rows {
		y := inner.Y + 2 + index*2
		if y >= inner.Bottom() {
			break
		}
		canvas.Text(inner.X, y, row.label, render.Style{Foreground: muted, Background: panel})
		canvas.Text(min(inner.X+11, inner.Right()-1), y, row.value, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
	}
}

func (app *showcase) drawScene(frame *lux.Frame, area render.Rect) {
	if area.Empty() {
		return
	}
	canvas := frame.Canvas
	canvas.Fill(area, " ", render.Style{Background: panel})
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: cyan, Background: panel})
	inner := area.Inset(1, 1)
	if inner.Empty() {
		return
	}
	speed := .65 + float64(app.scenes.Selected)*.14
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
