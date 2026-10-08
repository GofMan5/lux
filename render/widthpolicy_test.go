package render

import "testing"

// Golden widths per WidthPolicy knob. Expected values were checked against
// the raw Unicode 18.0.0 UCD files (EastAsianWidth.txt class A,
// emoji-data.txt Emoji_Presentation=Yes — see widthpolicy.go for the tables)
// and against the model terminal widths of the overlay.

func TestWidthPolicyZeroMatchesLegacy(t *testing.T) {
	// The zero policy must be byte-for-byte the historical behavior,
	// including the exotics the overlay tests cover.
	clusters := []string{
		"a", "e\u0301", "±", "α", "①", "界", "\u2630", "\U00018E00",
		"\U0001F1FA\U0001F1F8", "\U0001F468\u200D\U0001F469\u200D\U0001F466",
		"\u2E3A", "\u2E3B", "\u2139\uFE0E", "\u2139\uFE0F", "\uFE0F",
		"\U0001D127", "\U0001D166", "\U000113B8",
	}
	for _, cluster := range clusters {
		if got, legacy := GraphemeWidthWith(cluster, WidthPolicy{}), GraphemeWidth(cluster); got != legacy {
			t.Errorf("GraphemeWidthWith(%q, zero) = %d; legacy = %d", cluster, got, legacy)
		}
	}
	for _, line := range []string{"a±α①界\u2630\nsecond\trow", "\r\nmixed\r\u2E3A"} {
		if got, legacy := TextWidthWith(line, WidthPolicy{}), TextWidth(line); got != legacy {
			t.Errorf("TextWidthWith(%q, zero) = %d; legacy = %d", line, got, legacy)
		}
	}
	// A non-activating AmbiguousWidth (anything but 2) is exactly inactive.
	if got, legacy := terminalWidthOf("±", WidthPolicy{AmbiguousWidth: 3}), terminalWidth("±"); got != legacy {
		t.Errorf("terminalWidthOf(±, AmbiguousWidth=3) = %d; legacy = %d", got, legacy)
	}
}

func TestAmbiguousWidthTwo(t *testing.T) {
	policy := WidthPolicy{AmbiguousWidth: 2}
	tests := []struct {
		name     string
		grapheme string
		want     int
	}{
		{"plus-minus promoted", "±", 2},
		{"greek alpha promoted", "α", 2},
		{"box drawing horizontal promoted", "─", 2},
		{"circled digit one promoted", "①", 2},
		{"combining marks do not block the bump", "±\u0301", 2},
		{"combining diacritic on plain base stays narrow", "e\u0301", 1},
		{"plain latin stays narrow", "a", 1},
		{"copyright is narrow, not ambiguous", "©", 1},
		{"CJK ideograph already wide", "界", 2},
	}
	for _, tc := range tests {
		if got := GraphemeWidthWith(tc.grapheme, policy); got != tc.want {
			t.Errorf("%s: GraphemeWidthWith(%q) = %d; want %d", tc.name, tc.grapheme, got, tc.want)
		}
	}
	// Invalid values behave as 1: only exactly 2 activates the bump.
	for _, invalid := range []int{0, 3, -5} {
		if got := GraphemeWidthWith("±", WidthPolicy{AmbiguousWidth: invalid}); got != 1 {
			t.Errorf("AmbiguousWidth=%d: ± = %d; want 1 (behaves as 1)", invalid, got)
		}
	}
}

func TestVS16Blind(t *testing.T) {
	policy := WidthPolicy{VS16Blind: true}
	tests := []struct {
		name     string
		grapheme string
		want     int
	}{
		{"heart with VS16 collapses to one cell", "❤️", 1},
		{"plain heart already narrow", "❤", 1},
		{"warning sign with VS16", "⚠️", 1},
		{"information with VS16", "ℹ️", 1},
		{"VS15 text presentation never stripped", "ℹ︎", 1},
		{"VS15 heart never stripped", "❤︎", 1},
		{"two VS16s strip recursively", "❤\uFE0F\uFE0F", 1},
		{"emoji without VS16 keeps two cells", "😀", 2},
		{"flag pair has no trailing VS16", "\U0001F1FA\U0001F1F8", 2},
		{"waving flag with VS16 strips to text flag", "\U0001F3F3\uFE0F", 1},
		{"lone VS16 stays zero", "\uFE0F", 0},
	}
	for _, tc := range tests {
		if got := GraphemeWidthWith(tc.grapheme, policy); got != tc.want {
			t.Errorf("%s: GraphemeWidthWith(%q) = %d; want %d", tc.name, tc.grapheme, got, tc.want)
		}
	}
}

func TestNarrowEmoji(t *testing.T) {
	policy := WidthPolicy{NarrowEmoji: true}
	tests := []struct {
		name     string
		grapheme string
		want     int
	}{
		{"grinning face demoted", "😀", 2 - 1},
		{"soccer ball demoted", "⚽", 1},
		{"watch demoted (231A Emoji_Presentation)", "⌚", 1},
		{"high voltage demoted", "⚡", 1},
		{"drop of blood demoted (18.0 emoji)", "\U0001FA78", 1},
		{"flag pair demoted", "\U0001F1FA\U0001F1F8", 1},
		{"ZWJ family demoted as one cluster", "\U0001F468\u200D\U0001F469\u200D\U0001F466", 1},
		{"skin tone modifier rides along", "\U0001F600\U0001F3FD", 1},
		{"VS16 counts as emoji presentation", "❤️", 1},
		{"text-presentation heart without VS16 stays narrow", "❤", 1},
		{"CJK ideograph never demoted", "界", 2},
		{"trigram (18.0 wide, not emoji) never demoted", "\u2630", 2},
		{"jurchen ideograph never demoted", "\U00018E00", 2},
	}
	for _, tc := range tests {
		if got := GraphemeWidthWith(tc.grapheme, policy); got != tc.want {
			t.Errorf("%s: GraphemeWidthWith(%q) = %d; want %d", tc.name, tc.grapheme, got, tc.want)
		}
	}
}

func TestWidthPolicyKnobsCombine(t *testing.T) {
	combined := WidthPolicy{AmbiguousWidth: 2, NarrowEmoji: true}
	tests := []struct {
		grapheme string
		want     int
	}{
		{"±", 2}, {"界", 2}, {"a", 1}, {"😀", 1}, {"❤️", 1}, {"⚽", 1},
	}
	for _, tc := range tests {
		if got := GraphemeWidthWith(tc.grapheme, combined); got != tc.want {
			t.Errorf("combined: GraphemeWidthWith(%q) = %d; want %d", tc.grapheme, got, tc.want)
		}
	}
	// VS16-blind runs before the adjustments: ❤️ strips to ❤ (width 1),
	// then ambiguous-2 finds a non-ambiguous base and leaves it narrow.
	blin := WidthPolicy{AmbiguousWidth: 2, VS16Blind: true}
	if got := GraphemeWidthWith("❤️", blin); got != 1 {
		t.Errorf("ambiguous+blind: ❤️ = %d; want 1", got)
	}
	if got := GraphemeWidthWith("±", blin); got != 2 {
		t.Errorf("ambiguous+blind: ± = %d; want 2", got)
	}
}

func TestWidthPolicyTableEdges(t *testing.T) {
	ambiguous := []struct {
		r   rune
		hit bool
	}{
		{0x00A1, true}, {0x00A9, false}, {0x0300, true}, {0x036F, true}, {0x0370, false},
		{0x2460, true}, {0x24E9, true}, {0x24EA, false}, {0x24EB, true}, {0x254B, true},
		{0xE000, true}, {0xF8FF, true}, {0xF900, false}, {0x1F1AC, true}, {0x1F1AD, false},
		{0xF0000, true}, {0xFFFFD, true}, {0x100000, true}, {0x10FFFD, true}, {0x10FFFE, false},
	}
	for _, tc := range ambiguous {
		if got := ambiguousRune(tc.r); got != tc.hit {
			t.Errorf("ambiguousRune(%U) = %v; want %v", tc.r, got, tc.hit)
		}
	}
	emoji := []struct {
		r   rune
		hit bool
	}{
		{0x231A, true}, {0x2318, false}, {0x2648, true}, {0x2653, true},
		{0x1F1E6, true}, {0x1F1FF, true}, {0x1F200, false},
		{0x2764, false}, {0x1FA70, true}, {0x1FAFA, true}, {0x1FB00, false},
	}
	for _, tc := range emoji {
		if got := emojiPresentationRune(tc.r); got != tc.hit {
			t.Errorf("emojiPresentationRune(%U) = %v; want %v", tc.r, got, tc.hit)
		}
	}
}

func TestTextWidthWithPolicy(t *testing.T) {
	policy := WidthPolicy{AmbiguousWidth: 2}
	tests := []struct {
		text string
		want int
	}{
		{"±±", 4},           // 2+2
		{"±\nαα", 4},        // widest line is "αα"
		{"±\r\nα", 2},       // CR LF splits lines
		{"a\tb", 5},         // tab stop logic untouched
		{"±\tα", 2 + 2 + 2}, // tab to column 4, then α
		{"界±", 4},           // 2+2
		{"e\u0301±", 3},     // 1+2
	}
	for _, tc := range tests {
		if got := TextWidthWith(tc.text, policy); got != tc.want {
			t.Errorf("TextWidthWith(%q, ambiguous-2) = %d; want %d", tc.text, got, tc.want)
		}
	}
}

func TestCanvasPolicyDrawingAndMeasuring(t *testing.T) {
	buffer := NewBuffer(8, 3)
	canvas := NewCanvasWithPolicy(buffer, WidthPolicy{AmbiguousWidth: 2})
	style := Style{Foreground: RGBColor(4, 5, 6)}

	// Drawing advances by the policy width: ± takes two cells.
	if width := canvas.Text(0, 0, "±±", style); width != 4 {
		t.Fatalf("Text width = %d; want 4", width)
	}
	want := []Cell{
		{Grapheme: "±", Width: 2, Style: style},
		{Width: 0, Style: style},
		{Grapheme: "±", Width: 2, Style: style},
		{Width: 0, Style: style},
	}
	for x, expected := range want {
		if got := buffer.Cell(x, 0); got != expected {
			t.Errorf("Cell(%d, 0) = %#v; want %#v", x, got, expected)
		}
	}
	if advance := canvas.Put(0, 1, "±", style); advance != 2 {
		t.Errorf("Put advance = %d; want 2", advance)
	}
	// Fill steps by the policy width: the odd trailing cell gets a blank.
	canvas.Fill(Rect{X: 0, Y: 2, W: 3, H: 1}, "±", style)
	if got := buffer.Cell(0, 2); got.Grapheme != "±" || got.Width != 2 {
		t.Errorf("Fill cell 0 = %#v; want wide ±", got)
	}
	if got := buffer.Cell(2, 2); got.Grapheme != " " || got.Width != 1 {
		t.Errorf("Fill trailing cell = %#v; want a blank", got)
	}
}

func TestCanvasPolicyMeasureAccessors(t *testing.T) {
	buffer := NewBuffer(8, 2)
	canvas := NewCanvasWithPolicy(buffer, WidthPolicy{AmbiguousWidth: 2})
	if got := canvas.WidthPolicy(); got != (WidthPolicy{AmbiguousWidth: 2}) {
		t.Errorf("WidthPolicy() = %#v; want ambiguous-2", got)
	}
	if got := canvas.GraphemeWidth("±"); got != 2 {
		t.Errorf("canvas.GraphemeWidth(±) = %d; want 2", got)
	}
	if got := canvas.TextWidth("±±"); got != 4 {
		t.Errorf("canvas.TextWidth(±±) = %d; want 4", got)
	}
	// A default canvas keeps legacy measurements.
	legacy := NewCanvas(buffer)
	if got := legacy.GraphemeWidth("±"); got != 1 {
		t.Errorf("legacy canvas.GraphemeWidth(±) = %d; want 1", got)
	}
	// Nil canvases stay usable and report the default policy.
	var nilCanvas *Canvas
	if got := nilCanvas.WidthPolicy(); got != (WidthPolicy{}) {
		t.Errorf("nil WidthPolicy() = %#v; want zero", got)
	}
	if got := nilCanvas.GraphemeWidth("±"); got != 1 {
		t.Errorf("nil GraphemeWidth(±) = %d; want 1", got)
	}
	if got := nilCanvas.TextWidth("±±"); got != 2 {
		t.Errorf("nil TextWidth(±±) = %d; want 2", got)
	}
}

func TestCanvasPolicyTravelsThroughSubviewAndClip(t *testing.T) {
	buffer := NewBuffer(10, 3)
	canvas := NewCanvasWithPolicy(buffer, WidthPolicy{AmbiguousWidth: 2})
	style := Style{Foreground: RGBColor(7, 8, 9)}

	sub := canvas.Subview(Rect{X: 2, Y: 0, W: 4, H: 1})
	if got := sub.WidthPolicy().AmbiguousWidth; got != 2 {
		t.Fatalf("Subview lost the policy: %#v", sub.WidthPolicy())
	}
	if got := sub.TextWidth("±±"); got != 4 {
		t.Errorf("Subview TextWidth(±±) = %d; want 4", got)
	}
	if width := sub.Text(0, 0, "±", style); width != 2 {
		t.Errorf("Subview Text width = %d; want 2", width)
	}
	if got := buffer.Cell(2, 0); got.Grapheme != "±" || got.Width != 2 {
		t.Errorf("Subview draw target = %#v; want wide ± at absolute x=2", got)
	}

	clipped := canvas.Clip(Rect{X: 0, Y: 1, W: 4, H: 1})
	if got := clipped.WidthPolicy().AmbiguousWidth; got != 2 {
		t.Fatalf("Clip lost the policy: %#v", clipped.WidthPolicy())
	}
	if got := clipped.TextWidth("±±"); got != 4 {
		t.Errorf("Clip TextWidth(±±) = %d; want 4", got)
	}
}

// BenchmarkTextWidthAmbiguous2 prices the policy-adjusted path: every
// ambiguous cluster takes one promotion lookup after the overlay width.
// Compare against BenchmarkTextWidthLegacy for the inactive-policy baseline.
func BenchmarkTextWidthAmbiguous2(b *testing.B) {
	line := "±α─① ±α─① box drawing under a CJK-locale terminal ─ ─ ①"
	b.ReportAllocs()
	for range b.N {
		TextWidthWith(line, WidthPolicy{AmbiguousWidth: 2})
	}
}

func BenchmarkCanvasTextAmbiguous2(b *testing.B) {
	buffer := NewBuffer(120, 40)
	canvas := NewCanvasWithPolicy(buffer, WidthPolicy{AmbiguousWidth: 2})
	style := Style{Foreground: RGBColor(220, 225, 235)}
	line := "±± ±± ±± ──── ①②③④ ambiguous widths in a CJK locale "
	b.ReportAllocs()
	for range b.N {
		canvas.Text(2, 20, line, style)
	}
}
