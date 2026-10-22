package render

import (
	"testing"

	"github.com/rivo/uniseg"
)

// Golden widths for the Unicode 18.0.0 overlay (see widthoverlay.go for how
// the range tables were derived). Every expected value is the model width
// from the raw 18.0.0 UCD data, not the old uniseg result — that difference
// is the entire point of the overlay.

func TestOverlayRune(t *testing.T) {
	tests := []struct {
		name  string
		r     rune
		width int
		ok    bool
	}{
		{"trigram heaven (16.0 EAW promotion, W)", 0x2630, 2, true},
		{"hexagram block end", 0x4DFF, 2, true},
		{"monogram for earth (16.0 EAW promotion, W)", 0x1D300, 2, true},
		{"jurchen supplement start (18.0, W)", 0x18E00, 2, true},
		{"jurchen supplement end", 0x19191, 2, true},
		{"tomobiki symbol (15.1 EAW promotion)", 0x1F1AE, 2, true},
		{"tulu-tigalari vowel sign AA (16.0 Extend)", 0x113B8, 0, true},
		{"musical sprechgesang stem (16.0 Extend reclass)", 0x1D166, 0, true},
		{"musical combining stress (16.0 assignment)", 0x1D127, 0, true},
		{"latin ext-d prepended? no: extending mark 1ACF (16.0 Extend)", 0x1ACF, 0, true},
		{"below trigram block (unchanged legacy)", 0x262F, 0, false},
		{"above hexagram block", 0x4E00, 0, false},
		{"plain ASCII", 'a', 0, false},
		{"CJK legacy wide", 0x9E8C, 0, false},
		{"highest plane (unassigned legacy)", 0x10FFFF, 0, false},
	}
	for _, tc := range tests {
		w, ok := overlayRune(tc.r)
		if w != tc.width || ok != tc.ok {
			t.Errorf("%s: overlayRune(%U) = %d, %v; want %d, %v", tc.name, tc.r, w, ok, tc.width, tc.ok)
		}
	}
}

func TestRangeContainsBoundaries(t *testing.T) {
	if !rangeContains(overlayWide, 0x2630) || !rangeContains(overlayWide, 0x2637) {
		t.Error("range [0x2630, 0x2637] must contain both endpoints")
	}
	if rangeContains(overlayWide, 0x262F) || rangeContains(overlayWide, 0x2638) {
		t.Error("range [0x2630, 0x2637] must not contain neighbors")
	}
}

// The 18.0.0-aware terminal widths of single clusters. Clusters that contain
// no overlay codepoint must come out byte-for-byte identical to the raw
// uniseg results, including the exotics (2E3A/2E3B are 3/4 cells and get
// clamped, flags pair to 2, ZWJ families glue to 2, VS15 forces narrow).
func TestTerminalWidthOverlay(t *testing.T) {
	tests := []struct {
		name     string
		grapheme string
		want     int
	}{
		{"trigram heaven now 2", "\u2630", 2},
		{"hexagram for the creative heaven now 2", "\u4DC0", 2},
		{"monogram for earth now 2", "\u1D300", 2},
		{"jurchen character now 2", "\U00018E00", 2},
		{"tomobiki symbol now 2", "\U0001F1AE", 2},
		{"musical sprechgesang stem now 0", "\U0001D166", 0},
		{"musical combining stress now 0", "\U0001D127", 0},
		{"tulu-tigalari vowel sign now 0", "\U000113B8", 0},
		{"legacy latin", "a", 1},
		{"legacy combining cluster", "e\u0301", 1},
		{"legacy CJK", "安", 2},
		{"legacy flag pair", "\U0001F1FA\U0001F1F8", 2},
		{"legacy ZWJ family", "\U0001F468\u200D\U0001F469\u200D\U0001F466", 2},
		{"legacy two-em dash clamps to 2", "\u2E3A", 2},
		{"legacy three-em dash clamps to 2", "\u2E3B", 2},
		{"legacy VS15 text presentation", "\u2139\uFE0E", 1},
		{"legacy VS16 emoji presentation", "\u2139\uFE0F", 2},
		{"new Extend after legacy base", "e\U0001D166", 1},
		{"new Extend after emoji presentation", "\U0001F600\U000113B8", 2},
	}
	for _, tc := range tests {
		if got := terminalWidth(tc.grapheme); got != tc.want {
			t.Errorf("%s: terminalWidth(%q) = %d; want %d", tc.name, tc.grapheme, got, tc.want)
		}
	}
}

func TestOverlayClusterWidthFastPath(t *testing.T) {
	// Clusters without overlay codepoints must report ok=false so the caller
	// keeps the raw uniseg width (and never allocates a translated copy).
	for _, g := range []string{"a", "e\u0301", "安", "\U0001F1FA\U0001F1F8", "\u2E3A"} {
		if _, ok := overlayClusterWidth(g); ok {
			t.Errorf("overlayClusterWidth(%q) hit the table; want fast path", g)
		}
	}
	// A single overlay rune is enough to engage the translated path.
	if w, ok := overlayClusterWidth("\u2630"); !ok || w != 2 {
		t.Errorf("overlayClusterWidth(trigram) = %d, %v; want 2, true", w, ok)
	}
}

func TestTextWidthOverlay(t *testing.T) {
	// Mixed line: legacy ASCII, promoted trigram, new Extend, new Jurchen.
	// Widths: 1 + 2 + 0 + 2 = 5.
	line := "a\u2630\U0001D127\U00018E00"
	if got := TextWidth(line); got != 5 {
		t.Errorf("TextWidth(mixed overlay line) = %d; want 5", got)
	}
	if got := TextWidth("aa"); got != 2 {
		t.Errorf("TextWidth(legacy) = %d; want 2", got)
	}
}

func TestGraphemeWidthOverlay(t *testing.T) {
	if got := GraphemeWidth("\u2630"); got != 2 {
		t.Errorf("GraphemeWidth(trigram) = %d; want 2", got)
	}
	if got := GraphemeWidth("e\U0001D166"); got != 1 {
		t.Errorf("GraphemeWidth(base+new Extend) = %d; want 1", got)
	}
}

func TestCanvasTextRendersOverlayWidths(t *testing.T) {
	buffer := NewBuffer(8, 1)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(1, 2, 3)}

	// trigram now takes two cells, the new Extend rides along for free, the
	// new Jurchen character takes two more: 2 + 1 + 0 + 2 = 5.
	if width := canvas.Text(0, 0, "\u2630e\U0001D127\U00018E00", style); width != 5 {
		t.Fatalf("Text width = %d, want 5", width)
	}
	want := []Cell{
		{Grapheme: "\u2630", Width: 2, Style: style},
		{Width: 0, Style: style},
		{Grapheme: "e\U0001D127", Width: 1, Style: style},
		{Grapheme: "\U00018E00", Width: 2, Style: style},
		{Width: 0, Style: style},
	}
	for x, expected := range want {
		if got := buffer.Cell(x, 0); got != expected {
			t.Errorf("Cell(%d, 0) = %#v, want %#v", x, got, expected)
		}
	}
}

// BenchmarkTextWidthLegacy prices the overlay fast path on pure legacy text:
// every grapheme takes the two binary-search misses (wide, zero) and the
// translation is never built. Compare against BenchmarkTextWidthOverlay to
// see what an overlay-hit cluster costs; the wrap-budget acceptance in stage
// 7 item 4 builds on these numbers.
func BenchmarkTextWidthLegacy(b *testing.B) {
	line := "Lux TUI · smooth rendering, grapheme-safe layout and diffing · 60 FPS"
	b.ReportAllocs()
	for range b.N {
		TextWidth(line)
	}
}

func BenchmarkTextWidthOverlay(b *testing.B) {
	// Same shape as the legacy line but every third cluster is an overlay
	// hit: trigram (wide), jurchen (wide), musical Extend (zero).
	line := "a·\u2630 b·\U00018E00 c·\U0001D166 "
	b.ReportAllocs()
	for range b.N {
		TextWidth(line)
	}
}

func BenchmarkCanvasTextOverlay(b *testing.B) {
	buffer := NewBuffer(120, 40)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(220, 225, 235)}
	line := "☰ 𘀀 a𝅘𝅥𝅲 · trigram and jurchen wide, musical mark zero "
	b.ReportAllocs()
	for range b.N {
		canvas.Text(2, 20, line, style)
	}
}

// The printable-ASCII shortcut in terminalWidth must agree with the full
// overlay-then-uniseg path for every ASCII byte, including the controls it must not claim.
func TestTerminalWidthASCIIShortcutMatchesTables(t *testing.T) {
	for b := 0; b < 0x80; b++ {
		s := string(rune(b))
		if _, hit := overlayClusterWidth(s); hit {
			t.Fatalf("overlay claims ASCII %q", s)
		}
		if want := min(max(uniseg.StringWidth(s), 0), 2); terminalWidth(s) != want {
			t.Fatalf("terminalWidth(%q) = %d; uniseg says %d", s, terminalWidth(s), want)
		}
	}
}
