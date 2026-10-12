package render

import (
	"testing"

	"github.com/rivo/uniseg"
)

// Row painters measure a cluster (GraphemeWidth) and then draw it; before
// PutMeasured existed they re-measured inside Put. This class pins that the
// single-measurement path is cell-for-cell equivalent to Put for every
// printable, zero-width, control, and invalid cluster a row can produce.
var putMeasuredClusters = []string{
	"a", "Z", "0", " ", "·", "é", "界", "🙂", "🇫🇷", "👨‍👩‍👧‍👦",
	"e\u0301", "\u0301", "©", "©️", "→", "→\uFE0F",
	"\x1b", "\x00", "\x07", "\x7f", "\xff", "\xc0\x80", "",
}

func checkPutMeasuredReference(t *testing.T, cluster string, x, y int, flags, geometry byte) {
	t.Helper()
	policy := WidthPolicy{VS16Blind: flags&2 != 0, NarrowEmoji: flags&4 != 0}
	if flags&1 != 0 {
		policy.AmbiguousWidth = 2
	}
	got, want := textTestCanvas(policy, geometry), textTestCanvas(policy, geometry)
	style := Style{Foreground: RGBColor(200, 40, 90), Attrs: AttrUnderline}
	width := got.GraphemeWidth(cluster)
	if advance := want.Put(x, y, cluster, style); advance != width {
		t.Fatalf("Put(%q) advance=%d, GraphemeWidth=%d (policy=%+v)", cluster, advance, width, policy)
	}
	got.PutMeasured(x, y, cluster, width, style)
	if got.Buffer() == nil {
		return
	}
	for cy := range 6 {
		for cx := range 12 {
			if g, w := got.Buffer().Cell(cx, cy), want.Buffer().Cell(cx, cy); g != w {
				t.Fatalf("PutMeasured(%d,%d,%q,%d) cell(%d,%d)=%#v, Put gave %#v (policy=%+v, geometry=%d)", x, y, cluster, width, cx, cy, g, w, policy, geometry)
			}
		}
	}
}

func TestPutMeasuredMatchesPut(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	origins := [][2]int{{0, 0}, {1, 1}, {-1, 2}, {0, -2}, {11, 5}, {12, 0}, {5, 6}, {maxInt, 0}, {0, maxInt}}
	for flags := byte(0); flags < 8; flags++ {
		for geometry := byte(0); geometry < 6; geometry++ {
			for _, origin := range origins {
				for _, cluster := range putMeasuredClusters {
					checkPutMeasuredReference(t, cluster, origin[0], origin[1], flags, geometry)
				}
			}
		}
	}
}

// FuzzPutMeasuredMatchesPut walks arbitrary fuzzed text the way a row painter
// does — measure, draw, advance by the measured width — and requires the
// resulting buffer to be identical to the pre-optimization Put-per-cluster
// walk, including invalid input and policy-dependent widths.
func FuzzPutMeasuredMatchesPut(f *testing.F) {
	for _, text := range []string{"a界e\u0301🙂\n", "🇫🇷👨‍👩‍👧‍👦©️\rX", "\xff\x00\u0301\t", "plain ascii rows"} {
		f.Add(text, byte(0), byte(0), byte(0), byte(2))
	}
	f.Fuzz(func(t *testing.T, text string, x, y, flags, geometry byte) {
		if len(text) > 512 {
			t.Skip()
		}
		policy := WidthPolicy{VS16Blind: flags&2 != 0, NarrowEmoji: flags&4 != 0}
		if flags&1 != 0 {
			policy.AmbiguousWidth = 2
		}
		got, want := textTestCanvas(policy, geometry%6), textTestCanvas(policy, geometry%6)
		style := Style{Attrs: AttrBold}
		cx, cy := int(x%16)-2, int(y%10)-2
		graphemes := uniseg.NewGraphemes(text)
		for graphemes.Next() {
			cluster := graphemes.Str()
			width := got.GraphemeWidth(cluster)
			got.PutMeasured(cx, cy, cluster, width, style)
			want.Put(cx, cy, cluster, style)
			cx += width
		}
		if got.Buffer() == nil {
			return
		}
		for row := range 6 {
			for col := range 12 {
				if g, w := got.Buffer().Cell(col, row), want.Buffer().Cell(col, row); g != w {
					t.Fatalf("row walk %q cell(%d,%d)=%#v, Put walk gave %#v (policy=%+v, geometry=%d)", text, col, row, g, w, policy, geometry%6)
				}
			}
		}
	})
}

// BenchmarkPutMeasured prices the single-measurement write path directly
// against Put's derive-and-write path for one ASCII and one wide cluster.
func BenchmarkPutMeasured(b *testing.B) {
	for _, tc := range []struct {
		name    string
		cluster string
	}{
		{"ascii", "a"},
		{"wide", "界"},
		{"combining", "e\u0301"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := NewCanvas(NewBuffer(120, 40))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := c.GraphemeWidth(tc.cluster)
				c.PutMeasured(i%120, 0, tc.cluster, w, Style{})
			}
		})
	}
}
