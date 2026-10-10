package terminal

import (
	"context"
	"os"
	"testing"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/render"
)

// These guards turn the encoder's allocation discipline from a benchmark
// observation into a pull-request gate: every path below is measured at
// zero allocations per call, and any regression — an accidental
// fmt.Sprintf in the diff, a closure capture in the idle scan, a map in
// the style bookkeeping — fails the suite instead of quietly taxing
// every frame of every application at 60 FPS.

// TestAppendFrameWithReusedScratchAllocatesNothing prices the full diff
// encode of a styled 120x40 frame — paint, erase, style deltas, caret —
// with the caller-provided scratch reused across calls, exactly the way
// the writer goroutine reuses its buffer between presents.
func TestAppendFrameWithReusedScratchAllocatesNothing(t *testing.T) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	previous := frame.Clone()
	paintSample(frame, 1)
	scratch := make([]byte, 0, 8192)
	size := 0
	allocs := testing.AllocsPerRun(100, func() {
		scratch = appendFrame(scratch[:0], frame, previous, capability.TrueColor, true, false, false)
		size = len(scratch)
	})
	if size == 0 {
		t.Fatal("encode produced no bytes; the guard is not exercising the diff path")
	}
	if allocs != 0 {
		t.Fatalf("appendFrame allocated %v times per call; the frame encoder must stay allocation-free", allocs)
	}
}

// TestPresentOnUnchangedFrameAllocatesNothing prices the complete idle
// Present: write-slot semaphore, feature snapshot, dirty-row scan, and
// the zero-byte early exit. This is the per-frame floor every idle
// application pays at 60 FPS, so it must stay at zero.
func TestPresentOnUnchangedFrameAllocatesNothing(t *testing.T) {
	frame := render.NewBuffer(120, 40)
	paintSample(frame, 0)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open dev null: %v", err)
	}
	defer func() { _ = devNull.Close() }()
	backend := &Backend{
		writer:   devNull,
		opened:   true,
		features: capability.Set{Color: capability.TrueColor, ScrollRegions: true},
		previous: frame.Clone(),
	}
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		if err := backend.Present(ctx, frame); err != nil {
			t.Errorf("present idle frame: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("idle Present allocated %v times per call; the idle frame floor must stay allocation-free", allocs)
	}
}
