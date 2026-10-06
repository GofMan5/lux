// Scratch A/B harness used to gather the stage-5 idle-scan numbers in
// verification-stage5.txt. NOT part of the shipped package: copy this file
// into internal/terminal as zz_ab_test.go to reproduce the measurements
// (go test ./internal/terminal -run '^$' -bench 'BenchmarkAB|BenchmarkEncodeFrame$|BenchmarkScanUnchanged|BenchmarkScrollFrame' -benchtime=1s -count=3).
// appendFrameLegacy is the pre-stage-5 encoder kept as a stage-4 stand-in;
// it is byte-identical across trees (264/0/1508 bytes/frame), which is what
// makes the live-tree vs d529d99-worktree runs comparable.

package terminal

import (
	"strconv"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

func appendFrameLegacy(dst []byte, frame, previous *render.Buffer, depth capability.ColorDepth, synchronized bool) []byte {
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
					dst = appendStyleLegacy(dst, cell.Style, depth)
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
	caretX, caretY, caretOn := frame.Caret()
	oldX, oldY, caretWasOn := previous.Caret()
	caretMoved := caretOn != caretWasOn || caretOn && (caretX != oldX || caretY != oldY)
	if !wrote && !caretMoved {
		return dst[:start]
	}
	if wrote {
		dst = append(dst, resetStyle...)
	}
	switch {
	case caretOn:
		// Cell runs left the cursor wherever the last run ended, so the caret is
		// repositioned whenever anything was written, not only when it moved.
		dst = appendCursor(dst, caretY+1, caretX+1)
		if !caretWasOn {
			dst = append(dst, showCaret...)
		}
	case caretWasOn:
		dst = append(dst, hideCaret...)
	}
	if synchronized {
		payload := append([]byte("\x1b[?2026h"), dst[start:]...)
		payload = append(payload, "\x1b[?2026l"...)
		dst = append(dst[:start], payload...)
	}
	return dst
}
func appendStyleLegacy(dst []byte, style render.Style, depth capability.ColorDepth) []byte {
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
	dst = appendColorLegacy(dst, style.Foreground, depth, true)
	dst = appendColorLegacy(dst, style.Background, depth, false)
	return append(dst, 'm')
}
func appendColorLegacy(dst []byte, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
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

// benchFrames returns a frame and its predecessor: the same sample painted
// one variant apart, so only the top four rows differ.
func benchFrames(w, h int) (*render.Buffer, *render.Buffer) {
	frame := render.NewBuffer(w, h)
	paintSample(frame, 0)
	previous := frame.Clone()
	paintSample(frame, 1)
	return frame, previous
}

// A/B evidence for the stage-5 encoder work. appendFrameLegacy is a
// self-contained copy of the pre-stage-5 encoder (full SGR per run, one CUP
// per run, no erase, no scroll detection), so the old and new paths can be
// measured inside one binary.
//
// Fairness rules, matching how the encoders mutate state:
//   - diff and idle scenarios never scroll (paintSample only edits the top
//     four rows and blank-row candidates score zero), and neither encoder
//     mutates `previous` there, so both reuse one buffer cloned outside the
//     loop;
//   - the scroll scenario mutates `previous` whenever appendFrame scrolls, so
//     both encoders clone it per iteration and BenchmarkABClone prices the
//     clone, keeping the comparison symmetric and adjustable.
func BenchmarkABLegacyDiff(b *testing.B) {
	frame, previous := benchFrames(120, 40)
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrameLegacy(scratch[:0], frame, previous, capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

func BenchmarkABNewDiff(b *testing.B) {
	frame, previous := benchFrames(120, 40)
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrame(scratch[:0], frame, previous, capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

func BenchmarkABLegacyIdle(b *testing.B) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	previous := frame.Clone()
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrameLegacy(scratch[:0], frame, previous, capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

func BenchmarkABNewIdle(b *testing.B) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	previous := frame.Clone()
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrame(scratch[:0], frame, previous, capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

// benchScroll returns a frame one entry past base, the shape a scrolling list
// produces every frame.
func benchScroll(w, h int) (frame, base *render.Buffer) {
	base = render.NewBuffer(w, h)
	scrollingList(base, 0)
	frame = base.Clone()
	scrollingList(frame, 1)
	return frame, base
}

func BenchmarkABLegacyScroll(b *testing.B) {
	frame, base := benchScroll(120, 40)
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrameLegacy(scratch[:0], frame, base.Clone(), capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

func BenchmarkABNewScroll(b *testing.B) {
	frame, base := benchScroll(120, 40)
	scratch := make([]byte, 0, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		scratch = appendFrame(scratch[:0], frame, base.Clone(), capability.TrueColor, false)
	}
	b.ReportMetric(float64(len(scratch)), "bytes/frame")
}

var abCloneSink *render.Buffer

func BenchmarkABClone(b *testing.B) {
	base := render.NewBuffer(120, 40)
	scrollingList(base, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		abCloneSink = base.Clone()
	}
}
