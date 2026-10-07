package terminal

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

// present encodes one frame and replays it through the reference grid, so every
// byte-shape assertion below is also proved to reconstruct the frame exactly.
func encodeAndVerify(t *testing.T, grid *referenceGrid, frame, previous *render.Buffer, depth capability.ColorDepth) string {
	t.Helper()
	data := grid.present(t, frame, previous, depth, false)
	grid.requireMatches(t, frame, depth)
	return string(data)
}

func TestEncoderMovesCursorRelativelyWithinARow(t *testing.T) {
	frame := render.NewBuffer(40, 10)
	grid := newReferenceGrid(40, 10)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas := render.NewCanvas(frame)
	canvas.Put(10, 5, "A", render.Style{})
	canvas.Put(14, 5, "B", render.Style{})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, "\x1b[6;11H") {
		t.Fatalf("the first run must be addressed absolutely: %q", data)
	}
	if !strings.Contains(data, "\x1b[3C") {
		t.Fatalf("a short forward move must not restate the row: %q", data)
	}
}

func TestEncoderKeepsCursorWhenACaretFollowsItsRun(t *testing.T) {
	frame := render.NewBuffer(40, 10)
	grid := newReferenceGrid(40, 10)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	render.NewCanvas(frame).Text(4, 7, "typed", render.Style{})
	frame.SetCaret(9, 7)
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	// The run leaves the cursor exactly on the caret, so no move is needed at all.
	if want := "\x1b[8;5H\x1b[0mtyped\x1b[?25h"; data != want {
		t.Fatalf("caret after its own run emitted %q, want %q", data, want)
	}
}

func TestEncoderErasesBlankTailInsteadOfTransmittingIt(t *testing.T) {
	frame := render.NewBuffer(40, 4)
	canvas := render.NewCanvas(frame)
	canvas.Text(0, 1, strings.Repeat("X", 30), render.Style{Foreground: render.RGBColor(200, 40, 90)})
	grid := newReferenceGrid(40, 4)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Clear(render.Style{})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, eraseLine) {
		t.Fatalf("a cleared row must be erased, not blanked cell by cell: %q", data)
	}
	if strings.Contains(data, strings.Repeat(" ", 4)) {
		t.Fatalf("blanks were transmitted despite the erase: %q", data)
	}
}

func TestEncoderKeepsStyledBlanksOutOfTheErase(t *testing.T) {
	// Erase-to-end-of-line fills with the active background on terminals with
	// background-color erase and with the default background on those without,
	// so a tail that carries its own style must be transmitted instead.
	frame := render.NewBuffer(40, 2)
	canvas := render.NewCanvas(frame)
	canvas.Clear(render.Style{Background: render.IndexedColor(24)})
	canvas.Text(0, 0, strings.Repeat("X", 30), render.Style{})
	grid := newReferenceGrid(40, 2)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Clear(render.Style{Background: render.IndexedColor(24)})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if strings.Contains(data, eraseLine) {
		t.Fatalf("a styled tail must not be erased: %q", data)
	}
}

func TestEncoderEmitsStyleDeltas(t *testing.T) {
	frame := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(frame)
	background := render.RGBColor(0, 0, 255)
	canvas.Put(0, 0, "A", render.Style{Foreground: render.RGBColor(255, 0, 0), Background: background, Attrs: render.AttrBold})
	canvas.Put(1, 0, "B", render.Style{Foreground: render.RGBColor(0, 255, 0), Background: background, Attrs: render.AttrBold})
	grid := newReferenceGrid(8, 1)
	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
	if !strings.Contains(data, "A\x1b[38;2;0;255;0mB") {
		t.Fatalf("a foreground change must not restate the whole style: %q", data)
	}
}

func TestEncoderClearsIntensityWithoutLosingItsPartner(t *testing.T) {
	// SGR 22 turns bold and dim off together, so dropping bold must restate dim.
	frame := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(frame)
	canvas.Put(0, 0, "A", render.Style{Attrs: render.AttrBold | render.AttrDim | render.AttrUnderline})
	canvas.Put(1, 0, "B", render.Style{Attrs: render.AttrDim | render.AttrUnderline})
	grid := newReferenceGrid(8, 1)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
}

func TestEncoderSkipsStyleChangesInvisibleAtTheActiveDepth(t *testing.T) {
	frame := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(frame)
	canvas.Put(0, 0, "a", render.Style{Foreground: render.RGBColor(255, 0, 0)})
	canvas.Put(1, 0, "b", render.Style{Foreground: render.ANSIColor(9)})
	grid := newReferenceGrid(8, 1)
	data := encodeAndVerify(t, grid, frame, nil, capability.ANSI16)
	if !strings.Contains(data, "ab") {
		t.Fatalf("colors that downgrade to the same value must not emit an SGR: %q", data)
	}
}

// TestEncoderFrameBudget pins the encoded size of two representative frames.
// It is a regression guard, not a target: a change that grows either number
// should be a deliberate one.
func TestEncoderFrameBudget(t *testing.T) {
	frame := render.NewBuffer(80, 24)
	paintSample(frame, 0)
	grid := newReferenceGrid(80, 24)
	repaint := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	paintSample(frame, 1)
	frame.SetCaret(12, 3)
	diff := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)

	for _, budget := range []struct {
		name  string
		size  int
		limit int
	}{
		{"repaint", len(repaint), 700},
		{"diff", len(diff), 700},
	} {
		if budget.size > budget.limit {
			t.Fatalf("%s frame is %d bytes, budget is %d", budget.name, budget.size, budget.limit)
		}
	}
	t.Logf("80x24 truecolor: repaint %d bytes, diff %d bytes", len(repaint), len(diff))
}

func BenchmarkEncodeFrame(b *testing.B) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	previous := frame.Clone()
	paintSample(frame, 1)
	scratch := make([]byte, 0, 8192)
	size := 0
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrame(scratch[:0], frame, previous, capability.TrueColor, true, false, false)
		size = len(scratch)
	}
	b.ReportMetric(float64(size), "bytes/frame")
}

func BenchmarkScanUnchanged(b *testing.B) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	previous := frame.Clone()
	b.ResetTimer()
	for range b.N {
		appendFrame(nil, frame, previous, capability.TrueColor, true, false, false)
	}
}

// BenchmarkScrollFrame prices a one-row scroll of a 120x40 list. The clone is
// deliberately inside the loop: appendFrame mutates its terminal buffer when
// it scrolls, so every iteration needs a pristine predecessor. The clone cost
// is part of the scroll path's honest price, and an A/B against the legacy
// encoder must clone symmetrically.
func BenchmarkScrollFrame(b *testing.B) {
	base := render.NewBuffer(120, 40)
	scrollingList(base, 0)
	frame := base.Clone()
	scrollingList(frame, 1)
	scratch := make([]byte, 0, 8192)
	size := 0
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrame(scratch[:0], frame, base.Clone(), capability.TrueColor, true, false, false)
		size = len(scratch)
	}
	b.ReportMetric(float64(size), "bytes/frame")
}

// BenchmarkIdlePresent prices the complete Present call on a frame that did
// not change: write-slot semaphore, feature snapshot, the idle scan, and the
// zero-byte early exit. No terminal bytes are written, so the writer is only
// there to satisfy the nil guard, and the median is the per-frame floor every
// idle application pays at 60 FPS.
func BenchmarkIdlePresent(b *testing.B) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatalf("open dev null: %v", err)
	}
	defer func() { _ = devNull.Close() }()
	backend := &Backend{
		writer:   devNull,
		opened:   true,
		features: capability.Set{Color: capability.TrueColor, ScrollRegions: true},
		previous: frame.Clone(),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := backend.Present(ctx, frame); err != nil {
			b.Fatalf("present idle frame: %v", err)
		}
	}
}

func TestEncoderRewindsToACaretLeftOfItsRun(t *testing.T) {
	frame := render.NewBuffer(40, 3)
	grid := newReferenceGrid(40, 3)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	render.NewCanvas(frame).Text(20, 1, "edited", render.Style{})
	frame.SetCaret(22, 1)
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, "\r\x1b[22C") {
		t.Fatalf("a caret left of its run must rewind, not readdress: %q", data)
	}
}

// scrollingList paints a viewport of numbered rows starting at offset, framed
// by a static header and footer that must survive the scroll untouched.
func scrollingList(buffer *render.Buffer, offset int) {
	canvas := render.NewCanvas(buffer)
	canvas.Clear(render.Style{})
	_, height := buffer.Size()
	canvas.Text(0, 0, "HEADER", render.Style{Attrs: render.AttrBold})
	canvas.Text(0, height-1, "FOOTER", render.Style{Attrs: render.AttrDim})
	for row := 1; row < height-1; row++ {
		canvas.Text(0, row, "entry "+strconv.Itoa(offset+row)+" payload text", render.Style{
			Foreground: render.IndexedColor(uint8(30 + (offset+row)%40)),
		})
	}
}

func TestEncoderScrollsARegionInsteadOfRepainting(t *testing.T) {
	frame := render.NewBuffer(60, 24)
	scrollingList(frame, 0)
	grid := newReferenceGrid(60, 24)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	scrollingList(frame, 1)
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, "\x1b[2;23r") {
		t.Fatalf("the scrolled rows must be moved by the terminal: %q", data)
	}
	if !strings.Contains(data, "\x1b[1M") {
		t.Fatalf("a one-row scroll must delete one line: %q", data)
	}
	if !strings.Contains(data, dropRegion) {
		t.Fatalf("the scrolling region must be released: %q", data)
	}
	if strings.Contains(data, "HEADER") || strings.Contains(data, "FOOTER") {
		t.Fatalf("static rows outside the region were retransmitted: %q", data)
	}
	if len(data) > 120 {
		t.Fatalf("a one-row scroll cost %d bytes: %q", len(data), data)
	}
}

// A scroll is accepted on a score, not on proof, so a row that already
// matched before the shift can land on different content: the terminal moves
// it without comparing it. Its dirty bit was cleared before ScrollRows, so
// the bitset must be re-marked afterwards or that row is silently skipped.
// Five rows shift up here, but row 6 keeps the old row 6's bytes, which the
// shift vacates to a blank.
func TestEncoderRepaintsRowsThatWereEqualBeforeTheScroll(t *testing.T) {
	frame := render.NewBuffer(60, 8)
	canvas := render.NewCanvas(frame)
	canvas.Text(0, 0, "HEADER", render.Style{Attrs: render.AttrBold})
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	for row, word := range words {
		canvas.Text(0, row+1, word, render.Style{})
	}
	canvas.Text(0, 7, "FOOTER", render.Style{Attrs: render.AttrDim})
	grid := newReferenceGrid(60, 8)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Clear(render.Style{})
	canvas.Text(0, 0, "HEADER", render.Style{Attrs: render.AttrBold})
	for row := 1; row <= 5; row++ {
		canvas.Text(0, row, words[row], render.Style{})
	}
	canvas.Text(0, 6, words[5], render.Style{})
	canvas.Text(0, 7, "FOOTER", render.Style{Attrs: render.AttrDim})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, "\x1b[2;7r") {
		t.Fatalf("the shifted rows must be moved by the terminal: %q", data)
	}
	if strings.Count(data, "zeta") != 1 {
		t.Fatalf("the row exposed by the scroll must be repainted once: %q", data)
	}
	if strings.Contains(data, "alpha") {
		t.Fatalf("a row scrolled off the screen must not be retransmitted: %q", data)
	}
}

func TestEncoderScrollsBackwards(t *testing.T) {
	frame := render.NewBuffer(60, 24)
	scrollingList(frame, 8)
	grid := newReferenceGrid(60, 24)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	for _, step := range []int{7, 4, 0} {
		previous := frame.Clone()
		scrollingList(frame, step)
		data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
		if !strings.Contains(data, "L") {
			t.Fatalf("scrolling back to offset %d did not insert lines: %q", step, data)
		}
	}
}

func TestEncoderScrollsUnderEveryColorDepth(t *testing.T) {
	for _, depth := range []capability.ColorDepth{capability.TrueColor, capability.ANSI256, capability.ANSI16, capability.Monochrome} {
		for _, synchronized := range []bool{false, true} {
			frame := render.NewBuffer(48, 16)
			scrollingList(frame, 0)
			grid := newReferenceGrid(48, 16)
			grid.present(t, frame, nil, depth, synchronized)
			grid.requireMatches(t, frame, depth)
			for step := 1; step <= 5; step++ {
				previous := frame.Clone()
				scrollingList(frame, step*step)
				frame.SetCaret(step, step)
				grid.present(t, frame, previous, depth, synchronized)
				grid.requireMatches(t, frame, depth)
			}
		}
	}
}

func TestEncoderKeepsStyledRowsOutOfAScroll(t *testing.T) {
	// A scroll exposes rows the encoder never writes. On a terminal with
	// background-color erase those rows carry the active background, so the
	// style must be reset before the region moves.
	frame := render.NewBuffer(40, 12)
	canvas := render.NewCanvas(frame)
	canvas.Clear(render.Style{Background: render.IndexedColor(52)})
	for row := range 12 {
		canvas.Text(0, row, "line "+strconv.Itoa(row), render.Style{Background: render.IndexedColor(52)})
	}
	grid := newReferenceGrid(40, 12)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Clear(render.Style{Background: render.IndexedColor(52)})
	for row := range 12 {
		canvas.Text(0, row, "line "+strconv.Itoa(row+1), render.Style{Background: render.IndexedColor(52)})
	}
	encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
}

func TestEncoderIgnoresScrollsThatDoNotPay(t *testing.T) {
	frame := render.NewBuffer(40, 12)
	canvas := render.NewCanvas(frame)
	canvas.Text(0, 3, "one", render.Style{})
	grid := newReferenceGrid(40, 12)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Clear(render.Style{})
	canvas.Text(0, 2, "one", render.Style{})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if strings.Contains(data, dropRegion) {
		t.Fatalf("a shift that repairs two rows is cheaper to repaint: %q", data)
	}
}

// TestScrollBudgetIsIndependentOfScreenSize pins the whole point of the
// scrolling region: moving a list by one row costs the same on a small screen
// and a large one, because only the exposed rows are transmitted. Without the
// region the same frames cost 826 and 1434 bytes.
func TestScrollBudgetIsIndependentOfScreenSize(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		frame := render.NewBuffer(size.w, size.h)
		scrollingList(frame, 0)
		grid := newReferenceGrid(size.w, size.h)
		encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

		previous := frame.Clone()
		scrollingList(frame, 1)
		one := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)

		previous = frame.Clone()
		scrollingList(frame, 6)
		five := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)

		if len(one) > 80 {
			t.Fatalf("%dx%d one-row scroll is %d bytes: %q", size.w, size.h, len(one), one)
		}
		if len(five) > 260 {
			t.Fatalf("%dx%d five-row scroll is %d bytes: %q", size.w, size.h, len(five), five)
		}
		t.Logf("%dx%d list: scroll by 1 = %d bytes, scroll by 5 = %d bytes", size.w, size.h, len(one), len(five))
	}
}

func TestEncoderDoesNotInheritTheTerminalStyle(t *testing.T) {
	// A frame must not depend on the SGR state it finds. That matters most for a
	// scroll: on a terminal with background-color erase the exposed rows are
	// filled with the active background, which the encoder never chose.
	frame := render.NewBuffer(40, 12)
	scrollingList(frame, 0)
	grid := newReferenceGrid(40, 12)
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	grid.style = render.Style{Background: render.IndexedColor(160), Attrs: render.AttrUnderline}
	previous := frame.Clone()
	scrollingList(frame, 1)
	encodeAndVerify(t, grid, frame, previous, capability.TrueColor)

	grid.style = render.Style{Foreground: render.IndexedColor(9), Attrs: render.AttrReverse}
	previous = frame.Clone()
	render.NewCanvas(frame).Clear(render.Style{})
	encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
}
