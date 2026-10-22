package main

import (
	"fmt"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/phaseprofile"
)

// BenchmarkPreparation120x40 filters, scrolls and clears a 500-process table,
// one event per presented frame.
func BenchmarkPreparation120x40(b *testing.B) {
	var script []event.Event
	for range 3 {
		script = append(script, procsCycle()...)
	}
	phaseprofile.Benchmark(b, 120, 40, newProfileProcs, script)
}

func newProfileProcs() lux.Model {
	rows := make([]processRow, 500)
	for i := range rows {
		rows[i] = processRow{pid: 1000 + i, name: fmt.Sprintf("worker-%03d", i), cpu: float64(i % 97), mem: int64(i) << 20, state: "running"}
	}
	return newProcsApp(newScriptedSource(scriptStep{rows: rows}), nil, nil)
}

func procsCycle() []event.Event {
	var cycle []event.Event
	for range 6 {
		cycle = append(cycle, event.KeyPress(event.KeyDown, 0))
	}
	return append(cycle, event.RuneKey('w', 0), event.RuneKey('1', 0), event.KeyPress(event.KeyBackspace, 0),
		event.KeyPress(event.KeyBackspace, 0), event.KeyPress(event.KeyPageDown, 0), event.KeyPress(event.KeyUp, 0))
}

// TestLongSessionMemoryIsBounded filters, scrolls and resizes the 500-row
// table for 150 cycles; filtering must not accumulate per-query state.
func TestLongSessionMemoryIsBounded(t *testing.T) {
	cycle := append(procsCycle(),
		event.ResizeTo(100, 30), event.ResizeTo(160, 50), event.ResizeTo(120, 40))
	phaseprofile.CheckBounded(t, 120, 40, newProfileProcs(), cycle, 150, 20, 128<<10)
}
