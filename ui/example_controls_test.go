package ui_test

import (
	"fmt"

	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// ExampleContext_Checkbox draws one checkbox, then applies the same
// activation a routed Enter or mouse release would deliver and prints the
// repainted marker: the zero value is ready and unchecked.
func ExampleContext_Checkbox() {
	buffer := render.NewBuffer(24, 1)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	state := ui.CheckboxState{}
	ctx.Checkbox("sync", canvas, render.Rect{W: 24, H: 1}, &state, "stream telemetry", ui.CheckboxStyle{})
	state.Apply(ui.Action{Kind: ui.Activate, Event: event.KeyPress(event.KeyEnter, 0)})
	ctx.Checkbox("sync", canvas, render.Rect{W: 24, H: 1}, &state, "stream telemetry", ui.CheckboxStyle{})
	fmt.Println(renderRows(buffer)[0])
	// Output:
	// [x] stream telemetry
}

// ExampleContext_RadioGroup draws a three-station radio group whose zero
// value selects the first option, then moves the choice with the Down key
// exactly as routed key input would.
func ExampleContext_RadioGroup() {
	buffer := render.NewBuffer(20, 3)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	state := ui.RadioState{}
	labels := []string{"ink", "violet", "amber"}
	ctx.RadioGroup("palette", canvas, render.Rect{W: 20, H: 3}, &state, labels, ui.RadioStyle{})
	state.Apply(ui.Action{Kind: ui.Scroll, Event: event.KeyPress(event.KeyDown, 0)}, len(labels))
	ctx.RadioGroup("palette", canvas, render.Rect{W: 20, H: 3}, &state, labels, ui.RadioStyle{})
	for _, line := range renderRows(buffer) {
		fmt.Println(line)
	}
	// Output:
	// ( ) ink
	// (*) violet
	// ( ) amber
}

// ExampleContext_Select draws a closed dropdown, opens it the way a routed
// Enter does, and prints the anchor row plus the option rows. The zero value
// is ready: the first option is current and the popup centres on it.
func ExampleContext_Select() {
	buffer := render.NewBuffer(24, 5)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	options := []string{"alpha", "bravo", "charlie", "delta"}
	state := ui.SelectState{Selected: 1}
	ctx.Select("motion", canvas, render.Rect{W: 24, H: 1}, &state, options, ui.SelectStyle{})
	state.Apply(ui.Action{Kind: ui.Activate, Event: event.KeyPress(event.KeyEnter, 0)}, len(options))
	ctx.Select("motion", canvas, render.Rect{W: 24, H: 1}, &state, options, ui.SelectStyle{})
	for _, line := range renderRows(buffer) {
		fmt.Println(line)
	}
	// Output:
	// bravo                 ▾
	//    alpha
	//  > bravo
	//    charlie
	//    delta
}
