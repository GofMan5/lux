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
	phaseprofile.Benchmark(b, 120, 40, func() lux.Model { return newProfileWorkspace(config) }, script)
}

func newProfileWorkspace(config string) lux.Model {
	return newWorkspaceApp(&scriptedWorkspace{
		files:   []workspaceFile{{Name: "config.lux"}, {Name: "notes.txt"}},
		content: map[string]string{"config.lux": config, "notes.txt": "hello\n"},
	})
}

// TestLongSessionMemoryIsBounded types and erases a line, opens and closes the
// palette and resizes for 150 cycles; undo history is capped, so once it fills
// the heap must stay flat.
func TestLongSessionMemoryIsBounded(t *testing.T) {
	cycle := []event.Event{event.KeyPress(event.KeyDown, 0), event.KeyPress(event.KeyUp, 0), event.KeyPress(event.KeyTab, 0)}
	for _, r := range "retry = 3" {
		cycle = append(cycle, event.RuneKey(r, 0))
	}
	for range len("retry = 3") {
		cycle = append(cycle, event.KeyPress(event.KeyBackspace, 0))
	}
	cycle = append(cycle, event.RuneKey('p', event.ModCtrl), event.RuneKey('p', event.ModCtrl), event.KeyPress(event.KeyTab, 0),
		event.ResizeTo(100, 30), event.ResizeTo(160, 50), event.ResizeTo(120, 40))
	config := "name = demo\nworkers = 2\n" + strings.Repeat("# tuning note\n", 60)
	phaseprofile.CheckBounded(t, 120, 40, newProfileWorkspace(config), cycle, 150, 40, 128<<10)
}
