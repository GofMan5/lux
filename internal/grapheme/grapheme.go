// Package grapheme iterates extended grapheme clusters and nothing else.
//
// uniseg's Graphemes iterator steps the word, sentence and line-break state
// machines on every cluster as well, which none of LUX's drawing or editing
// paths read; those machines cost more than the grapheme step itself and the
// iterator allocates. Iterator keeps uniseg's segmentation exactly and adds
// an ASCII fast path: a printable or control ASCII byte followed by another
// ASCII byte is always a cluster on its own, except CR LF.
package grapheme

import (
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Iterator walks text one grapheme cluster at a time, with the Next, Str
// and Positions contract of uniseg.Graphemes.
type Iterator struct {
	rest, cluster string
	start, end    int
	state         int
}

// New returns an iterator positioned before the first cluster of text.
func New(text string) Iterator {
	return Iterator{rest: text, state: -1}
}

// Next advances to the next cluster and reports whether there was one.
func (it *Iterator) Next() bool {
	if it.rest == "" {
		it.cluster, it.start = "", it.end
		return false
	}
	it.start = it.end
	if len(it.rest) == 1 || it.rest[0] < utf8.RuneSelf && it.rest[1] < utf8.RuneSelf && it.rest[0] != '\r' {
		it.cluster, it.rest, it.state = it.rest[:1], it.rest[1:], -1
	} else {
		it.cluster, it.rest, _, it.state = uniseg.FirstGraphemeClusterInString(it.rest, it.state)
	}
	it.end += len(it.cluster)
	return true
}

// Str returns the current cluster.
func (it *Iterator) Str() string { return it.cluster }

// Positions returns the byte range of the current cluster in the text.
func (it *Iterator) Positions() (int, int) { return it.start, it.end }
