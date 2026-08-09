package main

import (
	"testing"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/ui"
)

func TestShowcaseWidgetStateAndShortcuts(t *testing.T) {
	app := newShowcase()
	app.name.Apply(ui.Action{Kind: ui.Input, Event: event.RuneKey('!', 0)})
	if app.name.Value != "LUX explorer!" {
		t.Fatalf("name = %q", app.name.Value)
	}
	changed, _ := app.scenes.Apply(ui.Action{Kind: ui.Scroll, Event: event.KeyPress(event.KeyDown, 0)}, len(sceneNames), 3)
	if !changed || app.scenes.Selected != 1 {
		t.Fatalf("scene = %#v", app.scenes)
	}
	if command := app.Update(event.KeyPress(event.KeyF1, 0)); command != nil || !app.showHelp {
		t.Fatal("F1 did not open help")
	}
	if command := app.Update(event.RuneKey('Q', event.ModCtrl)); command == nil {
		t.Fatal("Ctrl+Q did not request quit")
	}
}
