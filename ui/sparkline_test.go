package ui

import (
	"math"
	"testing"

	"github.com/GofMan5/lux/render"
)

func TestSparklineFractionRules(t *testing.T) {
	tests := []struct {
		name  string
		state SparklineState
		value float64
		want  float64
	}{
		{"pinned range", SparklineState{Min: 0, Max: 10}, 2.5, .25},
		{"pinned clamps low", SparklineState{Min: 0, Max: 10}, -5, 0},
		{"pinned clamps high", SparklineState{Min: 0, Max: 10}, 50, 1},
		{"auto uses series extent", SparklineState{Values: []float64{2, 6}}, 4, .5},
		{"auto clamps to extent", SparklineState{Values: []float64{2, 6}}, 9, 1},
		{"auto clamps below extent", SparklineState{Values: []float64{2, 6}}, 0, 0},
		{"degenerate series paints half", SparklineState{Values: []float64{5, 5, 5}}, 5, .5},
		{"degenerate pinned paints half", SparklineState{Min: 3, Max: 3}, 3, .5},
		{"inverted pinned paints half", SparklineState{Min: 5, Max: 1}, 3, .5},
		{"NaN paints the baseline", SparklineState{Values: []float64{3, 9}}, math.NaN(), 0},
		{"empty series is degenerate", SparklineState{}, 5, .5},
	}
	for _, test := range tests {
		state := test.state
		if got := state.Fraction(test.value); got != test.want {
			t.Fatalf("%s: Fraction(%v) = %v; want %v", test.name, test.value, got, test.want)
		}
	}
	var nilState *SparklineState
	if got := nilState.Fraction(5); got != 0 {
		t.Fatalf("nil Fraction = %v; want 0", got)
	}
}

func TestSparklineDrawsOneRowOfCaps(t *testing.T) {
	// The sparkline paints glyph cells only — no track, no background fill —
	// so each case starts from a fresh buffer; the application owns the
	// background it draws over.
	buffer := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// Auto-scaled 1..8: each column is one seventh higher than the last, so
	// the caps climb ▂▃▄▅▆▇ and the extremes are blank and full.
	state := SparklineState{Values: []float64{1, 2, 3, 4, 5, 6, 7, 8}}
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 8, H: 1}, &state, SparklineStyle{})
	if got := bufferRow(buffer, 0); got != " ▂▃▄▅▆▇█" {
		t.Fatalf("eight-level sparkline = %q", got)
	}
	// A flat series paints half-height caps everywhere instead of nothing.
	flatBuffer := render.NewBuffer(8, 1)
	flatCanvas := render.NewCanvas(flatBuffer)
	flat := SparklineState{Values: []float64{4, 4, 4, 4}}
	ctx.Sparkline(flatCanvas, render.Rect{X: 0, Y: 0, W: 8, H: 1}, &flat, SparklineStyle{})
	if got := bufferRow(flatBuffer, 0); got != "▄▄▄▄" {
		t.Fatalf("flat sparkline = %q", got)
	}
}

func TestSparklineDrawsMultiRowColumns(t *testing.T) {
	buffer := render.NewBuffer(3, 3)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// Pinned 0..1 over three rows: .5 is one full cell plus a half cap above
	// it, 1 is three full cells, 0 is a blank column.
	state := SparklineState{Values: []float64{.5, 1, 0}, Min: 0, Max: 1}
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 3, H: 3}, &state, SparklineStyle{})
	for y, want := range []string{" █", "▄█", "██"} {
		if got := bufferRow(buffer, y); got != want {
			t.Fatalf("multi-row row %d = %q; want %q", y, got, want)
		}
	}
}

func TestSparklineKeepsNewestValuesOnTheRight(t *testing.T) {
	buffer := render.NewBuffer(4, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// Ten rising values over four columns keep the last four (7, 8, 9, 10).
	// The scale stays the whole series' 1..10, so a sliding window reads at
	// a constant range instead of rescaling as it moves.
	state := SparklineState{Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &state, SparklineStyle{})
	// 7 is 6/9=.667 → ceil 6 → ▆, 8 is 7/9=.778 → ceil 7 → ▇, then █ █.
	if got := bufferRow(buffer, 0); got != "▆▇██" {
		t.Fatalf("windowed sparkline = %q", got)
	}
}

func TestSparklineGuards(t *testing.T) {
	buffer := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	state := SparklineState{Values: []float64{1, 2, 3}}
	// Empty bounds, nil canvas, and nil state draw nothing and panic nothing.
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 0, H: 1}, &state, SparklineStyle{})
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 0}, &state, SparklineStyle{})
	ctx.Sparkline(nil, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &state, SparklineStyle{})
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 1}, nil, SparklineStyle{})
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &SparklineState{}, SparklineStyle{})
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &SparklineState{Values: nil}, SparklineStyle{})
	if got := bufferRow(buffer, 0); got != "" {
		t.Fatalf("guarded draw painted %q", got)
	}
	// A row of NaNs alone is a degenerate series: half-height caps.
	nan := SparklineState{Values: []float64{math.NaN(), math.NaN()}}
	ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 8, H: 1}, &nan, SparklineStyle{})
	if got := bufferRow(buffer, 0); got != "▄▄" {
		t.Fatalf("all-NaN sparkline = %q", got)
	}
}

func BenchmarkSparklineDraw(b *testing.B) {
	// Display-only widget: no region, so the loop is pure painting anyway.
	buffer := render.NewBuffer(80, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	values := make([]float64, 80)
	for i := range values {
		values[i] = float64(i)
	}
	state := SparklineState{Values: values}
	b.ReportAllocs()
	for range b.N {
		ctx.Sparkline(canvas, render.Rect{X: 0, Y: 0, W: 80, H: 1}, &state, SparklineStyle{})
	}
}
