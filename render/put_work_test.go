package render

import "testing"

func BenchmarkPutPaths(b *testing.B) {
	style := Style{Foreground: RGBColor(50, 90, 130)}
	for _, name := range []string{"put", "putcell", "fill"} {
		b.Run(name, func(b *testing.B) {
			c := NewCanvas(NewBuffer(120, 40))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				switch name {
				case "put":
					for y := range 40 {
						for x := range 120 {
							c.Put(x, y, "x", style)
						}
					}
				case "putcell":
					for y := range 40 {
						for x := range 120 {
							c.PutCell(x, y, Cell{Grapheme: "x", Width: 1, Style: style})
						}
					}
				case "fill":
					c.Fill(c.Bounds(), "x", style)
				}
			}
		})
	}
}
