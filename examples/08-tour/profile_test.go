package main

import (
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/phaseprofile"
)

// BenchmarkPreparation120x40 walks every tour page, its focus order, and the
// settings and inspection overlays, one event per presented frame.
func BenchmarkPreparation120x40(b *testing.B) {
	var script []event.Event
	for page := '1'; page <= '9'; page++ {
		script = append(script, event.RuneKey(page, 0),
			event.KeyPress(event.KeyTab, 0), event.KeyPress(event.KeyDown, 0),
			event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyRight, 0))
	}
	script = append(script, event.KeyPress(event.KeyF3, 0), event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyF3, 0),
		event.KeyPress(event.KeyF4, 0), event.KeyPress(event.KeyF4, 0))
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newTour() }, script)
}
