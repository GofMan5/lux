package terminal

import (
	"regexp"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

// regionSequence matches the escape sequences only the scrolling path emits:
// DECSTBM (r), delete lines (M), and insert lines (L), including the bare
// region reset. No other sequence appendFrame produces ends in r, M, or L.
var regionSequence = regexp.MustCompile("\x1b\\[[0-9;]*[rML]")

// scrollGateFixture builds the one-row list scroll BenchmarkScrollFrame
// encodes: a 120x40 list whose viewport shifts a single entry between base
// and frame.
func scrollGateFixture() (base, frame *render.Buffer) {
	base = render.NewBuffer(120, 40)
	scrollingList(base, 0)
	frame = base.Clone()
	scrollingList(frame, 1)
	return base, frame
}

func TestScrollGateEmitsRegionSequencesWhenEnabled(t *testing.T) {
	base, frame := scrollGateFixture()

	data := appendFrame(nil, frame, base, capability.TrueColor, true, false, false)

	if len(regionSequence.FindAllIndex(data, -1)) < 1 {
		t.Fatalf("the open gate must emit region sequences, got %q", data)
	}
}

func TestScrollGateSuppressesRegionSequencesWhenDisabled(t *testing.T) {
	base, frame := scrollGateFixture()

	data := appendFrame(nil, frame, base, capability.TrueColor, false, false, false)

	if len(data) == 0 {
		t.Fatal("the closed gate must still repaint the scrolled rows, got no output")
	}
	if regionSequence.Match(data) {
		t.Fatalf("the closed gate must not emit region sequences, got %q", data)
	}
	// Sanity: the same fixture scrolls once the gate opens, so the zero
	// matches above are the gate suppressing them, not a dead fixture.
	base, frame = scrollGateFixture()
	enabled := appendFrame(nil, frame, base, capability.TrueColor, true, false, false)
	if !regionSequence.Match(enabled) {
		t.Fatalf("fixture stopped scrolling, got %q", enabled)
	}
}

func TestScrollGateFallbackReplayMatches(t *testing.T) {
	base, frame := scrollGateFixture()
	grid := newReferenceGrid(120, 40)

	// The fallback bytes must be a plain repaint: they have to bring a
	// terminal already showing base up to frame without any scrolling.
	grid.present(t, base, nil, capability.TrueColor, false)
	grid.requireMatches(t, base, capability.TrueColor)

	data := appendFrame(nil, frame, base, capability.TrueColor, false, false, false)
	grid.feed(t, data)
	grid.requireMatches(t, frame, capability.TrueColor)
}

func TestScrollGateUnchangedFrameEmitsNothing(t *testing.T) {
	base := render.NewBuffer(120, 40)
	scrollingList(base, 0)
	frame := base.Clone()

	data := appendFrame(nil, frame, base, capability.TrueColor, false, false, false)

	if len(data) != 0 {
		t.Fatalf("an unchanged frame must emit nothing, got %q", data)
	}
}

func TestScrollGateFallbackTerminalModelConsistent(t *testing.T) {
	base, frame := scrollGateFixture()
	pristine := base.Clone()

	data := appendFrame(nil, frame, base, capability.TrueColor, false, false, false)

	if len(data) == 0 {
		t.Fatal("the fallback must repaint the scrolled rows, got no output")
	}
	// The fallback repaints instead of scrolling, and the only appendFrame
	// statement that mutates the terminal model is the ScrollRows call in
	// the gated scroll branch. The model must therefore survive the fallback
	// untouched: the next frame diffs against exactly this content.
	// TestScrollGateFallbackReplayMatches proves the bytes themselves carry
	// the whole base-to-frame delta.
	_, height := base.Size()
	for y := range height {
		if !base.EqualRow(pristine, y) {
			t.Fatalf("the fallback mutated row %d of the terminal model", y)
		}
	}
	caretX, caretY, caretOn := base.Caret()
	wantX, wantY, wantOn := pristine.Caret()
	if caretX != wantX || caretY != wantY || caretOn != wantOn {
		t.Fatalf("the fallback moved the model caret to (%d, %d, %t), want (%d, %d, %t)",
			caretX, caretY, caretOn, wantX, wantY, wantOn)
	}
	// Contrast: once the gate opens, appendFrame is allowed to shift the
	// model, because the region sequences it just emitted move the real
	// screen the same way.
	base, frame = scrollGateFixture()
	pristine = base.Clone()
	enabled := appendFrame(nil, frame, base, capability.TrueColor, true, false, false)
	if !regionSequence.Match(enabled) {
		t.Fatalf("fixture stopped scrolling, got %q", enabled)
	}
	if base.Equal(pristine) {
		t.Fatal("the scroll path was expected to shift the terminal model")
	}
}

func TestScrollGateSmallFramesStaySimple(t *testing.T) {
	// A frame one row tall can never scroll, and one with fewer than
	// minScrollRows dirty rows cannot pay for a region, so the gate stays
	// shut for both even when the capability is advertised.
	tests := []struct {
		name  string
		base  *render.Buffer
		frame *render.Buffer
	}{
		{
			name:  "one row tall",
			base:  singleRow("alpha"),
			frame: singleRow("omega"),
		},
		{
			name:  "two dirty rows",
			base:  listedBuffer("one", "two", "three", "four"),
			frame: listedBuffer("one", "TWO", "three", "FOUR"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := appendFrame(nil, test.frame, test.base, capability.TrueColor, true, false, false)
			if regionSequence.Match(data) {
				t.Fatalf("a small frame must not emit region sequences, got %q", data)
			}
			if len(data) == 0 {
				t.Fatal("the changed rows must still be repainted")
			}
		})
	}
}

func singleRow(text string) *render.Buffer {
	buffer := render.NewBuffer(len(text), 1)
	render.NewCanvas(buffer).Text(0, 0, text, render.Style{})
	return buffer
}

func listedBuffer(rows ...string) *render.Buffer {
	buffer := render.NewBuffer(10, len(rows))
	canvas := render.NewCanvas(buffer)
	for y, row := range rows {
		canvas.Text(0, y, row, render.Style{})
	}
	return buffer
}
