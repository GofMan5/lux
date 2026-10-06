package render

import "testing"

func TestEqualRowMatchesIdenticalRowsOnly(t *testing.T) {
	frame := NewBuffer(8, 3)
	peer := NewBuffer(8, 3)
	canvas, other := NewCanvas(frame), NewCanvas(peer)
	canvas.Text(0, 0, "alpha", Style{Attrs: AttrBold})
	other.Text(0, 0, "alpha", Style{Attrs: AttrBold})
	canvas.Text(0, 1, "beta", Style{})
	other.Text(0, 1, "betz", Style{})

	if !frame.EqualRow(peer, 0) {
		t.Fatal("identical rows must compare equal")
	}
	if frame.EqualRow(peer, 1) {
		t.Fatal("rows differing in one grapheme must compare unequal")
	}
	if !frame.EqualRow(peer, 2) {
		t.Fatal("untouched blank rows must compare equal")
	}

	other.Text(0, 0, "alpha", Style{Attrs: AttrBold, Foreground: IndexedColor(9)})
	if frame.EqualRow(peer, 0) {
		t.Fatal("rows differing only in style must compare unequal")
	}
}

func TestEqualRowRejectsUnusableComparisons(t *testing.T) {
	buffer := NewBuffer(8, 3)
	var missing *Buffer

	if buffer.EqualRow(missing, 0) {
		t.Fatal("a nil peer must never compare equal")
	}
	if missing.EqualRow(buffer, 0) {
		t.Fatal("a nil receiver must never compare equal")
	}

	narrow := NewBuffer(4, 3)
	if buffer.EqualRow(narrow, 0) || narrow.EqualRow(buffer, 0) {
		t.Fatal("rows of buffers with different widths must never compare equal")
	}

	tall := NewBuffer(8, 6)
	if buffer.EqualRow(tall, 3) || tall.EqualRow(buffer, 3) {
		t.Fatal("a row outside either buffer must never compare equal")
	}

	if buffer.EqualRow(buffer, -1) || buffer.EqualRow(buffer, 3) {
		t.Fatal("out-of-range rows must never compare equal")
	}
}
