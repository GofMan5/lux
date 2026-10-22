package main

import (
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/phaseprofile"
)

// BenchmarkPreparation120x40 runs, scrolls, follows, cancels and retries jobs
// across the rows, one event per presented frame.
func BenchmarkPreparation120x40(b *testing.B) {
	var script []event.Event
	for range 3 {
		script = append(script, jobsCycle()...)
	}
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newJobsApp(demoSource{}) }, script)
}

func jobsCycle() []event.Event {
	return []event.Event{event.RuneKey(' ', 0), event.RuneKey('l', 0), event.RuneKey('l', 0),
		event.RuneKey('L', 0), event.RuneKey('f', 0), event.RuneKey('x', 0), event.RuneKey('r', 0),
		event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyTab, 0)}
}

// TestLongSessionMemoryIsBounded runs, follows, cancels and retries jobs and
// resizes for 150 cycles; the capped log history must keep the heap flat.
func TestLongSessionMemoryIsBounded(t *testing.T) {
	cycle := append(jobsCycle(),
		event.ResizeTo(100, 30), event.ResizeTo(160, 50), event.ResizeTo(120, 40))
	phaseprofile.CheckBounded(t, 120, 40, newJobsApp(demoSource{}), cycle, 150, 20, 128<<10)
}
