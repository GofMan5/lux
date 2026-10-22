package grapheme

import (
	"testing"

	"github.com/rivo/uniseg"
)

var corpus = []string{
	"", "a", "ab", "\r", "\r\n", "a\r\nb", "\n\n", "\t x", "é", "aéb",
	"汉字你好世界", "👨‍👩‍👧‍👦 family", "🇫🇷🇯🇵🇺", "a🇫🇷", "🏳️‍🌈", "🧑‍🚀x", "αβγ→λ ﬁ ①②③",
	"\x80", "a\xffb", "\xe2\x82", "x\u200dy", "1️⃣", "#⃣", "क्ष",
	"각", "ä́", "\x00\x1b[0m", "q̀w", "\ŕ",
}

// sameAsUniseg fails unless Iterator yields uniseg's clusters and offsets.
func sameAsUniseg(t *testing.T, text string) {
	t.Helper()
	want := uniseg.NewGraphemes(text)
	got := New(text)
	for want.Next() {
		if !got.Next() {
			t.Fatalf("%q: iterator ended before uniseg cluster %q", text, want.Str())
		}
		ws, we := want.Positions()
		gs, ge := got.Positions()
		if got.Str() != want.Str() || gs != ws || ge != we {
			t.Fatalf("%q: got %q [%d,%d), want %q [%d,%d)", text, got.Str(), gs, ge, want.Str(), ws, we)
		}
	}
	if got.Next() {
		t.Fatalf("%q: iterator yielded extra cluster %q", text, got.Str())
	}
}

func TestIteratorMatchesUniseg(t *testing.T) {
	for _, text := range corpus {
		sameAsUniseg(t, text)
		for _, other := range corpus {
			sameAsUniseg(t, text+other)
		}
	}
}

func FuzzIteratorMatchesUniseg(f *testing.F) {
	for _, text := range corpus {
		f.Add(text)
	}
	f.Fuzz(sameAsUniseg)
}

func BenchmarkIterator(b *testing.B) {
	text := "000123 | the quick brown fox jumps over 0123456789 the quick brown fox jumps over 0123456789 the quick"
	b.Run("grapheme", func(b *testing.B) {
		for range b.N {
			it := New(text)
			for it.Next() {
			}
		}
	})
	b.Run("uniseg", func(b *testing.B) {
		for range b.N {
			it := uniseg.NewGraphemes(text)
			for it.Next() {
			}
		}
	})
}
