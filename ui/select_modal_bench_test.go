package ui

import (
	"strconv"
	"testing"

	"github.com/GofMan5/lux/render"
)

func clippedSelectOptions() []string {
	options := make([]string, 14)
	for i := range options {
		options[i] = "option with a deliberately long truncated label " + strconv.Itoa(i)
	}
	return options
}

// BenchmarkSelectDrawClippedHalf prices the drawable popup band: an open
// select with fourteen truncated options whose clip exposes seven of the
// fourteen popup rows and hides the anchor row.
func BenchmarkSelectDrawClippedHalf(b *testing.B) {
	options := clippedSelectOptions()
	buffer := render.NewBuffer(30, 16)
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 0, Y: 1, W: 30, H: 7})
	ctx := Context{focused: "motion"}
	state := SelectState{Selected: 2, Open: true, PopupRows: 14}
	b.ReportAllocs()
	for range b.N {
		ctx.Select("motion", canvas, render.Rect{X: 0, Y: 0, W: 30, H: 1}, &state, options, SelectStyle{})
	}
}

// BenchmarkModalDrawClippedHalf prices the modal panel band: a 24×12 panel
// painted through a clip exposing half its rows, so the fill walk and the
// border follow six drawable rows instead of twelve.
func BenchmarkModalDrawClippedHalf(b *testing.B) {
	buffer := render.NewBuffer(32, 16)
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 0, Y: 0, W: 32, H: 8})
	ctx := Context{begun: true}
	b.ReportAllocs()
	for range b.N {
		ctx.Modal("help", canvas, render.Rect{X: 2, Y: 2, W: 24, H: 12}, "Dialog", ModalStyle{})
		ctx.EndModal()
	}
}
