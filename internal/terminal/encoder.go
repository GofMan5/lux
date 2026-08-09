package terminal

import (
	"strconv"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

const resetStyle = "\x1b[0m"

func appendFrame(dst []byte, frame, previous *render.Buffer, depth capability.ColorDepth, synchronized bool) []byte {
	width, height := frame.Size()
	previousWidth, previousHeight := previous.Size()
	full := previous == nil || width != previousWidth || height != previousHeight
	start := len(dst)
	wrote := false
	var current render.Style
	haveStyle := false
	for y := 0; y < height; y++ {
		for x := 0; x < width; {
			cell := frame.Cell(x, y)
			if cell.Width == 0 || (!full && equalRenderedCell(frame, previous, x, y, depth)) {
				x++
				continue
			}
			runStart := x
			for x < width {
				cell = frame.Cell(x, y)
				if cell.Width == 0 {
					x++
					continue
				}
				if !full && equalRenderedCell(frame, previous, x, y, depth) {
					break
				}
				x += max(int(cell.Width), 1)
			}
			dst = appendCursor(dst, y+1, runStart+1)
			wrote = true
			for column := runStart; column < x; {
				cell = frame.Cell(column, y)
				if cell.Width == 0 {
					column++
					continue
				}
				if !haveStyle || current != cell.Style {
					dst = appendStyle(dst, cell.Style, depth)
					current, haveStyle = cell.Style, true
				}
				grapheme := cell.Grapheme
				if grapheme == "" {
					grapheme = " "
				}
				dst = append(dst, grapheme...)
				column += max(int(cell.Width), 1)
			}
		}
	}
	if !wrote {
		return dst[:start]
	}
	dst = append(dst, resetStyle...)
	if synchronized {
		payload := append([]byte("\x1b[?2026h"), dst[start:]...)
		payload = append(payload, "\x1b[?2026l"...)
		dst = append(dst[:start], payload...)
	}
	return dst
}

func equalRenderedCell(frame, previous *render.Buffer, x, y int, depth capability.ColorDepth) bool {
	current, currentOK := frame.CellOK(x, y)
	old, oldOK := previous.CellOK(x, y)
	if currentOK != oldOK || !currentOK || current.Grapheme != old.Grapheme || current.Width != old.Width || current.Style.Attrs != old.Style.Attrs {
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

func appendStyle(dst []byte, style render.Style, depth capability.ColorDepth) []byte {
	dst = append(dst, '\x1b', '[', '0')
	attrs := [...]struct {
		mask render.Attr
		code string
	}{
		{render.AttrBold, "1"}, {render.AttrDim, "2"}, {render.AttrItalic, "3"},
		{render.AttrUnderline, "4"}, {render.AttrBlink, "5"}, {render.AttrReverse, "7"},
		{render.AttrHidden, "8"}, {render.AttrStrike, "9"},
	}
	for _, attribute := range attrs {
		if style.Attrs&attribute.mask != 0 {
			dst = append(dst, ';')
			dst = append(dst, attribute.code...)
		}
	}
	dst = appendColor(dst, style.Foreground, depth, true)
	dst = appendColor(dst, style.Background, depth, false)
	return append(dst, 'm')
}

func appendColor(dst []byte, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
	if color.Mode == render.ColorDefault || depth == capability.Monochrome {
		return dst
	}
	index := uint8(color.Value)
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			dst = append(dst, ';')
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
		dst = append(dst, ';')
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
	dst = append(dst, ';')
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
