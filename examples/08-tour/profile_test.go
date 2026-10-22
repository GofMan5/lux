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
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newTour() }, tourCycle())
}

func tourCycle() []event.Event {
	var cycle []event.Event
	for page := '1'; page <= '9'; page++ {
		cycle = append(cycle, event.RuneKey(page, 0),
			event.KeyPress(event.KeyTab, 0), event.KeyPress(event.KeyDown, 0),
			event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyRight, 0))
	}
	return append(cycle, event.KeyPress(event.KeyF3, 0), event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyF3, 0),
		event.KeyPress(event.KeyF4, 0), event.KeyPress(event.KeyF4, 0))
}

// TestLongSessionMemoryIsBounded tours every page, overlays and resizes for
// 60 cycles; page caches and region state must not accumulate.
func TestLongSessionMemoryIsBounded(t *testing.T) {
	cycle := append(tourCycle(), event.ResizeTo(100, 30), event.ResizeTo(160, 50), event.ResizeTo(120, 40))
	phaseprofile.CheckBounded(t, 120, 40, newTour(), cycle, 60, 10, 128<<10)
}
