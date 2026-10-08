package ui_test

import (
	"fmt"
	"time"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
)

// ExampleContext_Tabs draws a three-tab strip, then moves the selection with
// the Right key exactly as routed input would and prints the repainted strip.
// The zero value is ready: tab 0 is selected and the strip starts at its
// beginning.
func ExampleContext_Tabs() {
	buffer := render.NewBuffer(28, 1)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	labels := []string{"files", "logs", "config"}
	state := ui.TabsState{}
	ctx.Tabs("pages", canvas, render.Rect{W: 28, H: 1}, &state, labels, ui.TabsStyle{})
	state.Apply(ui.Action{Kind: ui.Scroll, Event: event.KeyPress(event.KeyRight, 0)}, labels)
	ctx.Tabs("pages", canvas, render.Rect{W: 28, H: 1}, &state, labels, ui.TabsStyle{})
	fmt.Println(renderRows(buffer)[0])
	// Output:
	//  files  logs  config
}

// ExampleContext_Gauge paints three gauges from the same zero-ready state
// family: empty, half (with the half-block cell), and full.
func ExampleContext_Gauge() {
	buffer := render.NewBuffer(12, 3)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	ctx.Gauge(canvas, render.Rect{Y: 0, W: 12, H: 1}, &ui.GaugeState{}, ui.GaugeStyle{})
	half := ui.GaugeState{Value: 55, Max: 100}
	ctx.Gauge(canvas, render.Rect{Y: 1, W: 12, H: 1}, &half, ui.GaugeStyle{})
	full := ui.GaugeState{Value: 12, Max: 12}
	ctx.Gauge(canvas, render.Rect{Y: 2, W: 12, H: 1}, &full, ui.GaugeStyle{})
	for _, line := range renderRows(buffer) {
		fmt.Println(line)
	}
	// Output:
	// ░░░░░░░░░░░░
	// ██████▌░░░░░
	// ████████████
}

// ExampleContext_Spinner paints a running spinner at two instants of a fixed
// clock. The glyph is a pure function of absolute time, so both draws are
// deterministic and the same elapsed age paints the same glyph at any frame
// rate.
func ExampleContext_Spinner() {
	buffer := render.NewBuffer(6, 2)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	running := ui.SpinnerState{Started: now.Add(-250 * time.Millisecond)}
	ctx.Spinner(canvas, render.Rect{Y: 0, W: 1, H: 1}, &running, now, ui.SpinnerStyle{})
	running.Started = now.Add(-400 * time.Millisecond)
	ctx.Spinner(canvas, render.Rect{Y: 1, W: 1, H: 1}, &running, now, ui.SpinnerStyle{})
	for _, line := range renderRows(buffer) {
		fmt.Println(line)
	}
	// Output:
	// -
	// |
}
