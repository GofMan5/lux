package render

import "testing"

// BenchmarkCanvasFill prices whole-frame and panel fills, the background
// paint every reference app pays before drawing content.
func BenchmarkCanvasFill(b *testing.B) {
	style := Style{Foreground: RGBColor(220, 225, 235), Background: RGBColor(20, 24, 32)}
	b.Run("clear-120x40", func(b *testing.B) {
		canvas := NewCanvas(NewBuffer(120, 40))
		b.ReportAllocs()
		for range b.N {
			canvas.Clear(style)
		}
	})
	b.Run("panel-clipped", func(b *testing.B) {
		canvas := NewCanvas(NewBuffer(120, 40)).Subview(Rect{X: 10, Y: 5, W: 100, H: 30}).Clip(Rect{X: 0, Y: 0, W: 100, H: 15})
		b.ReportAllocs()
		for range b.N {
			canvas.Fill(Rect{X: 2, Y: 2, W: 90, H: 25}, " ", style)
		}
	})
	b.Run("wide-120x40", func(b *testing.B) {
		canvas := NewCanvas(NewBuffer(120, 40))
		b.ReportAllocs()
		for range b.N {
			canvas.Fill(canvas.Bounds(), "界", style)
		}
	})
}
