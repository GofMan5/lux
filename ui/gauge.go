package ui

import (
	"github.com/lux-tui/lux/render"
)

// GaugeState is the progress value a Gauge paints. The zero value is an empty
// gauge: 0 of 1. Max is clamped to at least 1 when it is zero or negative, so
// a GaugeState{Value: 3} is "3 of 1", full, not a division by zero.
type GaugeState struct {
	Value float64
	Max   float64
}

// Fraction returns Value/Max clamped to [0, 1].
func (g *GaugeState) Fraction() float64 {
	if g == nil {
		return 0
	}
	denominator := g.Max
	if denominator <= 0 {
		denominator = 1
	}
	return min(max(g.Value/denominator, 0), 1)
}

// GaugeStyle defines the fill and the empty track. Zero values resolve
// through the context theme: Fill reads Accent, Track reads Muted.
type GaugeStyle struct {
	Fill  render.Style
	Track render.Style
}

// Gauge paints a determinate progress bar: a run of full block cells, a
// half-block cell when the partial remainder is at least half of a cell, then
// light-shade track cells to the end. Zero-value styles resolve through the
// context theme: Fill reads Accent, Track reads Muted. The gauge is
// display-only by design — it registers no region, routes no actions, and
// cannot be focused; a seek-style interactive bar would need its own press
// contract and is not claimed. Bounds taller than one row repeat the same
// bar on every row, so a fat gauge stays honest at any height. The bar
// width is bounds.W, so the visual resolution is one cell per bounds.W of
// progress.
func (c *Context) Gauge(canvas *render.Canvas, bounds render.Rect, state *GaugeState, style GaugeStyle) {
	if canvas == nil || bounds.Empty() {
		return
	}
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	theme := c.Theme()
	fill := themedStyle(style.Fill, theme.Accent, style.Fill)
	track := themedStyle(style.Track, theme.Muted, style.Track)
	canvas = canvas.Clip(bounds)
	scaled := state.Fraction() * float64(bounds.W)
	full := min(int(scaled), bounds.W)
	halfCell := scaled-float64(full) >= .5 && full < bounds.W
	trackStart := full
	if halfCell {
		trackStart++
	}
	for y := bounds.Y; y < bounds.Y+bounds.H; y++ {
		for x := bounds.X; x < bounds.X+full; x++ {
			canvas.Put(x, y, "█", fill)
		}
		if halfCell {
			canvas.Put(bounds.X+full, y, "▌", fill)
		}
		for x := bounds.X + trackStart; x < bounds.X+bounds.W; x++ {
			canvas.Put(x, y, "░", track)
		}
	}
}
