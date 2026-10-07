package terminal

import (
	"strings"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

// paintLinksSample writes a screen whose rows exercise the OSC 8 state
// machine: a wide grapheme sharing one link across both cells, a direct
// switch between targets, a link ending mid-row, and runs whose only change
// between frames is the link.
func paintLinksSample(buffer *render.Buffer, variant int) {
	canvas := render.NewCanvas(buffer)
	link := render.Link{URI: "https://example.com/" + string(rune('a'+variant))}
	other := render.Link{ID: "row", URI: "https://other.example"}
	blank := render.Style{Background: render.IndexedColor(17)}
	canvas.Clear(blank)
	canvas.PutCell(0, 0, render.Cell{Grapheme: "日", Style: blank, Link: link})
	canvas.PutCell(3, 0, render.Cell{Grapheme: "x", Style: blank, Link: other})
	canvas.PutCell(5, 0, render.Cell{Grapheme: "y", Style: blank})
	canvas.Text(0, 1, "plain tail", blank)
}

func TestEncoderHyperlinkByteForms(t *testing.T) {
	frame := render.NewBuffer(16, 2)
	grid := newReferenceGrid(16, 2)
	grid.links = true
	canvas := render.NewCanvas(frame)
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Link: render.Link{URI: "https://example.com"}})
	canvas.PutCell(4, 0, render.Cell{Grapheme: "B", Link: render.Link{ID: "doc", URI: "https://doc.example"}})
	canvas.PutCell(8, 0, render.Cell{Grapheme: "C"})
	canvas.PutCell(10, 0, render.Cell{Grapheme: "日", Link: render.Link{URI: "https://wide.example"}})

	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
	// A frame starts with an unknown terminal state, so the first run pays
	// for the full style: the open rides ahead of it.
	if !strings.Contains(data, "\x1b]8;;https://example.com\x1b\\\x1b[0mA") {
		t.Fatalf("a bare link must open with empty params: %q", data)
	}
	if !strings.Contains(data, "\x1b]8;id=doc;https://doc.example\x1b\\B") {
		t.Fatalf("an id must ride the params field: %q", data)
	}
	// Switching targets directly is legal, so no close precedes the second open.
	if strings.Contains(data, "\x1b]8;;\x1b\\\x1b]8;id=doc") {
		t.Fatalf("the link switch was closed before reopening: %q", data)
	}
	// Three closes: the blanks after A, the blanks after B, and the erasable
	// tail after the wide grapheme. endFrame adds nothing because the writer
	// no longer has a region open.
	if count := strings.Count(data, "\x1b]8;;\x1b\\"); count != 3 {
		t.Fatalf("got %d link closes, want 3: %q", count, data)
	}
}

func TestEncoderEndFrameClosesTheLastLink(t *testing.T) {
	frame := render.NewBuffer(4, 1)
	grid := newReferenceGrid(4, 1)
	grid.links = true
	render.NewCanvas(frame).PutCell(3, 0, render.Cell{
		Grapheme: "A", Link: render.Link{URI: "https://example.com"},
	})
	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
	// The linked glyph is the last cell written, so nothing else can close
	// the region: only endFrame's close follows it.
	if !strings.Contains(data, "\x1b]8;;https://example.com\x1b\\A\x1b]8;;\x1b\\") {
		t.Fatalf("the frame must not leave a link region open: %q", data)
	}
}

func TestEncoderHyperlinkGateIsOptIn(t *testing.T) {
	build := func() *render.Buffer {
		frame := render.NewBuffer(16, 2)
		canvas := render.NewCanvas(frame)
		canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Link: render.Link{URI: "https://example.com"}})
		canvas.PutCell(4, 1, render.Cell{Grapheme: "z", Style: render.Style{Foreground: render.RGBColor(1, 2, 3)}})
		return frame
	}
	gated := string(appendFrame(nil, build(), nil, capability.TrueColor, true, false, true))
	if !strings.Contains(gated, "\x1b]8;;https://example.com\x1b\\") {
		t.Fatalf("the opted-in stream lost its link: %q", gated)
	}
	legacy := string(appendFrame(nil, build(), nil, capability.TrueColor, true, false, false))
	if strings.Contains(legacy, "\x1b]8") {
		t.Fatalf("a gated stream must never emit OSC 8: %q", legacy)
	}

	// A frame that carries no links at all is byte-identical either way, which
	// is the compatibility contract for every existing application.
	buildLegacy := func() *render.Buffer {
		frame := render.NewBuffer(16, 2)
		canvas := render.NewCanvas(frame)
		canvas.Text(0, 0, "legacy", render.Style{Foreground: render.RGBColor(4, 5, 6), Attrs: render.AttrBold})
		return frame
	}
	opted := string(appendFrame(nil, buildLegacy(), nil, capability.TrueColor, true, false, true))
	plain := string(appendFrame(nil, buildLegacy(), nil, capability.TrueColor, true, false, false))
	if opted != plain {
		t.Fatalf("opt-in changed bytes for a link-free frame:\n%q\n%q", opted, plain)
	}
}

func TestEncoderRepaintsCellsWhoseLinkChanged(t *testing.T) {
	frame := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(frame)
	style := render.Style{Foreground: render.RGBColor(9, 9, 9)}
	canvas.Put(0, 0, "A", style)
	grid := newReferenceGrid(8, 1)
	grid.links = true
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Style: style, Link: render.Link{URI: "https://example.com"}})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if !strings.Contains(data, "\x1b]8;;https://example.com\x1b\\\x1b[0;38;2;9;9;9mA") {
		t.Fatalf("a link-only change must be transmitted: %q", data)
	}

	// The gated encoder never lets cell links influence the diff, so the same
	// change is invisible there and the frame stays empty.
	previous = frame.Clone()
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Style: style})
	if data := appendFrame(nil, frame, previous, capability.TrueColor, true, false, false); len(data) != 0 {
		t.Fatalf("a link-only change leaked into gated output: %q", data)
	}
}

func TestEncoderClosesHyperlinkBeforeResettingStyle(t *testing.T) {
	frame := render.NewBuffer(40, 4)
	canvas := render.NewCanvas(frame)
	// Row 2 carries text now; the next frame erases it, and row 1 gains link
	// cells, so one frame both opens a region and erases: the erase must not
	// let the region swallow the cells it exposes.
	canvas.Text(0, 2, "YYYY", render.Style{Foreground: render.RGBColor(70, 80, 90)})
	grid := newReferenceGrid(40, 4)
	grid.links = true
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	canvas.Text(0, 2, "    ", render.Style{})
	style := render.Style{Foreground: render.RGBColor(10, 20, 30)}
	for x := range 30 {
		canvas.PutCell(x, 1, render.Cell{Grapheme: "X", Style: style, Link: render.Link{URI: "https://example.com"}})
	}
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	// Erasing fills with the active background, so the open link must close
	// before the style reset, or the erased cells would inherit the region.
	if !strings.Contains(data, "\x1b]8;;\x1b\\\x1b[0m\x1b[K") {
		t.Fatalf("the erase must close the link before the reset: %q", data)
	}
}

func TestEncoderStyledUnderlineForms(t *testing.T) {
	frame := render.NewBuffer(24, 1)
	canvas := render.NewCanvas(frame)
	red := render.Style{Attrs: render.AttrUnderline, UnderlineStyle: render.UnderlineCurly, UnderlineColor: render.RGBColor(1, 2, 3)}
	canvas.Put(0, 0, "a", red)
	canvas.Put(1, 0, "b", render.Style{Attrs: render.AttrUnderline, UnderlineColor: render.RGBColor(1, 2, 3)})
	green := render.Style{Attrs: render.AttrUnderline, UnderlineStyle: render.UnderlineDouble, UnderlineColor: render.RGBColor(4, 5, 6)}
	canvas.Put(2, 0, "c", green)
	canvas.Put(3, 0, "d", render.Style{Attrs: render.AttrUnderline, UnderlineStyle: render.UnderlineDouble})
	grid := newReferenceGrid(24, 1)
	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	// The full form leads with the plain code for terminals that ignore
	// subparameters, then refines the shape, then the color.
	if !strings.Contains(data, "\x1b[0;4;4:3;58;2;1;2;3m") {
		t.Fatalf("curly underline with color emitted %q", data)
	}
	// Curly back to straight re-issues the plain code, which sets the shape.
	if !strings.Contains(data, "\x1b[4mb") {
		t.Fatalf("returning to straight emitted %q", data)
	}
	if !strings.Contains(data, "\x1b[4;4:2;58;2;4;5;6mc") {
		t.Fatalf("shape and color change emitted %q", data)
	}
	if !strings.Contains(data, "\x1b[59md") {
		t.Fatalf("dropping the underline color must emit SGR 59: %q", data)
	}
}

func TestReferenceGridUnderlinesAndLinksRoundTrip(t *testing.T) {
	depths := []struct {
		name  string
		depth capability.ColorDepth
	}{
		{"truecolor", capability.TrueColor},
		{"ansi256", capability.ANSI256},
		{"ansi16", capability.ANSI16},
		{"monochrome", capability.Monochrome},
	}
	for _, synchronized := range []bool{false, true} {
		for _, depth := range depths {
			name := depth.name
			if synchronized {
				name += "/sync"
			}
			t.Run(name, func(t *testing.T) {
				frame := render.NewBuffer(20, 4)
				paintLinksSample(frame, 0)
				underlined := render.Style{
					Attrs:          render.AttrUnderline | render.AttrItalic,
					UnderlineStyle: render.UnderlineDotted,
					UnderlineColor: render.IndexedColor(213),
				}
				render.NewCanvas(frame).Text(0, 2, "notes émotions", underlined)
				grid := newReferenceGrid(20, 4)
				grid.links = true
				grid.present(t, frame, nil, depth.depth, synchronized)
				grid.requireMatches(t, frame, depth.depth)

				for variant := 1; variant <= 2; variant++ {
					previous := frame.Clone()
					paintLinksSample(frame, variant)
					// The underline row changes shape and color but keeps text.
					render.NewCanvas(frame).Text(0, 2, "notes émotions", render.Style{
						Attrs:          render.AttrUnderline | render.AttrItalic,
						UnderlineStyle: render.UnderlineStyle(1 + variant),
						UnderlineColor: render.RGBColor(uint8(10*variant), 20, 30),
					})
					frame.SetCaret(variant, variant%4)
					grid.present(t, frame, previous, depth.depth, synchronized)
					grid.requireMatches(t, frame, depth.depth)
				}

				// A link row erased back to blanks must be transmitted. At
				// monochrome the scroll repair can win over the erase, so the
				// byte shape is only pinned where colors keep rows distinct.
				previous := frame.Clone()
				render.NewCanvas(frame).Clear(render.Style{})
				frame.HideCaret()
				data := grid.present(t, frame, previous, depth.depth, synchronized)
				grid.requireMatches(t, frame, depth.depth)
				if grid.links && depth.depth != capability.Monochrome && !strings.Contains(string(data), eraseLine) {
					t.Fatalf("a cleared link row was not erased: %q", data)
				}
			})
		}
	}
}
