package render

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestWrapLinesWordGoldens(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		limit      int
		wantLines  []string
		wantWidth  int
		wantHeight int
	}{
		{"plain fits", "ab cd", 8, []string{"ab cd"}, 5, 1},
		{"break at space", "ab cd", 3, []string{"ab", "cd"}, 2, 2},
		{"consumes whitespace at soft break", "ab  cd", 4, []string{"ab", "cd"}, 2, 2},
		{"keeps trailing whitespace at end", "ab ", 3, []string{"ab "}, 3, 1},
		{"keeps whitespace before hard break", "ab \ncd", 8, []string{"ab ", "cd"}, 3, 2},
		{"long word falls back to char wrap", "abcdefghij", 3, []string{"abc", "def", "ghi", "j"}, 3, 4},
		{"short word then long word", "hi abcdef", 3, []string{"hi", "abc", "def"}, 3, 3},
		{"indentation is kept", "  ab cd", 4, []string{"  ab", "cd"}, 4, 2},
		{"indentation wider than limit overflows", "     a", 2, []string{"     ", "a"}, 5, 2},
		{"wide grapheme leaves one-cell gap", "aa日b", 3, []string{"aa", "日b"}, 3, 2},
		{"zwj family never split", "aa👨‍👩‍👧b", 3, []string{"aa", "👨‍👩‍👧b"}, 3, 2},
		{"zwj family at boundary fits", "a👨‍👩‍👧b", 4, []string{"a👨‍👩‍👧b"}, 4, 1},
		{"wide grapheme exactly at limit", "a日", 3, []string{"a日"}, 3, 1},
		{"wide grapheme under one-cell limit", "日", 1, []string{"日"}, 2, 1},
		{"tab fits with stops", "a\tb", 5, []string{"a\tb"}, 5, 1},
		{"soft break at tab consumes it", "a\tb", 4, []string{"a", "b"}, 1, 2},
		{"crlf is one hard break", "ab\r\ncd", 10, []string{"ab", "cd"}, 2, 2},
		{"lone cr is a hard break", "ab\rcd", 10, []string{"ab", "cd"}, 2, 2},
		{"trailing newline keeps empty line", "a\n", 10, []string{"a", ""}, 1, 2},
		{"blank line between", "a\n\nb", 10, []string{"a", "", "b"}, 1, 3},
		{"only a newline", "\n", 5, []string{"", ""}, 0, 2},
		{"control graphemes cost no cells and stay", "a\x00b", 5, []string{"a\x00b"}, 2, 1},
		{"zwj boundary survives an invalid byte", "👨\u200d\xa9👧0", 3, []string{"👨\u200d\xa9", "👧0"}, 3, 2},
		{"nbsp is word content not a break", "a\u00a0b", 3, []string{"a\u00a0b"}, 3, 1},
		{"zero limit disables soft wrap", "ab cd\nef", 0, []string{"ab cd", "ef"}, 5, 2},
		{"negative limit disables soft wrap", "ab cd", -1, []string{"ab cd"}, 5, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLines := WrapLines(tc.text, tc.limit, WrapOptions{})
			if len(gotLines) != len(tc.wantLines) {
				t.Fatalf("lines = %#v, want %#v", gotLines, tc.wantLines)
			}
			for i := range tc.wantLines {
				if gotLines[i] != tc.wantLines[i] {
					t.Fatalf("line %d = %q, want %q (all: %#v)", i, gotLines[i], tc.wantLines[i], gotLines)
				}
			}
			gotMetrics := MeasureWrap(tc.text, tc.limit, WrapOptions{})
			wantMetrics := TextMetrics{Width: tc.wantWidth, Height: tc.wantHeight}
			if gotMetrics != wantMetrics {
				t.Fatalf("metrics = %+v, want %+v", gotMetrics, wantMetrics)
			}
		})
	}
}

func TestWrapLinesCharModeGoldens(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		limit      int
		wantLines  []string
		wantWidth  int
		wantHeight int
	}{
		{"whitespace kept as cells", "ab cd", 3, []string{"ab ", "cd"}, 3, 2},
		{"zwj family never split", "aa👨‍👩‍👧b", 3, []string{"aa", "👨‍👩‍👧b"}, 3, 2},
		{"family moves whole to next line", "a👨‍👩‍👧b", 3, []string{"a👨‍👩‍👧", "b"}, 3, 2},
		{"wide grapheme under one-cell limit", "日", 1, []string{"日"}, 2, 1},
		{"wide grapheme leaves one-cell gap", "a日", 1, []string{"a", "日"}, 2, 2},
		{"tab is one unit that fits", "a\tb", 4, []string{"a\t", "b"}, 4, 2},
		{"tab overflows a small limit alone", "a\tb", 2, []string{"a", "\t", "b"}, 4, 3},
		{"leading spaces wrap as cells", "   ab", 3, []string{"   ", "ab"}, 3, 2},
		{"trailing newline keeps empty line", "a\n", 10, []string{"a", ""}, 1, 2},
		{"control graphemes cost no cells and stay", "a\x1bb", 5, []string{"a\x1bb"}, 2, 1},
		{"zero limit disables soft wrap", "ab cd", 0, []string{"ab cd"}, 5, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLines := WrapLines(tc.text, tc.limit, WrapOptions{Mode: WrapChar})
			if len(gotLines) != len(tc.wantLines) {
				t.Fatalf("lines = %#v, want %#v", gotLines, tc.wantLines)
			}
			for i := range tc.wantLines {
				if gotLines[i] != tc.wantLines[i] {
					t.Fatalf("line %d = %q, want %q (all: %#v)", i, gotLines[i], tc.wantLines[i], gotLines)
				}
			}
			gotMetrics := MeasureWrap(tc.text, tc.limit, WrapOptions{Mode: WrapChar})
			wantMetrics := TextMetrics{Width: tc.wantWidth, Height: tc.wantHeight}
			if gotMetrics != wantMetrics {
				t.Fatalf("metrics = %+v, want %+v", gotMetrics, wantMetrics)
			}
		})
	}
}

func TestWrapEmptyInput(t *testing.T) {
	for _, mode := range []WrapMode{WrapWord, WrapChar} {
		opts := WrapOptions{Mode: mode}
		if got := WrapLines("", 10, opts); got != nil {
			t.Fatalf("mode %d lines = %#v, want nil", mode, got)
		}
		if got := MeasureWrap("", 10, opts); got != (TextMetrics{}) {
			t.Fatalf("mode %d metrics = %+v, want zero", mode, got)
		}
	}
}

func TestWrapPolicyChangesBreaks(t *testing.T) {
	// Default policy: each presentation emoji is two cells, so two emoji
	// cannot share a three-cell line.
	if got := WrapLines("😀😀", 3, WrapOptions{}); len(got) != 2 {
		t.Fatalf("default policy lines = %#v, want two lines", got)
	}
	// NarrowEmoji terminals measure one cell each: the same text now fits.
	policy := WidthPolicy{NarrowEmoji: true}
	if got := WrapLinesWith("😀😀", 3, WrapOptions{}, policy); len(got) != 1 || got[0] != "😀😀" {
		t.Fatalf("narrow-emoji policy lines = %#v, want one unsplit line", got)
	}
	if got := MeasureWrapWith("😀😀", 3, WrapOptions{}, policy); got != (TextMetrics{Width: 2, Height: 1}) {
		t.Fatalf("narrow-emoji metrics = %+v, want width 2 height 1", got)
	}
}

func TestWrapZeroPolicyMatchesDefault(t *testing.T) {
	text := "aa👨‍👩‍👧b 折返宽度 ab cd\ttab\r\nfinal\n"
	for _, mode := range []WrapMode{WrapWord, WrapChar} {
		opts := WrapOptions{Mode: mode}
		if got, want := WrapLinesWith(text, 7, opts, WidthPolicy{}), WrapLines(text, 7, opts); len(got) != len(want) {
			t.Fatalf("mode %d zero-policy lines = %#v, want %#v", mode, got, want)
		}
	}
}

func TestMeasureWrapMatchesWrappedLines(t *testing.T) {
	texts := []string{
		"ab cd ef gh ij kl mn op",
		"aa👨‍👩‍👧b and a 日 wide gap",
		"tabs\there\tand\tthere",
		"first\r\nsecond\nthird\r",
		"     deep indentation",
	}
	policies := []WidthPolicy{{}, {NarrowEmoji: true}, {AmbiguousWidth: 2}}
	for _, mode := range []WrapMode{WrapWord, WrapChar} {
		opts := WrapOptions{Mode: mode}
		for _, policy := range policies {
			for _, limit := range []int{1, 3, 5, 8} {
				for _, text := range texts {
					lines := WrapLinesWith(text, limit, opts, policy)
					metrics := MeasureWrapWith(text, limit, opts, policy)
					if metrics.Height != len(lines) {
						t.Fatalf("mode %d policy %+v limit %d height %d, want %d (%#v)",
							mode, policy, limit, metrics.Height, len(lines), lines)
					}
					width := 0
					for _, line := range lines {
						if w := TextWidthWith(line, policy); w > width {
							width = w
						}
					}
					if metrics.Width != width {
						t.Fatalf("mode %d policy %+v limit %d width %d, want %d (lines %#v)",
							mode, policy, limit, metrics.Width, width, lines)
					}
				}
			}
		}
	}
}

func TestCanvasTextWrapGolden(t *testing.T) {
	buffer := NewBuffer(12, 4)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(200, 200, 200)}
	metrics := canvas.TextWrap(Rect{X: 0, Y: 0, W: 5, H: 4}, "ab cd ef", WrapOptions{}, style)
	if metrics != (TextMetrics{Width: 5, Height: 2}) {
		t.Fatalf("metrics = %+v, want width 5 height 2", metrics)
	}
	if got := buffer.Cell(0, 0).Grapheme; got != "a" {
		t.Fatalf("row 0 col 0 = %q", got)
	}
	if got := buffer.Cell(4, 0).Grapheme; got != "d" {
		t.Fatalf("row 0 col 4 = %q, want d", got)
	}
	if got := buffer.Cell(5, 0).Grapheme; got != " " {
		t.Fatalf("row 0 col 5 = %q, want blank beyond rect", got)
	}
	if got := buffer.Cell(0, 1).Grapheme; got != "e" {
		t.Fatalf("row 1 col 0 = %q, want e", got)
	}
	if buffer.Cell(0, 0).Style.Foreground != style.Foreground {
		t.Fatalf("style not applied to wrapped text")
	}
}

func TestCanvasTextWrapClipsRowsButCountsOverflow(t *testing.T) {
	buffer := NewBuffer(12, 2)
	metrics := NewCanvas(buffer).TextWrap(Rect{X: 0, Y: 0, W: 5, H: 1}, "ab cd ef", WrapOptions{}, Style{})
	if metrics != (TextMetrics{Width: 5, Height: 2}) {
		t.Fatalf("metrics = %+v, want height 2 counting clipped row", metrics)
	}
	if got := buffer.Cell(0, 1).Grapheme; got != " " {
		t.Fatalf("second row drawn despite rect H=1: %q", got)
	}
}

func TestCanvasTextWrapDrawsWideGapAndNeverSplits(t *testing.T) {
	buffer := NewBuffer(6, 2)
	metrics := NewCanvas(buffer).TextWrap(Rect{X: 0, Y: 0, W: 3, H: 2}, "aa日b", WrapOptions{}, Style{})
	if metrics != (TextMetrics{Width: 3, Height: 2}) {
		t.Fatalf("metrics = %+v, want width 3 height 2", metrics)
	}
	if got := buffer.Cell(0, 0).Grapheme; got != "a" {
		t.Fatalf("row 0 col 0 = %q", got)
	}
	if got := buffer.Cell(2, 0).Grapheme; got != " " {
		t.Fatalf("wide grapheme straddling the limit must leave col 2 blank, got %q", got)
	}
	if got := buffer.Cell(0, 1).Grapheme; got != "日" {
		t.Fatalf("row 1 col 0 = %q, want wide grapheme whole", got)
	}
	if got := buffer.Cell(1, 1).Grapheme; got != "" {
		t.Fatalf("row 1 col 1 = %q, want wide spacer cell", got)
	}
	if got := buffer.Cell(2, 1).Grapheme; got != "b" {
		t.Fatalf("row 1 col 2 = %q, want b after the spacer", got)
	}
}

func TestCanvasTextWrapTabStopsRelativeToLineStart(t *testing.T) {
	buffer := NewBuffer(8, 1)
	metrics := NewCanvas(buffer).TextWrap(Rect{X: 0, Y: 0, W: 5, H: 1}, "a\tb", WrapOptions{}, Style{})
	if metrics != (TextMetrics{Width: 5, Height: 1}) {
		t.Fatalf("metrics = %+v, want width 5 height 1", metrics)
	}
	if got := buffer.Cell(0, 0).Grapheme; got != "a" {
		t.Fatalf("col 0 = %q", got)
	}
	if got := buffer.Cell(1, 0).Grapheme; got != " " {
		t.Fatalf("tab must skip cols 1-3, col 1 = %q", got)
	}
	if got := buffer.Cell(4, 0).Grapheme; got != "b" {
		t.Fatalf("tab stop puts b at col 4, got %q", got)
	}
}

func TestCanvasTextWrapFollowsCanvasPolicy(t *testing.T) {
	narrow := NewCanvasWithPolicy(NewBuffer(8, 2), WidthPolicy{NarrowEmoji: true})
	metrics := narrow.TextWrap(Rect{X: 0, Y: 0, W: 3, H: 2}, "😀😀", WrapOptions{}, Style{})
	if metrics != (TextMetrics{Width: 2, Height: 1}) {
		t.Fatalf("narrow policy metrics = %+v, want one line", metrics)
	}
	defaultCanvas := NewCanvas(NewBuffer(8, 2))
	metrics = defaultCanvas.TextWrap(Rect{X: 0, Y: 0, W: 3, H: 2}, "😀😀", WrapOptions{}, Style{})
	if metrics != (TextMetrics{Width: 2, Height: 2}) {
		t.Fatalf("default metrics = %+v, want two lines", metrics)
	}
}

func TestCanvasTextWrapNilCanvas(t *testing.T) {
	var canvas *Canvas
	if got := canvas.TextWrap(Rect{X: 0, Y: 0, W: 5, H: 2}, "ab", WrapOptions{}, Style{}); got != (TextMetrics{}) {
		t.Fatalf("nil canvas metrics = %+v, want zero", got)
	}
}

func TestCanvasTextWrapMatchesStandaloneWrap(t *testing.T) {
	text := "The wrap limit comes from the rectangle and the policy from the canvas.\nBoth must agree with WrapLinesWith."
	buffer := NewBuffer(30, 10)
	canvas := NewCanvasWithPolicy(buffer, WidthPolicy{AmbiguousWidth: 2})
	rect := Rect{X: 1, Y: 1, W: 28, H: 8}
	got := canvas.TextWrap(rect, text, WrapOptions{}, Style{})
	want := MeasureWrapWith(text, rect.W, WrapOptions{}, WidthPolicy{AmbiguousWidth: 2})
	if got != want {
		t.Fatalf("canvas metrics %+v, standalone %+v", got, want)
	}
	for i, line := range WrapLinesWith(text, rect.W, WrapOptions{}, WidthPolicy{AmbiguousWidth: 2}) {
		if buffer.Cell(rect.X, rect.Y+i).Grapheme != firstGraphemeCell(line) {
			t.Fatalf("row %d first cell mismatch with wrapped line %q", i, line)
		}
	}
}

func firstGraphemeCell(line string) string {
	graphemes := uniseg.NewGraphemes(line)
	if graphemes.Next() {
		return graphemes.Str()
	}
	return ""
}

// drawCells counts the cells Canvas.Text actually occupies for a wrapped
// line: tabs advance to their four-column stops, printable graphemes cost
// their terminal width, and anything Canvas.Text skips (control
// graphemes, invalid bytes) costs nothing.
func drawCells(line string) int {
	col := 0
	graphemes := uniseg.NewGraphemes(line)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch {
		case grapheme == "\t":
			col += 4 - col%4
		case printable(grapheme):
			col += terminalWidthOf(grapheme, WidthPolicy{})
		}
	}
	return col
}

func wrapInvariantsHold(t *testing.T, text string, limit int, opts WrapOptions) {
	t.Helper()
	lines := WrapLines(text, limit, opts)
	metrics := MeasureWrap(text, limit, opts)
	if metrics.Height != len(lines) {
		t.Fatalf("height %d, want %d lines", metrics.Height, len(lines))
	}
	width := 0
	for _, line := range lines {
		if w := drawCells(line); w > width {
			width = w
		}
	}
	if metrics.Width != width {
		t.Fatalf("width %d, want %d over %#v", metrics.Width, width, lines)
	}
	// The concatenation of the wrapped lines must be a subsequence of the
	// input graphemes: wrap may consume whitespace at soft breaks and spend
	// hard breaks splitting lines, but it must never invent, reorder, or
	// silently drop content (deleting a grapheme could merge neighboring
	// clusters and change how the line is measured).
	var input []string
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		input = append(input, graphemes.Str())
	}
	droppable := func(g string) bool {
		if g == "\n" || g == "\r\n" || g == "\r" {
			return true
		}
		if opts.Mode == WrapWord && (g == " " || g == "\t") {
			return true
		}
		return false
	}
	idx := 0
	for _, line := range lines {
		lineGraphemes := uniseg.NewGraphemes(line)
		for lineGraphemes.Next() {
			grapheme := lineGraphemes.Str()
			found := false
			for idx < len(input) {
				if input[idx] == grapheme {
					idx++
					found = true
					break
				}
				if !droppable(input[idx]) {
					t.Fatalf("wrap dropped grapheme %q from %#v", input[idx], lines)
				}
				idx++
			}
			if !found {
				t.Fatalf("wrap invented grapheme %q (lines %#v)", grapheme, lines)
			}
		}
	}
	for idx < len(input) && droppable(input[idx]) {
		idx++
	}
	if idx != len(input) {
		t.Fatalf("input grapheme %q missing from output (lines %#v)", input[idx], lines)
	}
}

func FuzzWrapLinesNeverPanicsOrInvents(f *testing.F) {
	seeds := []string{
		"ab cd ef",
		"aa👨‍👩‍👧b and a 日 wide gap",
		"a\n",
		"\r\n\r",
		"     a",
		"a\tb\tc",
		"日本語のテキストは折り返す",
		"😀😀😀",
		"a\x00b\x1b",
		strings.Repeat("word ", 20),
	}
	for _, seed := range seeds {
		f.Add(seed, uint8(3))
		f.Add(seed, uint8(0))
	}
	f.Fuzz(func(t *testing.T, text string, limitByte uint8) {
		limit := int(limitByte) % 24
		wrapInvariantsHold(t, text, limit, WrapOptions{})
		wrapInvariantsHold(t, text, limit, WrapOptions{Mode: WrapChar})
	})
}

// BenchmarkWrapWords64KiB prices the steady path of word wrap: one pass
// over graphemes, one line buffer reused per wrapped line, break
// decisions made from the running column. This is the number the
// stage-7 wrap budget refers to.
func BenchmarkWrapWords64KiB(b *testing.B) {
	text := strings.Repeat("word ", 13107) // ~64 KiB of breakable words
	b.ReportAllocs()
	for range b.N {
		if m := MeasureWrap(text, 60, WrapOptions{}); m.Height == 0 {
			b.Fatal("no wrapped lines")
		}
	}
}

// BenchmarkWrapUnbroken64KiB prices the worst case: a single 64 KiB word
// that must fall back to char wrap, so every grapheme is measured and
// line breaks happen between graphemes, not words.
func BenchmarkWrapUnbroken64KiB(b *testing.B) {
	text := strings.Repeat("ab", 32768) // one 64 KiB unbreakable run
	b.ReportAllocs()
	for range b.N {
		if m := MeasureWrap(text, 60, WrapOptions{}); m.Height == 0 {
			b.Fatal("no wrapped lines")
		}
	}
}

// BenchmarkWrapCharMode64KiB prices char mode on the same breakable text
// as BenchmarkWrapWords64KiB, isolating what word-boundary bookkeeping
// costs against breaking between every grapheme.
func BenchmarkWrapCharMode64KiB(b *testing.B) {
	text := strings.Repeat("word ", 13107)
	b.ReportAllocs()
	for range b.N {
		if m := MeasureWrap(text, 60, WrapOptions{Mode: WrapChar}); m.Height == 0 {
			b.Fatal("no wrapped lines")
		}
	}
}

// BenchmarkCanvasTextWrapParagraph prices the full draw path of
// TextWrap at a typical 120x40 frame: wrap measurement plus per-line
// Text draws with the shared style, the way a widget fills its rect.
func BenchmarkCanvasTextWrapParagraph(b *testing.B) {
	canvas := NewCanvas(NewBuffer(120, 40))
	style := Style{Foreground: RGBColor(220, 225, 235)}
	text := strings.Repeat("Lux TUI measures every grapheme before it wraps. ", 120)
	b.ReportAllocs()
	for range b.N {
		canvas.TextWrap(Rect{X: 1, Y: 1, W: 118, H: 38}, text, WrapOptions{}, style)
	}
}
