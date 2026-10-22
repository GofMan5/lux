package main

import (
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/phaseprofile"
)

// BenchmarkPreparation120x40 tours every page, opens the palette and help,
// and toggles the deploy, one event per presented frame.
func BenchmarkPreparation120x40(b *testing.B) {
	var script []event.Event
	for range 3 {
		script = append(script, studioCycle()...)
	}
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newStudio() }, script)
}

func studioCycle() []event.Event {
	return []event.Event{
		event.RuneKey('1', 0), event.RuneKey('d', 0), event.KeyPress(event.KeyTab, 0),
		// Tab lands in the filter: type, erase, Esc back to the table.
		event.RuneKey('2', 0), event.KeyPress(event.KeyTab, 0), event.RuneKey('x', 0),
		event.KeyPress(event.KeyBackspace, 0), event.KeyPress(event.KeyEscape, 0), event.KeyPress(event.KeyDown, 0),
		event.RuneKey('3', 0), event.KeyPress(event.KeyTab, 0), event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyEscape, 0),
		event.RuneKey('4', 0), event.RuneKey(' ', 0), event.RuneKey(' ', 0),
		event.RuneKey('5', 0), event.RuneKey('t', 0),
		event.RuneKey('p', event.ModCtrl), event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyEscape, 0),
		event.KeyPress(event.KeyF1, 0), event.KeyPress(event.KeyEscape, 0),
		event.RuneKey('1', 0), event.RuneKey('d', 0),
	}
}

// TestLongSessionMemoryIsBounded tours the app for 150 cycles with resizes;
// the capped activity, event and series histories must keep the heap flat.
// It starts at the largest size so all three runtime surfaces are allocated
// at full size up front: the free stack is LIFO, so a surface that rarely
// draws would otherwise grow to 160x50 whenever timing first hands it that
// size, a bounded one-time step that can land after the warmup.
func TestLongSessionMemoryIsBounded(t *testing.T) {
	cycle := append(studioCycle(), event.ResizeTo(100, 30), event.ResizeTo(120, 40), event.ResizeTo(160, 50))
	phaseprofile.CheckBounded(t, 160, 50, newStudio(), cycle, 150, 20, 128<<10)
}
