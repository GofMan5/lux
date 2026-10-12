package main

import (
	"context"
	"testing"
	"time"

	"github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

func TestCompactHiddenSpinnerSettlesAndResumes(t *testing.T) {
	backend := newProbeBackend(80, 24)
	app := newTour()
	app.page, app.tabs.Selected = 6, 6
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lux.Run(ctx, app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(hangTimeout):
			t.Error("tour did not stop")
		}
	})
	if backend.lastFrame(2*time.Second) == nil {
		t.Fatal("no initial frame")
	}
	if frames := backend.countFrames(300 * time.Millisecond); frames != 0 {
		t.Fatalf("hidden spinner presented %d recurring frames, want 0", frames)
	}
	for range 17 {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 3, Action: event.MouseWheel, Button: event.MouseWheelDown}}
	}
	if frame := backend.lastFrame(400 * time.Millisecond); frame == nil || !bufferContains(frame, "spinner, checkbox slows it") {
		t.Fatal("scrolling did not reveal the spinner")
	}
	if frames := backend.countFrames(400 * time.Millisecond); frames == 0 {
		t.Fatal("revealed spinner did not resume")
	}
	for range 17 {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 3, Action: event.MouseWheel, Button: event.MouseWheelUp}}
	}
	if frame := backend.lastFrame(400 * time.Millisecond); frame == nil || !bufferContains(frame, "slow spinner") {
		t.Fatal("scrolling did not return to form")
	}
	if frames := backend.countFrames(300 * time.Millisecond); frames != 0 {
		t.Fatalf("spinner still presented %d frames after scrolling out, want 0", frames)
	}
}

func TestWidgetsOnlyScheduleVisibleMotion(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		name          string
		w, h, offset  int
		gaugeMoving   bool
		spinner       bool
		wantMoving    bool
		wantImmediate bool
	}{
		{"compact form hides all motion", 80, 24, 0, true, true, false, false},
		{"gauge visible spinner hidden", 80, 24, 20, true, true, true, true},
		{"only spinner visible", 80, 24, 46, true, true, true, false},
		{"hidden gauge stopped spinner", 80, 24, 46, true, false, false, false},
		{"wide displays motion", 120, 40, 0, true, true, true, true},
		{"tiny hides everything", 10, 4, 0, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTour()
			app.page, app.tabs.Selected, app.wscroll.Offset = 6, 6, tc.offset
			app.sync.Started = time.Time{}
			if tc.spinner {
				app.sync.Started = now.Add(-205 * time.Millisecond)
			}
			if tc.gaugeMoving {
				app.target = .9
			}
			app.View(&lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(tc.w, tc.h)), Now: now, Capabilities: capability.Set{Interactive: true}})
			delay, moving := app.wantsFrame(now)
			if moving != tc.wantMoving || moving && (delay == 0) != tc.wantImmediate {
				t.Fatalf("wantsFrame = (%v,%v), want moving=%v immediate=%v", delay, moving, tc.wantMoving, tc.wantImmediate)
			}
			app.reduced = true
			if _, moving := app.wantsFrame(now); moving {
				t.Fatal("reduced motion must freeze visible and hidden decoration")
			}
			app.progressMode = 1
			if _, moving := app.wantsFrame(now); !moving {
				t.Fatal("work progress must remain independent of decoration visibility")
			}
		})
	}
}
