package terminal

import (
	"strconv"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

const (
	resetStyle = "\x1b[0m"
	showCaret  = "\x1b[?25h"
	hideCaret  = "\x1b[?25l"
	beginSync  = "\x1b[?2026h"
	endSync    = "\x1b[?2026l"
	eraseLine  = "\x1b[K"
)

func appendFrame(dst []byte, frame, previous *render.Buffer, depth capability.ColorDepth, synchronized bool) []byte {
	width, height := frame.Size()
	previousWidth, previousHeight := previous.Size()
	repaint := previous == nil || width != previousWidth || height != previousHeight
	start := len(dst)
	if synchronized {
		dst = append(dst, beginSync...)
	}
	wrote := false
	var style styleWriter
	var at cursor
	for y := 0; y < height; y++ {
		blankFrom := -1
		for x := 0; x < width; {
			cell := frame.Cell(x, y)
			if cell.Width == 0 || (!repaint && equalRenderedCell(frame, previous, x, y, depth)) {
				x++
				continue
			}
			if blankFrom < 0 {
				blankFrom = erasableTail(frame, y, width)
			}
			runStart := x
			for x < width {
				cell = frame.Cell(x, y)
				if cell.Width == 0 {
					x++
					continue
				}
				if !repaint && equalRenderedCell(frame, previous, x, y, depth) {
					break
				}
				x += max(int(cell.Width), 1)
			}
			dst = at.moveTo(dst, runStart, y)
			wrote = true
			column, erased := runStart, false
			for column < x {
				// Erasing the tail is shorter than transmitting its blanks, and it
				// also covers every later run on this row, which the frame wants
				// blank as well.
				if column >= blankFrom && x-column > len(eraseLine) {
					dst = style.apply(dst, render.Style{}, depth)
					dst = append(dst, eraseLine...)
					erased = true
					break
				}
				cell = frame.Cell(column, y)
				if cell.Width == 0 {
					column++
					continue
				}
				dst = style.apply(dst, cell.Style, depth)
				grapheme := cell.Grapheme
				if grapheme == "" {
					grapheme = " "
				}
				dst = append(dst, grapheme...)
				column += max(int(cell.Width), 1)
			}
			at.arrive(column, y, width)
			if erased {
				break
			}
		}
	}
	caretX, caretY, caretOn := frame.Caret()
	oldX, oldY, caretWasOn := previous.Caret()
	caretMoved := caretOn != caretWasOn || caretOn && (caretX != oldX || caretY != oldY)
	if !wrote && !caretMoved {
		return dst[:start]
	}
	if wrote && !style.isDefault() {
		dst = append(dst, resetStyle...)
	}
	switch {
	case caretOn:
		// Cell runs left the cursor wherever the last run ended, so the caret is
		// repositioned whenever anything was written, not only when it moved.
		dst = at.moveTo(dst, caretX, caretY)
		if !caretWasOn {
			dst = append(dst, showCaret...)
		}
	case caretWasOn:
		dst = append(dst, hideCaret...)
	}
	if synchronized {
		dst = append(dst, endSync...)
	}
	return dst
}

// erasableTail returns the first column of the trailing run of default-styled
// blanks on row y, or width when the row does not end in one. Erase-to-end-of-
// line is only equivalent to those cells: a background-color-erase terminal
// fills with the active background, so the encoder resets the style first and
// refuses tails that carry any style of their own.
func erasableTail(frame *render.Buffer, y, width int) int {
	column := width
	for column > 0 {
		cell := frame.Cell(column-1, y)
		if cell.Width != 1 || cell.Style != (render.Style{}) || (cell.Grapheme != " " && cell.Grapheme != "") {
			break
		}
		column--
	}
	return column
}

// cursor tracks where emitted output leaves the terminal cursor so the next
// move can be relative. It is deliberately conservative: any position the
// encoder cannot prove is forgotten, and absolute addressing is used instead.
type cursor struct {
	x, y  int
	known bool
}

func (c *cursor) moveTo(dst []byte, x, y int) []byte {
	if c.known && c.y == y {
		from, rewind := c.x, 0
		if x < from {
			// A carriage return is the cheapest way back to the left edge.
			from, rewind = 0, 1
		}
		if rewind+forwardCost(x-from) < 4+digits(y+1)+digits(x+1) {
			if rewind != 0 {
				dst = append(dst, '\r')
			}
			c.x = x
			return appendForward(dst, x-from)
		}
	}
	c.x, c.y, c.known = x, y, true
	return appendCursor(dst, y+1, x+1)
}

// arrive records where printing left the cursor. A run that reached the right
// edge leaves the terminal in its pending-wrap state, whose column is not
// portable, so the position is forgotten rather than guessed.
func (c *cursor) arrive(x, y, width int) {
	c.x, c.y, c.known = x, y, x < width
}

func forwardCost(steps int) int {
	switch {
	case steps <= 0:
		return 0
	case steps == 1:
		return 3
	default:
		return 3 + digits(steps)
	}
}

func appendForward(dst []byte, steps int) []byte {
	if steps <= 0 {
		return dst
	}
	dst = append(dst, '\x1b', '[')
	if steps > 1 {
		dst = strconv.AppendInt(dst, int64(steps), 10)
	}
	return append(dst, 'C')
}

func digits(value int) int {
	count := 1
	for value >= 10 {
		value /= 10
		count++
	}
	return count
}

func equalRenderedCell(frame, previous *render.Buffer, x, y int, depth capability.ColorDepth) bool {
	current, currentOK := frame.CellOK(x, y)
	old, oldOK := previous.CellOK(x, y)
	if currentOK != oldOK || !currentOK {
		return false
	}
	if current == old {
		// Identical cells always render identically, and most cells in a frame
		// are identical, so the downgrade comparison is only reached by the few
		// that actually differ.
		return true
	}
	if current.Grapheme != old.Grapheme || current.Width != old.Width || current.Style.Attrs != old.Style.Attrs {
		return false
	}
	return equalRenderedColor(current.Style.Foreground, old.Style.Foreground, depth) && equalRenderedColor(current.Style.Background, old.Style.Background, depth)
}

func equalRenderedColor(left, right render.Color, depth capability.ColorDepth) bool {
	if depth == capability.Monochrome {
		return true
	}
	leftMode, leftValue := renderedColor(left, depth)
	rightMode, rightValue := renderedColor(right, depth)
	return leftMode == rightMode && leftValue == rightValue
}

func renderedColor(color render.Color, depth capability.ColorDepth) (render.ColorMode, uint32) {
	if color.Mode == render.ColorDefault {
		return render.ColorDefault, 0
	}
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			return render.ColorRGB, color.Value
		case capability.ANSI256:
			return render.ColorIndexed, uint32(rgb256(red, green, blue))
		default:
			return render.ColorANSI, uint32(rgb16(red, green, blue))
		}
	}
	if color.Mode == render.ColorIndexed && depth == capability.ANSI16 {
		return render.ColorANSI, uint32(indexed16(uint8(color.Value)))
	}
	if color.Mode == render.ColorANSI {
		return render.ColorANSI, color.Value & 15
	}
	return color.Mode, color.Value
}

func appendCursor(dst []byte, row, column int) []byte {
	dst = append(dst, '\x1b', '[')
	dst = strconv.AppendInt(dst, int64(row), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(column), 10)
	return append(dst, 'H')
}

var attrCodes = [...]struct {
	mask render.Attr
	on   string
	off  string
}{
	{render.AttrBold, "1", "22"}, {render.AttrDim, "2", "22"}, {render.AttrItalic, "3", "23"},
	{render.AttrUnderline, "4", "24"}, {render.AttrBlink, "5", "25"}, {render.AttrReverse, "7", "27"},
	{render.AttrHidden, "8", "28"}, {render.AttrStrike, "9", "29"},
}

// intensity is the pair SGR 22 turns off together, so clearing one restates the
// other.
const intensity = render.AttrBold | render.AttrDim

// styleWriter emits the shortest SGR that turns the style the terminal already
// carries into the one a cell needs. A delta is usually shorter than restating
// the whole style, but not always: clearing several attributes costs more than
// a reset, so both forms are built and the shorter one is written. The scratch
// arrays hold the longest form either can produce, so building both is free.
type styleWriter struct {
	current  render.Style
	known    bool
	fullBuf  [64]byte
	deltaBuf [64]byte
}

func (w *styleWriter) apply(dst []byte, style render.Style, depth capability.ColorDepth) []byte {
	if w.known && w.current == style {
		return dst
	}
	full := appendFullStyle(w.fullBuf[:0], style, depth)
	if w.known {
		// An empty delta means the terminal already renders this style at the
		// active color depth, which is why it is compared before the full form.
		if delta := appendStyleDelta(w.deltaBuf[:0], w.current, style, depth); len(delta) < len(full) {
			w.current = style
			return append(dst, delta...)
		}
	}
	w.current, w.known = style, true
	return append(dst, full...)
}

func (w *styleWriter) isDefault() bool { return w.known && w.current == render.Style{} }

func appendFullStyle(dst []byte, style render.Style, depth capability.ColorDepth) []byte {
	dst = append(dst, '\x1b', '[', '0')
	first := false
	for _, attribute := range attrCodes {
		if style.Attrs&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.on)
		}
	}
	dst = appendColor(dst, &first, style.Foreground, depth, true)
	dst = appendColor(dst, &first, style.Background, depth, false)
	return append(dst, 'm')
}

func appendStyleDelta(dst []byte, from, to render.Style, depth capability.ColorDepth) []byte {
	start := len(dst)
	dst = append(dst, '\x1b', '[')
	first := true
	cleared, added := from.Attrs&^to.Attrs, to.Attrs&^from.Attrs
	if cleared&intensity != 0 {
		dst = appendParam(dst, &first, "22")
		added |= to.Attrs & intensity
	}
	for _, attribute := range attrCodes {
		if attribute.mask&intensity == 0 && cleared&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.off)
		}
	}
	for _, attribute := range attrCodes {
		if added&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.on)
		}
	}
	if !equalRenderedColor(from.Foreground, to.Foreground, depth) {
		dst = appendColorChange(dst, &first, to.Foreground, depth, true)
	}
	if !equalRenderedColor(from.Background, to.Background, depth) {
		dst = appendColorChange(dst, &first, to.Background, depth, false)
	}
	if first {
		return dst[:start]
	}
	return append(dst, 'm')
}

func appendColorChange(dst []byte, first *bool, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
	if color.Mode == render.ColorDefault {
		if foreground {
			return appendParam(dst, first, "39")
		}
		return appendParam(dst, first, "49")
	}
	return appendColor(dst, first, color, depth, foreground)
}

func appendParam(dst []byte, first *bool, code string) []byte {
	return append(appendSeparator(dst, first), code...)
}

func appendSeparator(dst []byte, first *bool) []byte {
	if !*first {
		dst = append(dst, ';')
	}
	*first = false
	return dst
}

func appendColor(dst []byte, first *bool, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
	if color.Mode == render.ColorDefault || depth == capability.Monochrome {
		return dst
	}
	index := uint8(color.Value)
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			dst = appendSeparator(dst, first)
			if foreground {
				dst = append(dst, "38;2;"...)
			} else {
				dst = append(dst, "48;2;"...)
			}
			dst = strconv.AppendInt(dst, int64(red), 10)
			dst = append(dst, ';')
			dst = strconv.AppendInt(dst, int64(green), 10)
			dst = append(dst, ';')
			return strconv.AppendInt(dst, int64(blue), 10)
		case capability.ANSI256:
			index = rgb256(red, green, blue)
		default:
			index = rgb16(red, green, blue)
		}
	} else if color.Mode == render.ColorIndexed && depth == capability.ANSI16 {
		index = indexed16(index)
	}
	if depth >= capability.ANSI256 && (color.Mode == render.ColorIndexed || color.Mode == render.ColorRGB) {
		dst = appendSeparator(dst, first)
		if foreground {
			dst = append(dst, "38;5;"...)
		} else {
			dst = append(dst, "48;5;"...)
		}
		return strconv.AppendInt(dst, int64(index), 10)
	}
	index &= 15
	code := 30 + int(index&7)
	if index >= 8 {
		code += 60
	}
	if !foreground {
		code += 10
	}
	dst = appendSeparator(dst, first)
	return strconv.AppendInt(dst, int64(code), 10)
}

func rgb256(red, green, blue uint8) uint8 {
	if red == green && green == blue {
		if red < 8 {
			return 16
		}
		if red > 248 {
			return 231
		}
		return uint8(232 + (int(red)-8)*24/247)
	}
	r := (int(red)*5 + 127) / 255
	g := (int(green)*5 + 127) / 255
	b := (int(blue)*5 + 127) / 255
	return uint8(16 + 36*r + 6*g + b)
}

func rgb16(red, green, blue uint8) uint8 {
	index := uint8(0)
	if red >= 128 {
		index |= 1
	}
	if green >= 128 {
		index |= 2
	}
	if blue >= 128 {
		index |= 4
	}
	if max(red, max(green, blue)) >= 192 {
		index |= 8
	}
	return index
}

func indexed16(index uint8) uint8 {
	if index < 16 {
		return index
	}
	if index >= 232 {
		gray := uint8(8 + (int(index)-232)*10)
		return rgb16(gray, gray, gray)
	}
	index -= 16
	return rgb16(uint8(index/36)*51, uint8(index/6%6)*51, uint8(index%6)*51)
}

func appendPlain(dst []byte, frame *render.Buffer) []byte {
	width, height := frame.Size()
	for y := 0; y < height; y++ {
		end := width
		for end > 0 {
			cell := frame.Cell(end-1, y)
			if cell.Width == 0 {
				end--
				continue
			}
			if cell.Grapheme != "" && cell.Grapheme != " " {
				break
			}
			end--
		}
		for x := 0; x < end; {
			cell := frame.Cell(x, y)
			if cell.Width == 0 {
				x++
				continue
			}
			if cell.Grapheme == "" {
				dst = append(dst, ' ')
			} else {
				dst = append(dst, cell.Grapheme...)
			}
			x += max(int(cell.Width), 1)
		}
		dst = append(dst, '\n')
	}
	return dst
}
