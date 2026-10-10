package render

import "testing"

// These guards turn the renderer's allocation discipline from a benchmark
// observation into a pull-request gate: every path below is measured at
// zero allocations per call, and any regression fails the suite instead
// of quietly taxing every frame of every application.

// TestCanvasPaintAllocatesNothing prices a full canvas repaint of a
// 120x40 frame — clear, border, and a mixed Latin/CJK/emoji line on
// every interior row — because that is the shape a typical model paints
// per frame. Runes, styles, and wide-cell padding must all flow through
// the preallocated cells without a single heap escape.
func TestCanvasPaintAllocatesNothing(t *testing.T) {
	buffer := NewBuffer(120, 40)
	canvas := NewCanvas(buffer)
	style := Style{Foreground: RGBColor(220, 225, 235), Background: RGBColor(20, 24, 32)}
	line := "Lux TUI · smooth 界面 · 60 FPS · 👨‍👩‍👧‍👦"
	allocs := testing.AllocsPerRun(20, func() {
		canvas.Clear(style)
		canvas.Border(canvas.Bounds(), RoundedBorder, style)
		for y := 1; y < 39; y++ {
			canvas.Text(2, y, line, style)
		}
	})
	if allocs != 0 {
		t.Fatalf("canvas repaint allocated %v times per call; painting must stay allocation-free", allocs)
	}
}

// TestBufferAppendDiffNoChangesAllocatesNothing prices the idle-diff
// sweep that proves an unchanged frame unchanged: AppendDiff walks the
// full 160x50 grid, reuses the caller's change slice, and must not
// allocate — this runs once per presented frame in the writer.
func TestBufferAppendDiffNoChangesAllocatesNothing(t *testing.T) {
	front := NewBuffer(160, 50)
	back := front.Clone()
	changes := make([]Change, 0, 32)
	count := 0
	allocs := testing.AllocsPerRun(100, func() {
		changes = back.AppendDiff(changes[:0], front)
		count = len(changes)
	})
	if count != 0 {
		t.Fatalf("no-change diff reported %d changes", count)
	}
	if allocs != 0 {
		t.Fatalf("AppendDiff allocated %v times per call; the idle sweep must stay allocation-free", allocs)
	}
}
