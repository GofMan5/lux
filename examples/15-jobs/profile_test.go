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
		script = append(script, event.RuneKey(' ', 0), event.RuneKey('l', 0), event.RuneKey('l', 0),
			event.RuneKey('L', 0), event.RuneKey('f', 0), event.RuneKey('x', 0), event.RuneKey('r', 0),
			event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyTab, 0))
	}
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newJobsApp(demoSource{}) }, script)
}
