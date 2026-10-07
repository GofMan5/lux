package terminal

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
	"github.com/rivo/uniseg"
)

// referenceGrid is a deliberately narrow VT interpreter: it understands exactly
// the sequences appendFrame can emit and nothing else. Reconstructing a grid
// from real output and comparing it with the source buffer proves the encoder,
// instead of asserting the byte strings we happen to produce today.
type referenceGrid struct {
	buffer   *render.Buffer
	canvas   *render.Canvas
	style    render.Style
	link     render.Link
	links    bool
	x, y     int
	top      int
	bottom   int
	caretOn  bool
	sync     int
	maxSync  int
	sawColor bool
}

// newReferenceGrid starts from a blank screen. One grid is fed every successive
// frame, exactly like a real terminal, so retained cells are what makes a
// changed-run diff verifiable.
func newReferenceGrid(width, height int) *referenceGrid {
	buffer := render.NewBuffer(width, height)
	grid := &referenceGrid{buffer: buffer, bottom: height - 1}
	grid.canvas = render.NewCanvas(buffer)
	return grid
}

func (g *referenceGrid) feed(t *testing.T, data []byte) {
	t.Helper()
	text := string(data)
	run := strings.Builder{}
	flush := func() {
		if run.Len() == 0 {
			return
		}
		g.print(run.String())
		run.Reset()
	}
	for index := 0; index < len(text); {
		if text[index] == '\r' {
			flush()
			g.x = 0
			index++
			continue
		}
		if text[index] != 0x1b {
			run.WriteByte(text[index])
			index++
			continue
		}
		flush()
		if strings.HasPrefix(text[index:], "\x1b]8;") {
			consumed := g.osc8(text[index:])
			if consumed <= 0 {
				t.Fatalf("reference grid: malformed OSC 8 at %d: %q", index, text[index:min(index+24, len(text))])
			}
			index += consumed
			continue
		}
		consumed := g.escape(t, text[index:])
		if consumed <= 0 {
			t.Fatalf("reference grid: unsupported sequence at %d: %q", index, text[index:min(index+16, len(text))])
		}
		index += consumed
	}
	flush()
}

// osc8 consumes one hyperlink sequence terminated by ST, which is the only
// terminator the encoder emits, and switches the region the interpreter is
// inside. The close form carries empty parameters and no URI; the params
// field defines exactly one key, id.
func (g *referenceGrid) osc8(text string) int {
	rest := strings.TrimPrefix(text, "\x1b]8;")
	end := strings.Index(rest, "\x1b\\")
	if end < 0 {
		return 0
	}
	params, uri, ok := strings.Cut(rest[:end], ";")
	if !ok {
		return 0
	}
	link := render.Link{URI: uri}
	if params != "" {
		if !strings.HasPrefix(params, "id=") {
			return 0
		}
		link.ID = params[len("id="):]
	}
	g.link = link
	return len(text) - len(rest) + end + 2
}

func (g *referenceGrid) escape(t *testing.T, text string) int {
	t.Helper()
	if !strings.HasPrefix(text, "\x1b[") {
		return 0
	}
	end := strings.IndexAny(text[2:], "CHKLMhlmr")
	if end < 0 {
		return 0
	}
	end += 2
	body, final := text[2:end], text[end]
	width, height := g.buffer.Size()
	switch {
	case final == 'r':
		g.top, g.bottom = 0, height-1
		if body != "" {
			first, last, ok := strings.Cut(body, ";")
			if !ok {
				return 0
			}
			top, errTop := strconv.Atoi(first)
			bottom, errBottom := strconv.Atoi(last)
			if errTop != nil || errBottom != nil || top < 1 || bottom > height || top >= bottom {
				return 0
			}
			g.top, g.bottom = top-1, bottom-1
		}
		// DECSTBM homes the cursor.
		g.x, g.y = 0, 0
	case final == 'L' || final == 'M':
		lines := 1
		if body != "" {
			value, err := strconv.Atoi(body)
			if err != nil || value < 1 {
				return 0
			}
			lines = value
		}
		if g.y < g.top || g.y > g.bottom {
			// Insert and delete line are ignored outside the scrolling region.
			return end + 1
		}
		delta := lines
		exposedTop, exposedBottom := g.bottom-lines+1, g.bottom
		if final == 'L' {
			delta, exposedTop, exposedBottom = -lines, g.y, min(g.y+lines-1, g.bottom)
		}
		g.buffer.ScrollRows(g.y, g.bottom, delta)
		// Background-color erase fills the exposed rows with the active
		// background, so an encoder that forgets to reset its style is caught.
		if g.style != (render.Style{}) {
			for y := max(exposedTop, g.top); y <= exposedBottom; y++ {
				for x := 0; x < width; x++ {
					g.canvas.Put(x, y, " ", g.style)
				}
			}
		}
		g.x = 0
	case final == 'H':
		row, column, ok := strings.Cut(body, ";")
		if !ok {
			return 0
		}
		y, errRow := strconv.Atoi(row)
		x, errColumn := strconv.Atoi(column)
		if errRow != nil || errColumn != nil || x < 1 || y < 1 {
			return 0
		}
		g.x, g.y = x-1, y-1
	case final == 'C':
		steps := 1
		if body != "" {
			value, err := strconv.Atoi(body)
			if err != nil || value < 1 {
				return 0
			}
			steps = value
		}
		g.x = min(g.x+steps, width-1)
	case final == 'K':
		if body != "" && body != "0" {
			return 0
		}
		for x := g.x; x < width; x++ {
			g.canvas.Put(x, g.y, " ", g.style)
		}
	case final == 'm':
		if !g.sgr(body) {
			return 0
		}
	case body == "?25" && final == 'h':
		g.caretOn = true
	case body == "?25" && final == 'l':
		g.caretOn = false
	case body == "?2026" && final == 'h':
		g.sync++
		g.maxSync = max(g.maxSync, g.sync)
	case body == "?2026" && final == 'l':
		g.sync--
	default:
		return 0
	}
	return end + 1
}

func (g *referenceGrid) sgr(body string) bool {
	parameters := strings.Split(body, ";")
	attrs := map[string]render.Attr{
		"1": render.AttrBold, "2": render.AttrDim, "3": render.AttrItalic, "4": render.AttrUnderline,
		"5": render.AttrBlink, "7": render.AttrReverse, "8": render.AttrHidden, "9": render.AttrStrike,
	}
	// SGR 22 clears bold and dim together, which is why the encoder restates the
	// survivor instead of assuming one code per attribute.
	cleared := map[string]render.Attr{
		"22": render.AttrBold | render.AttrDim, "23": render.AttrItalic, "24": render.AttrUnderline,
		"25": render.AttrBlink, "27": render.AttrReverse, "28": render.AttrHidden, "29": render.AttrStrike,
	}
	for index := 0; index < len(parameters); index++ {
		parameter := parameters[index]
		if parameter == "0" || parameter == "" {
			g.style = render.Style{}
			continue
		}
		if shape, on, ok := parseUnderlineShape(parameter); ok {
			if on {
				g.style.Attrs |= render.AttrUnderline
				g.style.UnderlineStyle = shape
			} else {
				g.style.Attrs &^= render.AttrUnderline
			}
			continue
		}
		if attr, ok := attrs[parameter]; ok {
			g.style.Attrs |= attr
			if attr == render.AttrUnderline {
				// The plain code is a straight underline, whatever shape the
				// region carried before.
				g.style.UnderlineStyle = render.UnderlineSingle
			}
			continue
		}
		if attr, ok := cleared[parameter]; ok {
			g.style.Attrs &^= attr
			continue
		}
		value, err := strconv.Atoi(parameter)
		if err != nil {
			return false
		}
		switch {
		case value == 38 || value == 48:
			color, consumed, ok := parseExtendedColor(parameters[index:])
			if !ok {
				return false
			}
			g.setColor(value == 38, color)
			index += consumed - 1
		case value == 58:
			color, consumed, ok := parseExtendedColor(parameters[index:])
			if !ok {
				return false
			}
			g.style.UnderlineColor = color
			index += consumed - 1
		case value == 59:
			g.style.UnderlineColor = render.Color{}
		case value == 39 || value == 49:
			g.setColor(value == 39, render.Color{})
		case value >= 30 && value <= 37:
			g.setColor(true, render.ANSIColor(uint8(value-30)))
		case value >= 40 && value <= 47:
			g.setColor(false, render.ANSIColor(uint8(value-40)))
		case value >= 90 && value <= 97:
			g.setColor(true, render.ANSIColor(uint8(value-90+8)))
		case value >= 100 && value <= 107:
			g.setColor(false, render.ANSIColor(uint8(value-100+8)))
		default:
			return false
		}
	}
	return true
}

// parseUnderlineShape reads the 4:x subparameter form. Semicolons separate
// parameters, so the colon form survives as one field and is examined before
// the plain codes. 4:0 removes the underline; 4:1 through 4:5 set the shape.
func parseUnderlineShape(parameter string) (shape render.UnderlineStyle, on bool, ok bool) {
	if len(parameter) < 3 || parameter[:2] != "4:" {
		return render.UnderlineSingle, false, false
	}
	value, err := strconv.Atoi(parameter[2:])
	if err != nil {
		return render.UnderlineSingle, false, false
	}
	if value == 0 {
		return render.UnderlineSingle, false, true
	}
	if value < 1 || value > 5 {
		return render.UnderlineSingle, false, false
	}
	// The wire digit runs 1 through 5 over the enum values 0 through 4.
	return render.UnderlineStyle(value - 1), true, true
}

func parseExtendedColor(parameters []string) (render.Color, int, bool) {
	if len(parameters) < 3 {
		return render.Color{}, 0, false
	}
	switch parameters[1] {
	case "5":
		index, err := strconv.Atoi(parameters[2])
		if err != nil || index < 0 || index > 255 {
			return render.Color{}, 0, false
		}
		return render.IndexedColor(uint8(index)), 3, true
	case "2":
		if len(parameters) < 5 {
			return render.Color{}, 0, false
		}
		channels := [3]uint8{}
		for offset := range channels {
			value, err := strconv.Atoi(parameters[2+offset])
			if err != nil || value < 0 || value > 255 {
				return render.Color{}, 0, false
			}
			channels[offset] = uint8(value)
		}
		return render.RGBColor(channels[0], channels[1], channels[2]), 5, true
	default:
		return render.Color{}, 0, false
	}
}

func (g *referenceGrid) setColor(foreground bool, color render.Color) {
	g.sawColor = true
	if foreground {
		g.style.Foreground = color
		return
	}
	g.style.Background = color
}

func (g *referenceGrid) print(text string) {
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		cluster := graphemes.Str()
		width := render.GraphemeWidth(cluster)
		// PutCell is the only public way to attach a link, and it stamps the
		// continuation cell of a wide grapheme exactly like the encoder does.
		g.canvas.PutCell(g.x, g.y, render.Cell{Grapheme: cluster, Style: g.style, Link: g.link})
		g.x += max(width, 1)
	}
}

// present feeds one encoded frame into the grid and returns the emitted bytes.
// Scroll regions stay enabled: the reference grid tests the byte stream a
// capable terminal sees, while dedicated tests exercise the fallback gate.
func (g *referenceGrid) present(t *testing.T, frame, previous *render.Buffer, depth capability.ColorDepth, synchronized bool) []byte {
	t.Helper()
	data := appendFrame(nil, frame, previous, depth, true, synchronized, g.links)
	g.feed(t, data)
	if g.sync != 0 {
		t.Fatalf("reference grid: unbalanced synchronized output wrapper: %d", g.sync)
	}
	return data
}

func (g *referenceGrid) requireMatches(t *testing.T, frame *render.Buffer, depth capability.ColorDepth) {
	t.Helper()
	width, height := frame.Size()
	gridWidth, gridHeight := g.buffer.Size()
	if width != gridWidth || height != gridHeight {
		t.Fatalf("reference grid: size %dx%d, frame %dx%d", gridWidth, gridHeight, width, height)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !equalRenderedCell(frame, g.buffer, x, y, depth, g.links) {
				t.Fatalf("reference grid: cell (%d,%d) is %#v, frame has %#v", x, y, g.buffer.Cell(x, y), frame.Cell(x, y))
			}
		}
	}
	caretX, caretY, caretOn := frame.Caret()
	// A terminal has one cursor: the caret is wherever the frame left it, so the
	// encoder must position it last whenever the caret is visible.
	if caretOn != g.caretOn || caretOn && (caretX != g.x || caretY != g.y) {
		t.Fatalf("reference grid: caret (%d,%d,%t), frame wants (%d,%d,%t)", g.x, g.y, g.caretOn, caretX, caretY, caretOn)
	}
}

func paintSample(buffer *render.Buffer, variant int) {
	canvas := render.NewCanvas(buffer)
	canvas.Clear(render.Style{})
	styles := []render.Style{
		{Foreground: render.RGBColor(220, 40, 90), Background: render.RGBColor(12, 14, 18)},
		{Foreground: render.IndexedColor(213), Attrs: render.AttrBold | render.AttrUnderline},
		{Foreground: render.ANSIColor(3), Background: render.ANSIColor(12), Attrs: render.AttrReverse},
		{Attrs: render.AttrDim | render.AttrItalic | render.AttrStrike},
	}
	lines := []string{
		"LUX " + strconv.Itoa(variant),
		"日本語テキスト",
		"émotion \U0001F1FA\U0001F1F8",
		"tail",
	}
	for index, line := range lines {
		canvas.Text(index%3, index, line, styles[(index+variant)%len(styles)])
	}
	canvas.Border(render.Rect{X: 0, Y: 0, W: 12, H: 4}, render.SingleBorder, styles[variant%len(styles)])
}

func TestReferenceGridMatchesEncodedFrames(t *testing.T) {
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
				frame := render.NewBuffer(20, 6)
				paintSample(frame, 0)
				grid := newReferenceGrid(20, 6)
				data := grid.present(t, frame, nil, depth.depth, synchronized)
				grid.requireMatches(t, frame, depth.depth)
				if synchronized && grid.maxSync != 1 {
					t.Fatalf("synchronized output was not wrapped: %d", grid.maxSync)
				}
				if depth.depth == capability.Monochrome && grid.sawColor {
					// Comparing through equalRenderedCell ignores color at this
					// depth, so assert no color parameter was emitted at all.
					t.Fatalf("monochrome output carries color: %q", data)
				}

				// A changed-run diff transmits only differences, so the same grid
				// must remain correct across successive frames.
				for variant := 1; variant <= 3; variant++ {
					previous := frame.Clone()
					paintSample(frame, variant)
					frame.SetCaret(variant, variant%6)
					grid.present(t, frame, previous, depth.depth, synchronized)
					grid.requireMatches(t, frame, depth.depth)
				}

				// Cells change while the caret stays put: the cursor must be
				// repositioned anyway, because the runs moved it. requireMatches
				// compares the grid cursor with the frame caret, so an encoder that
				// only repositions on caret movement fails here.
				previous := frame.Clone()
				caretX, caretY, _ := frame.Caret()
				render.NewCanvas(frame).Put(19, 5, "Z", render.Style{Attrs: render.AttrBold})
				frame.SetCaret(caretX, caretY)
				grid.present(t, frame, previous, depth.depth, synchronized)
				grid.requireMatches(t, frame, depth.depth)

				// Erasing content back to blanks must be transmitted too.
				previous = frame.Clone()
				render.NewCanvas(frame).Clear(render.Style{})
				frame.HideCaret()
				grid.present(t, frame, previous, depth.depth, synchronized)
				grid.requireMatches(t, frame, depth.depth)
			})
		}
	}
}

func TestReferenceGridRetainsUntransmittedCells(t *testing.T) {
	// Content that is never retransmitted can only match if the grid remembers
	// it, which is what makes a changed-run diff verifiable at all.
	frame := render.NewBuffer(16, 3)
	canvas := render.NewCanvas(frame)
	canvas.Clear(render.Style{})
	canvas.Text(0, 0, "STABLE HEADER", render.Style{Foreground: render.RGBColor(20, 200, 120)})
	grid := newReferenceGrid(16, 3)
	grid.present(t, frame, nil, capability.TrueColor, false)
	grid.requireMatches(t, frame, capability.TrueColor)

	previous := frame.Clone()
	canvas.Text(0, 1, "row two", render.Style{Attrs: render.AttrItalic})
	data := grid.present(t, frame, previous, capability.TrueColor, false)
	grid.requireMatches(t, frame, capability.TrueColor)
	if strings.Contains(string(data), "STABLE") {
		t.Fatalf("an unchanged row was retransmitted: %q", data)
	}
	if !strings.Contains(string(data), "\x1b[2;1H") {
		t.Fatalf("the changed row was not addressed: %q", data)
	}
}

func TestReferenceGridFullRepaintAfterResize(t *testing.T) {
	frame := render.NewBuffer(20, 6)
	paintSample(frame, 1)
	grid := newReferenceGrid(20, 6)
	grid.present(t, frame, nil, capability.TrueColor, false)
	grid.requireMatches(t, frame, capability.TrueColor)

	previous := frame.Clone()
	frame.Resize(9, 3)
	paintSample(frame, 2)
	resized := newReferenceGrid(9, 3)
	data := resized.present(t, frame, previous, capability.TrueColor, false)
	resized.requireMatches(t, frame, capability.TrueColor)
	if !strings.Contains(string(data), "\x1b[1;1H") {
		t.Fatalf("a resized frame must repaint from the origin: %q", data)
	}
}

func TestReferenceGridCaretOnlyChange(t *testing.T) {
	frame := render.NewBuffer(12, 3)
	paintSample(frame, 0)
	frame.SetCaret(4, 1)
	grid := newReferenceGrid(12, 3)
	grid.present(t, frame, nil, capability.ANSI256, false)
	grid.requireMatches(t, frame, capability.ANSI256)

	previous := frame.Clone()
	frame.SetCaret(7, 2)
	data := grid.present(t, frame, previous, capability.ANSI256, false)
	grid.requireMatches(t, frame, capability.ANSI256)
	if want := "\x1b[3;8H"; string(data) != want {
		t.Fatalf("caret-only frame emitted %q, want %q", data, want)
	}

	previous = frame.Clone()
	if data := appendFrame(nil, frame, previous, capability.ANSI256, true, false, grid.links); len(data) != 0 {
		t.Fatalf("an unchanged frame must emit nothing, got %q", data)
	}
}
