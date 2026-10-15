package main

import (
	"fmt"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// Retain routing metadata only, never the event's potentially large text payload.
type workbenchInspection struct {
	open        bool
	sourceFocus string
	lastTarget  ui.ID
	lastBounds  render.Rect
	lastHandled bool
}

func (app *tour) drawInspection(frame *lux.Frame, area render.Rect) {
	canvas := frame.Canvas.Clip(area)
	app.ui.BeginModal("inspection-toggle", canvas, area)
	defer app.ui.EndModal()
	text := render.Style{Foreground: app.paint.ink, Background: app.paint.background}
	muted := render.Style{Foreground: app.paint.muted, Background: app.paint.background}
	canvas.Text(area.X, area.Y, "Workbench inspection", render.Style{Foreground: app.paint.ink, Background: app.paint.background, Attrs: render.AttrBold})
	canvas.Text(area.X, area.Y+1, "SIMULATED capability profiles", muted)
	for index, profile := range []string{"Monochrome", "ANSI16", "ANSI256", "TrueColor"} {
		marker := "[ ]"
		if index == int(frame.Capabilities.Color) {
			marker = "[*]"
		}
		canvas.Text(area.X+2, area.Y+2+index, fmt.Sprintf("%s %-11s preview only", marker, profile), muted)
	}
	canvas.Text(area.X, area.Y+7, fmt.Sprintf("Actual capabilities: terminal=%s color=%s unicode=%t interactive=%t", frame.Capabilities.Terminal, frame.Capabilities.Color, frame.Capabilities.Unicode, frame.Capabilities.Interactive), text)
	canvas.Text(area.X, area.Y+12, "Source focus: "+app.inspection.sourceFocus, text)
	canvas.Text(area.X, area.Y+13, fmt.Sprintf("Last route: handled=%t target=%s", app.inspection.lastHandled, app.inspection.lastTarget), text)
	bounds := app.inspection.lastBounds
	canvas.Text(area.X, area.Y+14, fmt.Sprintf("Target bounds: x=%d y=%d w=%d h=%d", bounds.X, bounds.Y, bounds.W, bounds.H), text)
	canvas.Text(area.X, area.Y+15, fmt.Sprintf("Viewport: %dx%d | inspection: %dx%d", app.width, app.height, area.W, area.H), text)
	canvas.Text(area.X, area.Y+16, fmt.Sprintf("Last Present: %s | target %d fps | dropped %d", frame.Metrics.PresentTime.Round(100*time.Microsecond), frame.Metrics.TargetFPS, frame.Metrics.DroppedFrames), text)
	canvas.Text(area.X, area.Y+17, "Present includes backend I/O; not preparation-only frame cost.", muted)
	canvas.Text(area.X, area.Y+19, "F4/Escape: return", muted)
}

func (app *tour) uiFocusName() string {
	for _, id := range []ui.ID{"tabs", "catalog-tree", "catalog-mode", "catalog-editor", "name", "list", "slow", "glyph", "results", "scroll", "wscroll", "table", "gauge", "annex-run", "annex-indet", "annex-error", "annex-stop", "annex-notify", "annex-copy"} {
		if app.ui.Focused(id) {
			return string(id)
		}
	}
	return "none"
}
