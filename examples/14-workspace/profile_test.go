package main

import (
	"strings"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/phaseprofile"
)

// BenchmarkPreparation120x40 browses, edits and previews a config file, one
// event per presented frame.
func BenchmarkPreparation120x40(b *testing.B) {
	config := "name = demo\nworkers = 2\n" + strings.Repeat("# tuning note\n", 60)
	var script []event.Event
	for range 3 {
		script = append(script, event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyUp, 0), event.KeyPress(event.KeyTab, 0))
		for _, r := range "retry = 3" {
			script = append(script, event.RuneKey(r, 0))
		}
		script = append(script, event.KeyPress(event.KeyEnter, 0), event.RuneKey('p', event.ModCtrl), event.RuneKey('p', event.ModCtrl),
			event.KeyPress(event.KeyTab, 0))
	}
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model {
		return newWorkspaceApp(&scriptedWorkspace{
			files:   []workspaceFile{{Name: "config.lux"}, {Name: "notes.txt"}},
			content: map[string]string{"config.lux": config, "notes.txt": "hello\n"},
		})
	}, script)
}
