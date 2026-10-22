package render

import (
	"math/rand/v2"
	"testing"
)

// fillReference is Fill as it was before the narrow fast path: one Put per
// tile. Fill must stay cell-for-cell identical to it.
func fillReference(c *Canvas, rect Rect, grapheme string, style Style) {
	grapheme, width := firstGrapheme(grapheme, c.WidthPolicy())
	if width == 0 || !printable(grapheme) {
		return
	}
	rect = rect.Intersect(c.Bounds())
	for y := rect.Y; y < rect.Bottom(); y++ {
		for x := rect.X; x < rect.Right(); x += width {
			if x+width <= rect.Right() {
				c.Put(x, y, grapheme, style)
			} else {
				c.Put(x, y, " ", style)
			}
		}
	}
}

func TestFillMatchesPerCellReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	seeds := []string{"a", "界", "👍", "é", "x"}
	graphemes := []string{" ", "x", "界", "é", "", "\t", "\x1b", "ab"}
	styleA := Style{Foreground: RGBColor(1, 2, 3)}
	styleB := Style{Background: RGBColor(9, 8, 7)}
	for round := range 4000 {
		w, h := 1+rng.IntN(14), 1+rng.IntN(6)
		seed := NewBuffer(w, h)
		seedCanvas := NewCanvas(seed)
		for range rng.IntN(w * h) {
			seedCanvas.PutCell(rng.IntN(w), rng.IntN(h), Cell{Grapheme: seeds[rng.IntN(len(seeds))], Style: styleA, Link: Link{URI: "u"}})
		}
		randRect := func(maxW, maxH int) Rect {
			return Rect{X: rng.IntN(maxW+4) - 2, Y: rng.IntN(maxH+4) - 2, W: rng.IntN(maxW + 3), H: rng.IntN(maxH + 3)}
		}
		sub, clip, fill := randRect(w, h), randRect(w, h), randRect(w, h)
		useSub, useClip := rng.IntN(2) == 0, rng.IntN(2) == 0
		grapheme := graphemes[rng.IntN(len(graphemes))]
		canvasOf := func(b *Buffer) *Canvas {
			c := NewCanvas(b)
			if useSub {
				c = c.Subview(sub)
			}
			if useClip {
				c = c.Clip(clip)
			}
			return c
		}
		got, want := seed.Clone(), seed.Clone()
		canvasOf(got).Fill(fill, grapheme, styleB)
		fillReference(canvasOf(want), fill, grapheme, styleB)
		for i := range want.cells {
			if got.cells[i] != want.cells[i] {
				t.Fatalf("round %d: %dx%d sub=%v(%v) clip=%v(%v) fill=%v %q: cell %d = %#v, want %#v",
					round, w, h, sub, useSub, clip, useClip, fill, grapheme, i, got.cells[i], want.cells[i])
			}
		}
	}
}
