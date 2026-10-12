package ui

import (
	"strings"
	"testing"

	"github.com/GofMan5/lux/render"
)

// BenchmarkTextAreaDrawClippedHalf prices the wrapped-row draw band: a
// 40×10 text area of mixed-width clusters (ASCII, wide, combining, tabs)
// painted through a clip exposing four rows. The layout cache is warm, so
// the loop measures row drawing, not wrapping.
func BenchmarkTextAreaDrawClippedHalf(b *testing.B) {
	area := &TextArea{}
	area.SetValue(strings.Repeat("mixed 界é🙂 cluster rows wrap\taround forty\tcolumns\n", 6))
	buffer := render.NewBuffer(40, 10)
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 0, Y: 6, W: 40, H: 4})
	ctx := Context{focused: "area"}
	rect := render.Rect{X: 0, Y: 0, W: 40, H: 10}
	ctx.TextArea("area", canvas, rect, area, TextAreaStyle{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx.TextArea("area", canvas, rect, area, TextAreaStyle{})
	}
}

// BenchmarkTextInputDraw prices the input row: a field whose value is a
// mixed-cluster string with the caret at the end, so every frame walks the
// whole visible window plus the trailing caret cell.
func BenchmarkTextInputDraw(b *testing.B) {
	value := strings.Repeat("mixed 界é→ input ", 6)
	input := &TextInput{Value: value, Cursor: len(value)}
	buffer := render.NewBuffer(40, 1)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "field"}
	rect := render.Rect{X: 0, Y: 0, W: 40, H: 1}
	ctx.Input("field", canvas, rect, input, InputStyle{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx.Input("field", canvas, rect, input, InputStyle{})
	}
}
