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
	rows := make([]processRow, 500)
	for i := range rows {
		rows[i] = processRow{pid: 1000 + i, name: fmt.Sprintf("worker-%03d", i), cpu: float64(i % 97), mem: int64(i) << 20, state: "running"}
	}
	var script []event.Event
	for range 3 {
		for range 6 {
			script = append(script, event.KeyPress(event.KeyDown, 0))
		}
		script = append(script, event.RuneKey('w', 0), event.RuneKey('1', 0), event.KeyPress(event.KeyBackspace, 0),
			event.KeyPress(event.KeyBackspace, 0), event.KeyPress(event.KeyPageDown, 0), event.KeyPress(event.KeyUp, 0))
	}
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model {
		return newProcsApp(newScriptedSource(scriptStep{rows: rows}), nil, nil)
	}, script)
}
