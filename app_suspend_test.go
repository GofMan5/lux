package lux

import (
	"context"
	"testing"
	"time"

	"github.com/lux-tui/lux/event"
)

// waitForFrames polls the model's frame counter until it reaches want or
// the hang timeout trips. The run loop renders on its own tick, so the
// repaint an event dirties is observed one tick later, never inline.
func waitForFrames(t *testing.T, model *testModel, want int) {
	t.Helper()
	deadline := time.Now().Add(hangTimeout)
	for {
		model.mu.Lock()
		frames := model.frames
		model.mu.Unlock()
		if frames >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d frames rendered, want at least %d", frames, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func seenKinds(model *testModel) []event.Kind {
	model.mu.Lock()
	defer model.mu.Unlock()
	return append([]event.Kind(nil), model.updates...)
}

func containsKind(kinds []event.Kind, kind event.Kind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

// TestRunDeliversSuspendAndResumeAndRepaints pins the run-loop wiring: the
// lifecycle kinds ride the ordinary update path — an unknown-kind model
// written before suspension existed sees them as plain events — and each one
// dirties the frame, so a suspended model that pauses its animations stops
// painting and a resumed one repaints without any app-side special case.
func TestRunDeliversSuspendAndResumeAndRepaints(t *testing.T) {
	backend := newFakeBackend(20, 5)
	model := &testModel{quitOn: event.Resume}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, WithBackend(backend), WithMaxFPS(240)) }()
	waitForFrames(t, model, 1)
	model.mu.Lock()
	baseline := model.frames
	model.mu.Unlock()

	backend.events <- event.Event{Kind: event.Suspend}
	// The suspend notice must reach Update and dirty the frame: the model
	// gets its chance to pause before the process stops.
	waitForFrames(t, model, baseline+1)
	if !containsKind(seenKinds(model), event.Suspend) {
		t.Fatalf("suspend never reached the model: %v", seenKinds(model))
	}

	backend.events <- event.Event{Kind: event.Resume}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("resume quit timeout")
	}
	// The model quit on the resume itself, so the post-resume repaint may
	// or may not have made it to a tick — what is pinned is the delivery:
	// the model saw the resume it decided to quit on.
	if !containsKind(seenKinds(model), event.Resume) {
		t.Fatalf("resume never reached the model: %v", seenKinds(model))
	}
	if !backend.closed {
		t.Fatal("the run did not close the backend")
	}
}
