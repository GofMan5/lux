package main

import (
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// App-owned values, not a global palette mutation or a library theme registry.
// The incumbent dark colors remain byte-identical; other themes are explicit
// workbench choices. Illustrative gradients and 3D materials stay their data.
type tourPaint struct {
	ink, muted, violet, cyan, green, amber, background, panel render.Color
}

func workbenchPaint(index int) tourPaint {
	p := ui.ReferencePalette()
	paint := tourPaint{p.Ink, p.Muted, p.Violet, p.Cyan, render.RGBColor(52, 211, 153), render.RGBColor(251, 191, 36), p.Background, render.RGBColor(15, 18, 31)}
	switch index {
	case 1: // Light: dark text and saturated focus/selection on light surfaces.
		paint = tourPaint{render.RGBColor(24, 28, 40), render.RGBColor(70, 78, 96), render.RGBColor(91, 33, 182), render.RGBColor(14, 89, 112), render.RGBColor(14, 100, 70), render.RGBColor(135, 71, 0), render.RGBColor(250, 250, 252), render.RGBColor(235, 238, 244)}
	case 2: // Terminal defaults: attributes, never an assumed background color.
		paint = tourPaint{}
	case 3: // Explicit black/white; selection and focus also differ by attributes.
		white, black := render.RGBColor(255, 255, 255), render.RGBColor(0, 0, 0)
		paint = tourPaint{white, white, white, white, white, white, black, black}
	}
	return paint
}

func (p tourPaint) theme() ui.Theme {
	if p == (tourPaint{}) {
		return ui.DefaultTheme()
	}
	return ui.Theme{
		Text:   render.Style{Foreground: p.ink, Background: p.background},
		Muted:  render.Style{Foreground: p.muted, Background: p.background},
		Accent: render.Style{Foreground: p.background, Background: p.violet, Attrs: render.AttrBold},
		Focus:  render.Style{Foreground: p.background, Background: p.cyan, Attrs: render.AttrBold | render.AttrUnderline},
		Danger: render.Style{Foreground: p.muted, Background: p.background, Attrs: render.AttrDim},
	}
}
