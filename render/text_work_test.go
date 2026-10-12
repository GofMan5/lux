package render

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/rivo/uniseg"
)

// Independent canvases may be drawn concurrently; no package-global draw
// instrumentation or scratch storage may introduce shared mutable state.
func TestTextIndependentCanvases(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			canvas := NewCanvas(NewBuffer(20, 2))
			for range 50 {
				canvas.Text(0, 0, "hello 界\nworld", Style{})
			}
		}()
	}
	wg.Wait()
}

// referenceText preserves the pre-optimization algorithm. Compare both the
// whole backing buffer and the widest-input-line return, including hidden
// lines, CR resets, tab stops, invalid input and policy-dependent widths.
func referenceText(c *Canvas, x, y int, text string, style Style) int {
	startX, cursorX, cursorY, widest := x, x, y, 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch grapheme {
		case "\n", "\r\n":
			widest = max(widest, cursorX-startX)
			cursorX, cursorY = startX, cursorY+1
		case "\r":
			widest = max(widest, cursorX-startX)
			cursorX = startX
		case "\t":
			spaces := 4 - (cursorX-startX)%4
			for range spaces {
				c.Put(cursorX, cursorY, " ", style)
				cursorX++
			}
		default:
			if !printable(grapheme) {
				continue
			}
			width := terminalWidthOf(grapheme, c.WidthPolicy())
			if width == 0 {
				c.appendCombining(cursorX, cursorY, grapheme)
			} else {
				c.Put(cursorX, cursorY, grapheme, style)
				cursorX += width
			}
		}
	}
	return max(widest, cursorX-startX)
}

func textTestCanvas(policy WidthPolicy, geometry byte) *Canvas {
	if geometry == 0 {
		return nil
	}
	if geometry == 1 {
		return &Canvas{}
	}
	c := NewCanvasWithPolicy(NewBuffer(12, 6), policy)
	c.Fill(c.Bounds(), ".", Style{Attrs: AttrItalic})
	// Seed wide cells and links across possible clipping boundaries: a text
	// replacement must not split an existing wide cell or retain its link.
	for y := range 6 {
		c.PutCell(1, y, Cell{Grapheme: "界", Link: Link{URI: "https://example.org"}})
		c.Put(9, y, "界", Style{Attrs: AttrBold})
	}
	switch geometry {
	case 3:
		return c.Clip(Rect{X: 2, Y: 2, W: 8, H: 2})
	case 4:
		return c.Subview(Rect{X: 1, Y: 1, W: 10, H: 4}).Clip(Rect{X: 1, Y: 1, W: 7, H: 2})
	case 5:
		return c.Clip(Rect{})
	default:
		return c
	}
}

func checkTextReference(t *testing.T, text string, x, y int, flags, geometry byte) {
	t.Helper()
	policy := WidthPolicy{VS16Blind: flags&2 != 0, NarrowEmoji: flags&4 != 0}
	if flags&1 != 0 {
		policy.AmbiguousWidth = 2
	}
	got, want := textTestCanvas(policy, geometry), textTestCanvas(policy, geometry)
	style := Style{Foreground: RGBColor(30, 80, 120), Attrs: AttrUnderline}
	gotWidth, wantWidth := got.Text(x, y, text, style), referenceText(want, x, y, text, style)
	if gotWidth != wantWidth {
		t.Fatalf("Text(%d,%d,%q) width=%d, want %d (policy=%+v, geometry=%d)", x, y, text, gotWidth, wantWidth, policy, geometry)
	}
	if got.Buffer() == nil {
		return
	}
	for cy := range 6 {
		for cx := range 12 {
			if g, w := got.Buffer().Cell(cx, cy), want.Buffer().Cell(cx, cy); g != w {
				t.Fatalf("Text(%d,%d,%q) cell(%d,%d)=%#v, want %#v (policy=%+v, geometry=%d)", x, y, text, cx, cy, g, w, policy, geometry)
			}
		}
	}
}

func TestTextMatchesReference(t *testing.T) {
	texts := []string{
		"", "alpha\tb\ngamma\ndelta\nepsilon", "ab\ncde\r\nfg\rhij\nklmno",
		"\u0301a界\u0301🇺🇸👨‍👩‍👧‍👦©️·→\n\tX", "01234567890123456789\rZ\t!",
		"a\x00b\x1b[31m\x7f\xff\xc0\x80\n\u200d\u0301", "\t\t\r\n\n\t",
	}
	maxInt := int(^uint(0) >> 1)
	origins := [][2]int{{0, -3}, {-2, -1}, {0, 0}, {1, 1}, {9, 2}, {11, 3}, {12, 0}, {1, 6}, {maxInt, 0}, {0, maxInt}, {-maxInt - 1, 0}}
	for flags := byte(0); flags < 8; flags++ {
		for geometry := byte(0); geometry < 6; geometry++ {
			for _, origin := range origins {
				for _, text := range texts {
					checkTextReference(t, text, origin[0], origin[1], flags, geometry)
				}
			}
		}
	}
}

func FuzzTextMatchesReference(f *testing.F) {
	for _, text := range []string{"alpha\tb\ngamma", "界\u0301🇫🇷©️\rX", "\xff\n\t\r\n\u0301"} {
		f.Add(text, byte(0), byte(0), byte(0), byte(2))
	}
	f.Fuzz(func(t *testing.T, text string, x, y, flags, geometry byte) {
		if len(text) > 4096 {
			t.Skip()
		}
		checkTextReference(t, text, int(x%20)-4, int(y%12)-4, flags, geometry%6)
	})
}

var textBenchmarkWidth int

func BenchmarkText(b *testing.B) {
	for _, tc := range []struct {
		name, text string
		x, y       int
		clip       Rect
	}{
		{"visible-ascii", strings.Repeat("the quick brown fox ", 6), 0, 0, Bounds(120, 40)},
		{"visible-egc", strings.Repeat("界é🇫🇷👨‍👩‍👧‍👦→ ", 10), 0, 0, Bounds(120, 40)},
		{"tall-overscroll", strings.Repeat(strings.Repeat("x", 120)+"\n", 2000), 0, -1000, Bounds(120, 40)},
		{"clip-band", strings.Repeat(strings.Repeat("x", 120)+"\n", 40), 0, 0, Rect{Y: 15, W: 120, H: 5}},
		{"exhausted-columns", strings.Repeat("x", 2000) + "\rZ", 0, 0, Bounds(120, 40)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := NewCanvas(NewBuffer(120, 40)).Clip(tc.clip)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				textBenchmarkWidth = c.Text(tc.x, tc.y, tc.text, Style{})
			}
		})
	}
}

func ExampleCanvas_Text_offscreenMeasurement() {
	canvas := NewCanvas(NewBuffer(4, 1))
	fmt.Println(canvas.Text(0, -1, "hidden longest\nseen\nhidden\tend", Style{}))
	fmt.Println(canvas.Buffer().Cell(0, 0).Grapheme)
	// Output:
	// 14
	// s
}
