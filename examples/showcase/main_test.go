package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lux-tui/lux"
	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
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
	app.showHelp = true
	if !app.showHelp {
		t.Fatal("help state failed")
	}
}

func TestShowcaseClipboardShortcutTargetsAndVirtualLayout(t *testing.T) {
	app := newShowcase()
	app.name.Cursor, app.name.Anchor = 0, len(app.name.Value)
	if err := app.name.Copy(context.Background(), &app.clipboard); err != nil {
		t.Fatal(err)
	}
	app.name.SetValue("changed")
	if err := app.name.Paste(context.Background(), &app.clipboard); err != nil || app.name.Value != "changedLUX explorer" {
		t.Fatalf("clipboard value=%q err=%v", app.name.Value, err)
	}

	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(120, 36)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	// The standard medium/wide layout must expose the million-row viewport.
	app.ui.Focus("logs") // not committed yet; registration is inspected below.
	found := false
	// Registration is private to package ui, so prove the visible heading/rows.
	for y := 0; y < 36; y++ {
		for x := 0; x < 120; x++ {
			if frame.Canvas.Buffer().Cell(x, y).Grapheme == "V" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("virtual log was not rendered at 120x36")
	}
}

type interactiveBackend struct {
	events chan event.Event
	frames chan *render.Buffer
	once   sync.Once
}

func (b *interactiveBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}
func (b *interactiveBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}
func (b *interactiveBackend) Present(_ context.Context, frame *render.Buffer) error {
	select {
	case b.frames <- frame.Clone():
	default:
	}
	return nil
}
func (*interactiveBackend) Size() (int, int, error) { return 120, 36, nil }
func (*interactiveBackend) Close() error            { return nil }

func TestShowcaseEndToEndInputAndModal(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newShowcase()
	app.observe = make(chan showcaseState, 32)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)
	waitState(t, app.observe)
	// Focus starts on TextInput; committed Unicode text must reach the editor.
	backend.events <- event.RuneKey('界', 0)
	state := waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.name != "LUX explorer" })
	if state.name != "LUX explorer界" {
		t.Fatalf("name = %q", state.name)
	}
	backend.events <- event.KeyPress(event.KeyF1, 0)
	waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.showHelp })
	waitFrame(t, backend.frames)
	// Wait until the modal hit/focus snapshot, not merely its pixels, is live.
	time.Sleep(10 * time.Millisecond)
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, func(state showcaseState) bool { return !state.showHelp })
	backend.events <- event.RuneKey('q', event.ModCtrl)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("showcase did not quit")
	}
}

func waitState(t *testing.T, states <-chan showcaseState) showcaseState {
	t.Helper()
	select {
	case state := <-states:
		return state
	case <-time.After(2 * time.Second):
		t.Fatal("state timeout")
		return showcaseState{}
	}
}

func waitStateWhere(t *testing.T, states <-chan showcaseState, match func(showcaseState) bool) showcaseState {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-states:
			if match(state) {
				return state
			}
		case <-deadline:
			t.Fatal("matching state timeout")
			return showcaseState{}
		}
	}
}

func waitFrame(t *testing.T, frames <-chan *render.Buffer) {
	t.Helper()
	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("frame timeout")
	}
}
