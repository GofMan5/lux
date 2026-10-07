package render

import "testing"

func TestPutCellAttachesLinksAndStampsWideCells(t *testing.T) {
	buffer := NewBuffer(8, 2)
	canvas := NewCanvas(buffer)
	link := Link{URI: "https://example.com", ID: "doc"}
	style := Style{Foreground: RGBColor(9, 8, 7)}

	if width := canvas.PutCell(0, 0, Cell{Grapheme: "界", Style: style, Link: link}); width != 2 {
		t.Fatalf("PutCell width = %d, want 2", width)
	}
	lead := buffer.Cell(0, 0)
	trail := buffer.Cell(1, 0)
	if lead.Link != link || trail.Link != link {
		t.Fatalf("wide grapheme link: lead %#v, trail %#v, want %v", lead.Link, trail.Link, link)
	}
	if trail.Width != 0 || trail.Grapheme != "" {
		t.Fatalf("wide continuation = %#v, want empty", trail)
	}

	// Put never attaches a link, so the plain API cannot regress the gate.
	canvas.Put(3, 0, "x", style)
	if got := buffer.Cell(3, 0); got.Link != (Link{}) {
		t.Fatalf("Put attached a link: %#v", got.Link)
	}
}

func TestPutCellDistrustsCellWidthAndClipsLikePut(t *testing.T) {
	buffer := NewBuffer(4, 2)
	canvas := NewCanvas(buffer)
	link := Link{URI: "https://example.com"}

	canvas.PutCell(0, 0, Cell{Grapheme: "ab", Style: Style{}, Link: link})
	first, second := buffer.Cell(0, 0), buffer.Cell(1, 0)
	if first.Grapheme != "a" || first.Width != 1 || second != BlankCell {
		t.Fatalf("multi-grapheme cell: first %#v, second %#v", first, second)
	}

	// A cell whose Width field lies is still measured, never trusted.
	canvas.PutCell(2, 0, Cell{Grapheme: "界", Width: 1, Style: Style{}, Link: link})
	if got := buffer.Cell(3, 0); got.Link != link || got.Width != 0 {
		t.Fatalf("continuation = %#v, want stamped link and width 0", got)
	}

	// A wide grapheme that would cross the right edge is dropped whole.
	canvas.PutCell(3, 1, Cell{Grapheme: "界", Style: Style{}, Link: link})
	if got := buffer.Cell(3, 1); got != BlankCell {
		t.Fatalf("wide grapheme was split by the edge: %#v", got)
	}
	// PutCell off the canvas is a no-op that still reports the advance.
	if width := canvas.PutCell(9, 9, Cell{Grapheme: "e", Style: Style{}, Link: link}); width != 1 {
		t.Fatalf("off-canvas advance = %d, want 1", width)
	}
}

func TestPutCellCombiningGraphemeJoinUsesTheActiveLink(t *testing.T) {
	buffer := NewBuffer(4, 1)
	canvas := NewCanvas(buffer)
	link := Link{URI: "https://example.com"}
	canvas.PutCell(0, 0, Cell{Grapheme: "e", Style: Style{}, Link: link})
	canvas.PutCell(1, 0, Cell{Grapheme: "\u0301", Style: Style{}, Link: link})
	got := buffer.Cell(0, 0)
	if got.Grapheme != "e\u0301" || got.Link != link {
		t.Fatalf("combined cell = %#v", got)
	}
	if got := buffer.Cell(1, 0); got != BlankCell {
		t.Fatalf("combining mark consumed a cell: %#v", got)
	}
}

func TestLinkAndUnderlineZeroValues(t *testing.T) {
	if !(Link{}).Empty() {
		t.Fatal("zero Link must be Empty")
	}
	if (Link{ID: "x"}).Empty() {
		t.Fatal("an ID alone marks the link region open")
	}
	if (Link{URI: "https://example.com"}).Empty() {
		t.Fatal("a URI alone marks the link region open")
	}
	if style := (Style{}); style.UnderlineStyle != UnderlineSingle || style.UnderlineColor != (Color{}) {
		t.Fatalf("zero Style carries underline extras: %#v", style)
	}
}
