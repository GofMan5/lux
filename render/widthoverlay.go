package render

import (
	"strings"

	"github.com/rivo/uniseg"
)

// WHY: uniseg v0.4.7 (our segmentation engine) ships Unicode 15.0-era
// EastAsianWidth / emoji / GraphemeBreakProperty data, so freshly assigned
// codepoints fall back to width 1 and post-15.0 East Asian Width promotions
// (e.g. U+2630..U+2637 trigrams and U+4DC0..U+4DFF hexagrams became W in
// Unicode 16.0) render in one cell instead of two. Rather than forking the
// segmentation engine, we keep uniseg as the single source of grapheme
// segmentation and layer a minimal, data-driven width overlay that upgrades
// the width result to Unicode 18.0.0 semantics (machine date: 2026-10-07,
// 18.0.0 is the current release).
//
// The overlay was derived mechanically by comparing uniseg.StringWidth of
// every codepoint 0..0x10FFFF against a rune-width model built from the raw
// 18.0.0 UCD files (EastAsianWidth.txt, emoji/emoji-data.txt and
// auxiliary/GraphemeBreakProperty.txt, see THIRD_PARTY_NOTICES.md). Every
// codepoint where the model and the library disagree is recorded below:
// 33 wide ranges (1357 codepoints, model width 2, library width 1) and 53
// zero ranges (145 codepoints, model width 0, library width 1 â€” new Extend
// and Control assignments such as the Tulu-Tigalari marks and the musical
// reclassifications U+1D166/U+1D16D from SpacingMark to Extend in 16.0).
// No codepoint needs to get narrower and Extended_Pictographic did not grow
// after 15.0 (it is stable), so no VS16 presentation table is required.
//
// Known, documented drift (COMPATIBILITY.md): segmentation stays uniseg, so
// clusters composed only of post-15.0 codepoints can still be split wrong
// (GB11 ZWJ emoji made of new codepoints, GB9c Indic conjuncts). The overlay
// is width-only: cluster boundaries remain whatever uniseg produces.

type runeRange struct{ lo, hi rune }

// overlayWide are the codepoints whose terminal width changes from 1 to 2
// between the uniseg 15.0-era tables and Unicode 18.0.0.
var overlayWide = []runeRange{
	{0x2630, 0x2637},
	{0x268A, 0x268F},
	{0x2FFC, 0x2FFF},
	{0x31E4, 0x31E5},
	{0x31EF, 0x31EF},
	{0x4DC0, 0x4DFF},
	{0x1171E, 0x1171E},
	{0x16FF2, 0x16FF6},
	{0x187F8, 0x187FF},
	{0x18CD6, 0x18CDA},
	{0x18CFF, 0x18CFF},
	{0x18D09, 0x18D20},
	{0x18D80, 0x18DF2},
	{0x18E00, 0x19191},
	{0x191A0, 0x191D2},
	{0x1B123, 0x1B128},
	{0x1B168, 0x1B168},
	{0x1D300, 0x1D356},
	{0x1D360, 0x1D376},
	{0x1F1AE, 0x1F1AE},
	{0x1F260, 0x1F265},
	{0x1F6D8, 0x1F6D9},
	{0x1F7DA, 0x1F7DA},
	{0x1FA89, 0x1FA8F},
	{0x1FABE, 0x1FABE},
	{0x1FAC6, 0x1FAC6},
	{0x1FAC8, 0x1FAC8},
	{0x1FACC, 0x1FACD},
	{0x1FADC, 0x1FADD},
	{0x1FADF, 0x1FADF},
	{0x1FAE9, 0x1FAEB},
	{0x1FAEF, 0x1FAEF},
	{0x1FAF9, 0x1FAFA},
}

// overlayZero are the codepoints whose terminal width changes from 1 to 0
// (newly assigned Extend/Control, or marks reclassified into Extend).
var overlayZero = []runeRange{
	{0x05C8, 0x05C9},
	{0x0897, 0x0897},
	{0x0B53, 0x0B54},
	{0x0CC0, 0x0CC0},
	{0x0CC7, 0x0CC8},
	{0x0CCA, 0x0CCB},
	{0x1715, 0x1715},
	{0x1734, 0x1734},
	{0x1ACF, 0x1AF0},
	{0x1B3B, 0x1B3B},
	{0x1B3D, 0x1B3D},
	{0x1B43, 0x1B44},
	{0x1BAA, 0x1BAA},
	{0x1BF2, 0x1BF3},
	{0xA953, 0xA953},
	{0xA9C0, 0xA9C0},
	{0x10D69, 0x10D6D},
	{0x10ECB, 0x10ECF},
	{0x10EF0, 0x10EFC},
	{0x111C0, 0x111C0},
	{0x11235, 0x11235},
	{0x1134D, 0x1134D},
	{0x113B8, 0x113B8},
	{0x113BB, 0x113C0},
	{0x113C2, 0x113C2},
	{0x113C5, 0x113C5},
	{0x113C7, 0x113C9},
	{0x113CE, 0x113D0},
	{0x113D2, 0x113D2},
	{0x113E1, 0x113E2},
	{0x116B6, 0x116B6},
	{0x1193D, 0x1193D},
	{0x11B60, 0x11B60},
	{0x11B62, 0x11B64},
	{0x11B66, 0x11B66},
	{0x11DF0, 0x11DF0},
	{0x11F41, 0x11F41},
	{0x11F5A, 0x11F5A},
	{0x1611E, 0x16129},
	{0x1612D, 0x1612F},
	{0x16FF0, 0x16FF1},
	{0x1D127, 0x1D128},
	{0x1D166, 0x1D166},
	{0x1D16D, 0x1D16D},
	{0x1D250, 0x1D252},
	{0x1D25B, 0x1D25C},
	{0x1D25F, 0x1D25F},
	{0x1D280, 0x1D281},
	{0x1E5EE, 0x1E5EF},
	{0x1E6E3, 0x1E6E3},
	{0x1E6E6, 0x1E6E6},
	{0x1E6EE, 0x1E6EF},
	{0x1E6F5, 0x1E6F5},
}

// overlayRune reports the overlay width of a single codepoint. The second
// return value is false for every codepoint where uniseg already matches the
// 18.0.0 model.
func overlayRune(r rune) (width int, ok bool) {
	if rangeContains(overlayWide, r) {
		return 2, true
	}
	if rangeContains(overlayZero, r) {
		return 0, true
	}
	return 0, false
}

func rangeContains(ranges []runeRange, r rune) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		switch {
		case r < ranges[mid].lo:
			hi = mid - 1
		case r > ranges[mid].hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// stand-in codepoints for cluster translation. U+0300 is Extend with width 0
// and U+4E00 is East Asian Wide with width 2 in the uniseg tables, so they
// reproduce exactly the width semantics the overlay wants for the new runes.
const (
	overlayStandInZero = 0x0300 // COMBINING GRAVE ACCENT, Extend, width 0
	overlayStandInWide = 0x4E00 // CJK UNIFIED IDEOGRAPH-4E00, EAW W, width 2
)

// overlayClusterWidth returns the 18.0.0-aware width of one grapheme cluster.
// ok is false when the cluster contains no overlay codepoint at all, in which
// case the caller must use uniseg.StringWidth unchanged â€” that keeps the
// fast path allocation-free and preserves exotic uniseg results (U+2E3A is 3
// cells, U+2E3B is 4, VS15/VS16 presentation switches, RI pairs, Hangul LVT
// composition) byte-for-byte.
//
// When a cluster does contain new codepoints we cannot ask uniseg for their
// contribution directly, so we translate each of them into a legacy stand-in
// with the same grapheme-break and width class (Extend -> U+0300, Wide ->
// U+4E00) and measure the translated cluster instead. Because the stand-ins
// share the old classes, uniseg's cluster composition logic (which runes are
// summed, how VS16 forces emoji presentation, RI pairing) applies to the new
// codepoints exactly as the 18.0.0 model prescribes.
func overlayClusterWidth(cluster string) (width int, ok bool) {
	// Quick scan first: the common case (pure legacy text) must not build a
	// translated copy of every cluster on the hot render path.
	translated := false
	for _, r := range cluster {
		if _, hit := overlayRune(r); hit {
			translated = true
			break
		}
	}
	if !translated {
		return 0, false
	}
	var b strings.Builder
	b.Grow(len(cluster))
	for _, r := range cluster {
		if w, hit := overlayRune(r); hit {
			// The stand-ins carry the overlay width while keeping uniseg's
			// composition rules engaged for the rest of the cluster.
			if w == 0 {
				r = overlayStandInZero
			} else {
				r = overlayStandInWide
			}
		}
		b.WriteRune(r)
	}
	// Invalid UTF-8 bytes decode to U+FFFD during translation; both the
	// original bytes and the replacement round to width 1, so the result
	// does not drift for undecodable input.
	return uniseg.StringWidth(b.String()), true
}
