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
}

func newShowcase() *showcase { return &showcase{mesh: scene3d.Cube(1.45)} }

func (app *showcase) Update(next event.Event) lux.Command {
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
		case next.Key.Rune == ' ':
			app.paused = !app.paused
		}
	}
	return nil
}

func (app *showcase) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})

	switch layout.Classify(area) {
	case layout.Tiny:
		canvas.Text(0, 0, "LUX TUI - enlarge terminal", render.Style{Foreground: violet, Attrs: render.AttrBold})
		return
	case layout.Compact:
		app.drawHeader(canvas, area)
		viewport := render.Rect{X: 1, Y: 3, W: max(area.W-2, 1), H: max(area.H-5, 1)}
		app.drawScene(frame, viewport)
	default:
		header, body := area.SplitTop(3)
		body.H = max(body.H-2, 0)
		app.drawHeader(canvas, header)
		panels := layout.Flex(app.panels[:], body.Inset(1, 0), layout.Horizontal, 2,
			layout.Constraint{Basis: 29, Min: 23, Max: 38, Grow: 1, Shrink: 1},
			layout.Constraint{Basis: 70, Min: 28, Grow: 3, Shrink: 2},
		)
		app.drawInfo(canvas, panels[0], frame)
		app.drawScene(frame, panels[1])
	}
	footer := " [space] pause   [q/esc] quit   resize freely "
	canvas.Text(max((area.W-render.TextWidth(footer))/2, 0), max(area.H-1, 0), footer, render.Style{Foreground: muted})
	if frame.Capabilities.Interactive && !app.paused && !frame.ReducedMotion {
		frame.RequestFrame()
	}
}

func (app *showcase) drawHeader(canvas *render.Canvas, area render.Rect) {
	style := render.Style{Foreground: violet, Attrs: render.AttrBold}
	canvas.Text(2, area.Y, "LUX", style)
	canvas.Text(6, area.Y, "TUI", render.Style{Foreground: ink, Attrs: render.AttrBold})
	canvas.Text(2, area.Y+1, "terminal graphics, without the weight", render.Style{Foreground: muted})
	if area.W > 55 {
		canvas.Text(area.W-22, area.Y, "● LIVE / CELL ENGINE", render.Style{Foreground: cyan, Attrs: render.AttrBold})
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
	object := scene3d.Object{Mesh: app.mesh, Transform: scene3d.RotateY(float64(app.angle)).Mul(scene3d.RotateX(float64(app.angle) * .63))}
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
