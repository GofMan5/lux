package terminal

import (
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
		scratch = appendFrame(scratch[:0], frame, previous, capability.TrueColor, false)
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
		appendFrame(nil, frame, previous, capability.TrueColor, false)
	}
}
