package main

import (
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

var workbenchThemes = []string{"dark", "light", "terminal-default", "high-contrast"}
var workbenchDensities = []string{"compact", "comfortable"}

type workbenchSettings struct {
	open    bool
	theme   ui.RadioState
	density ui.RadioState
	motion  ui.CheckboxState
}

func (app *tour) drawSettings(canvas *render.Canvas, area render.Rect) {
	canvas = canvas.Clip(area)
	// Full-screen settings temporarily own keyboard input; the router restores
	// the previous editor's focus and caret on F3/Escape, not a second model.
	app.ui.BeginModal("settings-toggle", canvas, area)
	defer app.ui.EndModal()
	text := render.Style{Foreground: app.paint.ink, Background: app.paint.background}
	canvas.Text(area.X, area.Y, "Workbench settings", render.Style{Foreground: app.paint.ink, Background: app.paint.background, Attrs: render.AttrBold})
	canvas.Text(area.X, area.Y+1, "Changes apply immediately; F3 returns to the current stand.", text)
	canvas.Text(area.X, area.Y+3, "Theme", text)
	app.ui.RadioGroup("settings-theme", canvas, render.Rect{X: area.X, Y: area.Y + 4, W: area.W, H: 4}, &app.settings.theme, workbenchThemes, app.radioStyle())
	canvas.Text(area.X, area.Y+9, "Density", text)
	app.ui.RadioGroup("settings-density", canvas, render.Rect{X: area.X, Y: area.Y + 10, W: area.W, H: 2}, &app.settings.density, workbenchDensities, app.radioStyle())
	app.ui.Checkbox("settings-motion", canvas, render.Rect{X: area.X, Y: area.Y + 13, W: area.W, H: 1}, &app.settings.motion, "Reduced motion", app.checkboxStyle())
	canvas.Text(area.X, area.Y+15, "Runtime reduced-motion policy is always respected.", text)
	canvas.Text(area.X, area.Y+17, "Tab: focus | Arrows / Space: change | F3: return", text)
}
