package main

import (
	"testing"

	"github.com/GofMan5/lux/event"
)

func TestWorkbenchInspectionReturnsToEditor(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	for range 3 {
		frame = catalogKey(t, backend, app, frame, event.KeyTab)
	}
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Text, Text: "inspection draft "})
	draft := frame.text
	frame = catalogKey(t, backend, app, frame, event.KeyF4)
	for _, text := range []string{"Workbench inspection", "SIMULATED capability profiles", "Actual capabilities:", "Source focus: catalog-editor", "target=catalog-editor", "Target bounds:", "Last Present:", "not preparation-only"} {
		if !bufferContains(frame.frame, text) {
			t.Fatalf("compact inspection lost %q", text)
		}
	}
	if _, _, visible := frame.frame.Caret(); visible {
		t.Fatal("inspection exposed the hidden editor caret")
	}
	if frame.scheduled {
		t.Fatal("inspection scheduled hidden decoration")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEscape)
	if frame.text != draft || frame.focus != "catalog-editor" || !bufferContains(frame.frame, "Editing widgets") {
		t.Fatal("inspection lost source stand/focus/draft")
	}
}
