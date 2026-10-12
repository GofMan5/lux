package render

import "testing"

func TestCanvasTextPreservesGraphemesAndRepairsWideCells(t *testing.T) {
	buffer := NewBuffer(8, 2)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(12, 34, 56), Attrs: AttrBold | AttrUnderline}

	if width := canvas.Text(0, 0, "A界e\u0301🙂", style); width != 6 {
		t.Fatalf("Text width = %d, want 6", width)
	}
	want := []Cell{
		{Grapheme: "A", Width: 1, Style: style},
		{Grapheme: "界", Width: 2, Style: style},
		{Width: 0, Style: style},
		{Grapheme: "é", Width: 1, Style: style},
		{Grapheme: "🙂", Width: 2, Style: style},
		{Width: 0, Style: style},
	}
	for x, expected := range want {
		if got := buffer.Cell(x, 0); got != expected {
			t.Errorf("Cell(%d, 0) = %#v, want %#v", x, got, expected)
		}
	}

	canvas.Put(2, 0, "x", Style{})
	if got := buffer.Cell(1, 0); got != BlankCell {
		t.Errorf("overwritten wide lead = %#v, want blank", got)
	}
	if got := buffer.Cell(2, 0); got.Grapheme != "x" || got.Width != 1 {
		t.Errorf("wide continuation replacement = %#v", got)
	}

	canvas.Put(4, 0, "y", Style{})
	if got := buffer.Cell(5, 0); got != BlankCell {
		t.Errorf("wide trailing cell = %#v after lead overwrite, want blank", got)
	}
}

func TestCanvasClippingAndSubview(t *testing.T) {
	buffer := NewBuffer(7, 3)
	canvas := NewCanvas(buffer)
	canvas.Fill(canvas.Bounds(), ".", Style{})
	view := canvas.Subview(Rect{X: 2, Y: 1, W: 3, H: 2})
	view.Fill(view.Bounds(), "x", Style{})

	for y := range 3 {
		for x := range 7 {
			want := "."
			if x >= 2 && x < 5 && y >= 1 {
				want = "x"
			}
			if got := buffer.Cell(x, y).Grapheme; got != want {
				t.Fatalf("Cell(%d, %d) = %q, want %q", x, y, got, want)
			}
		}
	}

	canvas.Put(1, 0, "界", Style{})
	canvas.Clip(Rect{X: 2, Y: 0, W: 2, H: 1}).Put(2, 0, "z", Style{})
	if got := buffer.Cell(1, 0); got.Grapheme != "界" || got.Width != 2 {
		t.Fatalf("clip split a wide cell: %#v", got)
	}
	if got := buffer.Cell(2, 0); !got.IsContinuation() {
		t.Fatalf("wide continuation was replaced across clip: %#v", got)
	}
}

func TestCanvasGlobalRectHonorsSubviewAndClip(t *testing.T) {
	canvas := NewCanvas(NewBuffer(20, 10)).Subview(Rect{X: 4, Y: 2, W: 10, H: 6}).Clip(Rect{X: 2, Y: 1, W: 5, H: 3})
	got := canvas.GlobalRect(Rect{X: 0, Y: 0, W: 10, H: 6})
	want := Rect{X: 6, Y: 3, W: 5, H: 3}
	if got != want {
		t.Fatalf("global rect = %#v, want %#v", got, want)
	}
}

func TestCanvasGlobalPointPreservesClippedOrigin(t *testing.T) {
	canvas := NewCanvas(NewBuffer(20, 10)).Subview(Rect{X: 4, Y: 2, W: 10, H: 6}).Clip(Rect{X: 2, Y: 1, W: 5, H: 3})
	x, y := canvas.GlobalPoint(0, 0)
	if x != 4 || y != 2 {
		t.Fatalf("global point = %d,%d", x, y)
	}
}

func TestCanvasDrawableRect(t *testing.T) {
	if got := NewCanvas(NewBuffer(20, 10)).DrawableRect(); got != (Rect{W: 20, H: 10}) {
		t.Fatalf("base canvas drawable = %#v; want full bounds", got)
	}
	view := NewCanvas(NewBuffer(20, 10)).Subview(Rect{X: 4, Y: 2, W: 10, H: 6})
	if got := view.DrawableRect(); got != (Rect{W: 10, H: 6}) {
		t.Fatalf("subview drawable = %#v; want full subview bounds", got)
	}
	clipped := view.Clip(Rect{X: 2, Y: 1, W: 5, H: 3})
	if got := clipped.DrawableRect(); got != (Rect{X: 2, Y: 1, W: 5, H: 3}) {
		t.Fatalf("clipped drawable = %#v; want clip in local coordinates", got)
	}
	over := view.Clip(Rect{X: 8, Y: 5, W: 10, H: 10})
	if got := over.DrawableRect(); got != (Rect{X: 8, Y: 5, W: 2, H: 1}) {
		t.Fatalf("clip past bounds drawable = %#v; want clamped to bounds", got)
	}
	var nilCanvas *Canvas
	if got := nilCanvas.DrawableRect(); got != (Rect{}) {
		t.Fatalf("nil canvas drawable = %#v; want empty", got)
	}
}

func TestBufferResizeCloneDiffAndReset(t *testing.T) {
	style := Style{Background: IndexedColor(42), Attrs: AttrItalic}
	buffer := NewBuffer(4, 2)
	canvas := NewCanvas(buffer)
	canvas.Put(2, 0, "界", style)
	clone := buffer.Clone()

	if !clone.Equal(buffer) {
		t.Fatal("Clone differs from source")
	}
	clone.Resize(3, 2)
	if got := clone.Cell(2, 0); got != BlankCell {
		t.Fatalf("Resize retained clipped wide lead: %#v", got)
	}
	if got := buffer.Cell(2, 0); got.Grapheme != "界" {
		t.Fatal("Clone mutated source")
	}

	copyBuffer := NewBuffer(1, 1)
	copyBuffer.CopyFrom(buffer)
	NewCanvas(copyBuffer).Put(0, 1, "x", style)
	changes := copyBuffer.Diff(buffer)
	if len(changes) != 1 || changes[0].X != 0 || changes[0].Y != 1 || changes[0].Cell.Grapheme != "x" {
		t.Fatalf("Diff = %#v, want one changed cell", changes)
	}
	copyBuffer.Reset()
	for index := range copyBuffer.Len() {
		if got := copyBuffer.CellAt(index); got != BlankCell {
			t.Fatalf("CellAt(%d) after Reset = %#v", index, got)
		}
	}
}

func TestBorderAndResponsiveRect(t *testing.T) {
	buffer := NewBuffer(5, 3)
	canvas := NewCanvas(buffer)
	canvas.Border(canvas.Bounds(), RoundedBorder, Style{})
	if got := buffer.Cell(0, 0).Grapheme; got != "╭" {
		t.Fatalf("top-left = %q", got)
	}
	if got := buffer.Cell(4, 2).Grapheme; got != "╯" {
		t.Fatalf("bottom-right = %q", got)
	}

	left, rest := (Rect{X: 2, Y: 3, W: 5, H: 4}).SplitLeft(99)
	if left != (Rect{X: 2, Y: 3, W: 5, H: 4}) || !rest.Empty() {
		t.Fatalf("clamped SplitLeft = %#v, %#v", left, rest)
	}
	top, bottom := rest.SplitTop(2)
	if !top.Empty() || !bottom.Empty() {
		t.Fatalf("empty split = %#v, %#v", top, bottom)
	}
}

func TestDoubleBufferLifecycle(t *testing.T) {
	double := NewDoubleBuffer(3, 1)
	NewCanvas(double.Back()).Text(0, 0, "ok", Style{})
	changes := double.Changes()
	if len(changes) != 2 {
		t.Fatalf("Changes length = %d, want 2", len(changes))
	}
	double.Swap()
	if changes := double.Changes(); len(changes) != 2 {
		t.Fatalf("Changes after swap = %d, want two cells reverting older back frame", len(changes))
	}
	double.Back().CopyFrom(double.Front())
	if changes := double.Changes(); len(changes) != 0 {
		t.Fatalf("Changes after CopyFrom = %d, want 0", len(changes))
	}
}

func TestWidthsAndTextControls(t *testing.T) {
	if got := GraphemeWidth("👨‍👩‍👧‍👦tail"); got != 2 {
		t.Fatalf("family emoji width = %d, want 2", got)
	}
	if got := TextWidth("ab\n界x"); got != 3 {
		t.Fatalf("multiline width = %d, want 3", got)
	}
	buffer := NewBuffer(4, 1)
	width := NewCanvas(buffer).Text(0, 0, "a\x1bb", Style{})
	if width != 2 || buffer.Cell(0, 0).Grapheme != "a" || buffer.Cell(1, 0).Grapheme != "b" {
		t.Fatalf("control handling width=%d cells=%#v", width, buffer.cells)
	}
	if got := TextWidth("abc\rx"); got != 3 {
		t.Fatalf("carriage-return width = %d, want 3", got)
	}
}

func BenchmarkCanvasFrame(b *testing.B) {
	buffer := NewBuffer(120, 40)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(220, 225, 235), Background: RGBColor(20, 24, 32)}
	line := "Lux TUI · smooth 界面 · 60 FPS · 👨‍👩‍👧‍👦"
	b.ReportAllocs()
	for range b.N {
		canvas.Clear(style)
		canvas.Border(canvas.Bounds(), RoundedBorder, style)
		for y := 1; y < 39; y++ {
			canvas.Text(2, y, line, style)
		}
	}
}

func BenchmarkBufferDiffNoChanges(b *testing.B) {
	front := NewBuffer(160, 50)
	back := front.Clone()
	changes := make([]Change, 0, 32)
	b.ReportAllocs()
	for range b.N {
		changes = back.AppendDiff(changes[:0], front)
	}
}

// BenchmarkEqualRowUnchanged prices the sweep that proves an idle frame idle:
// one EqualRow call per row, no other work. internal/terminal's idle scan runs
// exactly this loop against the same 120x40 shape, so the difference between
// BenchmarkScanUnchanged's median and this one is the terminal-side
// bookkeeping around the scan (appendFrame prologue, dirty-row bitset, early
// exit), not the render comparison itself.
func BenchmarkEqualRowUnchanged(b *testing.B) {
	front := NewBuffer(120, 40)
	back := front.Clone()
	same := true
	b.ReportAllocs()
	for range b.N {
		for y := 0; y < 40; y++ {
			same = front.EqualRow(back, y) && same
		}
	}
	if !same {
		b.Fatal("clones diverged during benchmark")
	}
}

func TestScrollRowsMovesAndBlanksLikeATerminal(t *testing.T) {
	buffer := NewBuffer(3, 5)
	canvas := NewCanvas(buffer)
	for row := range 5 {
		canvas.Text(0, row, string(rune('a'+row)), Style{Attrs: AttrBold})
	}
	rowText := func(y int) string {
		text := ""
		for x := 0; x < 3; x++ {
			text += buffer.Cell(x, y).Grapheme
		}
		return text
	}

	// Scrolling up inside a region leaves the rows outside it untouched.
	if !buffer.ScrollRows(1, 3, 1) {
		t.Fatal("ScrollRows reported no movement")
	}
	for y, want := range []string{"a  ", "c  ", "d  ", "   ", "e  "} {
		if got := rowText(y); got != want {
			t.Fatalf("after scrolling up, row %d = %q, want %q", y, got, want)
		}
	}
	if got := buffer.Cell(0, 3); got != BlankCell {
		t.Fatalf("an exposed row kept its style: %#v", got)
	}

	if !buffer.ScrollRows(1, 3, -2) {
		t.Fatal("ScrollRows reported no movement")
	}
	for y, want := range []string{"a  ", "   ", "   ", "c  ", "e  "} {
		if got := rowText(y); got != want {
			t.Fatalf("after scrolling down, row %d = %q, want %q", y, got, want)
		}
	}

	// A shift at least as large as the region blanks all of it, and impossible
	// requests move nothing.
	if !buffer.ScrollRows(0, 4, 9) {
		t.Fatal("an oversized shift reported no movement")
	}
	for y := range 5 {
		if got := rowText(y); got != "   " {
			t.Fatalf("row %d survived an oversized shift: %q", y, got)
		}
	}
	if buffer.ScrollRows(1, 3, 0) || buffer.ScrollRows(4, 1, 1) || (*Buffer)(nil).ScrollRows(0, 1, 1) {
		t.Fatal("ScrollRows moved something it should not have")
	}
}
