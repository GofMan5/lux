package render

import (
	"math/rand"
	"testing"
)

// The bulk-copy fill must leave every cell equal to the plain loop's result.
func TestFillCellsMatchesLoop(t *testing.T) {
	cell := Cell{Grapheme: "x", Width: 1, Style: Style{Foreground: RGBColor(1, 2, 3)}, Link: Link{URI: "u"}}
	for n := 0; n <= 300; n++ {
		cells := make([]Cell, n+1)
		fillCells(cells[:n], cell)
		for i := range n {
			if cells[i] != cell {
				t.Fatalf("len %d: cell %d = %+v", n, i, cells[i])
			}
		}
		if cells[n] != (Cell{}) {
			t.Fatalf("len %d: fill wrote past the slice", n)
		}
	}
}

// replacementFits against the original CellOK-based scan, including widths
// that run off either edge of a deliberately inconsistent row.
func TestReplacementFitsMatchesReference(t *testing.T) {
	reference := func(b *Buffer, clip Rect, x, y, width int) bool {
		left, right := x, x+width-1
		for column := left; column <= right; column++ {
			cell, ok := b.CellOK(column, y)
			if !ok {
				return false
			}
			switch cell.Width {
			case 0:
				left = min(left, column-1)
			case 2:
				right = max(right, column+1)
			}
		}
		return clip.Contains(left, y) && clip.Contains(right, y)
	}
	random := rand.New(rand.NewSource(58))
	for trial := 0; trial < 2000; trial++ {
		b := NewBuffer(1+random.Intn(8), 1+random.Intn(3))
		for i := range b.cells {
			b.cells[i].Width = uint8(random.Intn(3))
		}
		clip := Rect{X: random.Intn(3) - 1, Y: random.Intn(3) - 1, W: random.Intn(10), H: random.Intn(4)}
		x, y, width := random.Intn(b.width+2)-1, random.Intn(b.height+2)-1, 1+random.Intn(2)
		if got, want := b.replacementFits(clip, x, y, width), reference(b, clip, x, y, width); got != want {
			t.Fatalf("trial %d: replacementFits(%v, %d, %d, %d) = %v; want %v", trial, clip, x, y, width, got, want)
		}
	}
}

// In-place Resize against the original allocate-and-copy version over chains
// of random sizes, so reused capacity sees every narrow/widen/grow/shrink mix.
func TestResizeMatchesReference(t *testing.T) {
	reference := func(b *Buffer, width, height int) *Buffer {
		r := &Buffer{width: width, height: height, cells: make([]Cell, width*height)}
		fillCells(r.cells, BlankCell)
		for y := 0; y < min(height, b.height); y++ {
			copy(r.cells[y*width:y*width+min(width, b.width)], b.cells[y*b.width:])
		}
		r.repair()
		return r
	}
	random := rand.New(rand.NewSource(59))
	for chain := 0; chain < 300; chain++ {
		b := NewBuffer(random.Intn(12), random.Intn(6))
		for step := 0; step < 12; step++ {
			canvas := NewCanvas(b)
			for i := 0; i < b.width*b.height/2; i++ {
				canvas.Text(random.Intn(b.width+1), random.Intn(b.height+1), []string{"a", "汉", "bc"}[random.Intn(3)], Style{Foreground: RGBColor(uint8(i), 0, 0)})
			}
			width, height := random.Intn(12), random.Intn(6)
			want := reference(b, width, height)
			b.Resize(width, height)
			if !b.Equal(want) {
				t.Fatalf("chain %d step %d: Resize(%d, %d) differs from reference", chain, step, width, height)
			}
			for _, cell := range b.cells[len(b.cells):cap(b.cells)] {
				if cell != (Cell{}) {
					t.Fatalf("chain %d step %d: stale cell %+v kept past the end", chain, step, cell)
				}
			}
		}
	}
}

func BenchmarkResizeAlternating(b *testing.B) {
	buffer := NewBuffer(160, 53)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buffer.Resize(120+40*(i%2), 40+13*(i%2))
	}
}
