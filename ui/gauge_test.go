package ui

import (
	"testing"

	"github.com/lux-tui/lux/render"
)

func TestGaugeFractionClamps(t *testing.T) {
	tests := []struct {
		state GaugeState
		want  float64
	}{
		{GaugeState{}, 0},
		{GaugeState{Value: 3}, 1},
		{GaugeState{Value: 25, Max: 100}, .25},
		{GaugeState{Value: 250, Max: 100}, 1},
		{GaugeState{Value: -5, Max: 100}, 0},
		{GaugeState{Value: -5, Max: -2}, 0},
	}
	for _, test := range tests {
		state := test.state
		if got := state.Fraction(); got != test.want {
			t.Fatalf("Fraction(%+v) = %v; want %v", test.state, got, test.want)
		}
	}
	var nilState *GaugeState
	if got := nilState.Fraction(); got != 0 {
		t.Fatalf("nil Fraction = %v; want 0", got)
	}
}

func TestGaugeDrawsFillTrackAndHalfCell(t *testing.T) {
	buffer := render.NewBuffer(10, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// 2.5 of 10 over 10 cells: two full blocks, one half block, seven track.
	state := GaugeState{Value: 2.5, Max: 10}
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &state, GaugeStyle{})
	if got := bufferRow(buffer, 0); got != "██▌░░░░░░░" {
		t.Fatalf("half gauge = %q", got)
	}
	// 2.4 rounds down to plain track after the fill.
	state.Value = 2.4
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &state, GaugeStyle{})
	if got := bufferRow(buffer, 0); got != "██░░░░░░░░" {
		t.Fatalf("rounded-down gauge = %q", got)
	}
	// The zero value paints a pure track.
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &GaugeState{}, GaugeStyle{})
	if got := bufferRow(buffer, 0); got != "░░░░░░░░░░" {
		t.Fatalf("zero gauge = %q", got)
	}
	// Full paints no track, and past-full clamps to full.
	full := GaugeState{Value: 9, Max: 9}
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &full, GaugeStyle{})
	if got := bufferRow(buffer, 0); got != "██████████" {
		t.Fatalf("full gauge = %q", got)
	}
	// A fraction landing exactly on a cell boundary has no half cell.
	exact := GaugeState{Value: 5, Max: 10}
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &exact, GaugeStyle{})
	if got := bufferRow(buffer, 0); got != "█████░░░░░" {
		t.Fatalf("exact-boundary gauge = %q", got)
	}
}

func TestGaugeFatRowRepeatsAndClips(t *testing.T) {
	buffer := render.NewBuffer(8, 2)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	state := GaugeState{Value: 1, Max: 2}
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 8, H: 2}, &state, GaugeStyle{})
	for y := range 2 {
		if got := bufferRow(buffer, y); got != "████░░░░" {
			t.Fatalf("fat gauge row %d = %q", y, got)
		}
	}
	// Bounds outside the canvas draw nothing and panic nothing.
	ctx.Gauge(canvas, render.Rect{X: 20, Y: 0, W: 4, H: 1}, &state, GaugeStyle{})
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 0, H: 1}, &state, GaugeStyle{})
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 0}, &state, GaugeStyle{})
	ctx.Gauge(nil, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &state, GaugeStyle{})
}

func BenchmarkGaugeDraw(b *testing.B) {
	// Display-only widget: no region, so the loop is pure painting anyway.
	buffer := render.NewBuffer(80, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	state := GaugeState{Value: 62, Max: 100}
	b.ReportAllocs()
	for range b.N {
		ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 80, H: 1}, &state, GaugeStyle{})
	}
}
