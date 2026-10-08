package ui

import (
	"math"

	"github.com/lux-tui/lux/render"
)

// sparkGlyphs are the eight partial-block glyphs a column can wear as its
// cap, from one eighth to a full cell, stored as ready-made strings: a
// per-column string(rune) conversion would allocate once per painted cap,
// and a sparkline paints one cap per column per frame.
var sparkGlyphs = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// SparklineState is the series a Sparkline paints. Values is app-owned and
// never copied: the widget reads it during the frame. The zero value is an
// empty sparkline (no columns). Min and Max pin the value range when Max >
// Min; the zero pair auto-scales to the actual minimum and maximum of the
// series. A degenerate range — pinned or computed, when Max <= Min — paints
// every column at half height, so a flat series still reads as present
// instead of vanishing.
type SparklineState struct {
	Values []float64
	Min    float64
	Max    float64
}

// SparklineStyle defines the glyph style. A zero Fill resolves through the
// context theme's Accent.
type SparklineStyle struct {
	Fill render.Style
}

// Fraction maps one value into [0, 1] under the state's range rules:
// pinned when Max > Min, auto-scaled from Values otherwise, clamped, and
// NaN-safe (a NaN value paints as the baseline).
func (s *SparklineState) Fraction(value float64) float64 {
	if s == nil {
		return 0
	}
	lo, hi := s.Min, s.Max
	if !(hi > lo) {
		lo, hi = s.rangeOf()
	}
	return sparkFraction(value, lo, hi)
}

// sparkFraction is the value→fraction math under a resolved range. A
// degenerate range paints half height, and NaN clamps to the baseline.
// The drawing loop calls this directly with a range resolved once per
// frame — calling Fraction per column would rescan the whole series
// every column, O(n²) in the series length.
func sparkFraction(value, lo, hi float64) float64 {
	if hi <= lo {
		return .5
	}
	fraction := (min(max(value, lo), hi) - lo) / (hi - lo)
	if math.IsNaN(fraction) {
		return 0
	}
	return min(max(fraction, 0), 1)
}

// rangeOf scans Values for the actual extent. It returns 0, 0 for an empty
// series.
func (s *SparklineState) rangeOf() (lo, hi float64) {
	if len(s.Values) == 0 {
		return 0, 0
	}
	lo, hi = math.MaxFloat64, -math.MaxFloat64
	for _, value := range s.Values {
		if math.IsNaN(value) {
			continue
		}
		lo = min(lo, value)
		hi = max(hi, value)
	}
	if lo > hi {
		return 0, 0
	}
	return lo, hi
}

// Sparkline paints a mini bar chart from block glyphs: each column's height
// is its value's fraction of the range, drawn bottom-aligned — full "█"
// cells with one partial cap glyph ("▁".."▇") on top, so a one-row sparkline
// shows eighth-of-a-cell resolution and a multi-row one composes the same
// math from whole cells. A series longer than the width keeps its newest
// values (the latest value is always the rightmost column); a shorter
// series starts at the left edge and leaves the rest blank. Only glyph
// cells are painted — no track, no background fill — so the application
// owns whatever sits under the sparkline, same as text. A zero Fill
// resolves through the context theme's Accent. The sparkline is
// display-only by design — no region, no routing, no focus; an
// interactive scrub would need its own press contract and is not claimed.
func (c *Context) Sparkline(canvas *render.Canvas, bounds render.Rect, state *SparklineState, style SparklineStyle) {
	if canvas == nil || bounds.Empty() || state == nil || len(state.Values) == 0 {
		return
	}
	if bounds.Intersect(canvas.Bounds()).Empty() {
		return
	}
	fill := themedStyle(style.Fill, c.Theme().Accent, style.Fill)
	canvas = canvas.Clip(bounds)
	// Resolve the range once: Fraction would rescan Values for every
	// column otherwise.
	lo, hi := state.Min, state.Max
	if !(hi > lo) {
		lo, hi = state.rangeOf()
	}
	columns := min(bounds.W, len(state.Values))
	first := max(len(state.Values)-columns, 0)
	bottom := bounds.Y + bounds.H - 1
	for column := 0; column < columns; column++ {
		cells := sparkFraction(state.Values[first+column], lo, hi) * float64(bounds.H)
		full := min(int(cells), bounds.H)
		for k := 0; k < full; k++ {
			canvas.Put(bounds.X+column, bottom-k, "█", fill)
		}
		remainder := cells - float64(full)
		level := int(math.Ceil(remainder * 8))
		if level > 0 && full < bounds.H {
			canvas.Put(bounds.X+column, bottom-full, sparkGlyphs[level-1], fill)
		}
	}
}
