package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lux-tui/lux"
	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
)

// hangTimeout only bounds a test that would otherwise wait forever. Making it
// generous costs a passing run nothing and stops a loaded machine from
// reporting a stall as a logic failure.
const hangTimeout = 10 * time.Second

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
	// The standard medium/wide layout must expose the million-row viewport and
	// the live Table demo, both of which live in the scrolling panel.
	painted := frame.Canvas.Buffer()
	for _, want := range []string{"Virtual log (1M)", "WIDGET TABLE", "TASK"} {
		found := false
		for y := 0; y < 36 && !found; y++ {
			found = strings.Contains(rowText(painted, y), want)
		}
		if !found {
			t.Fatalf("%q was not rendered at 120x36", want)
		}
	}
	// The widget table pushed telemetry below the fold; scrolling the panel
	// document to its end must reveal it without dropping any control.
	app.controls.Offset = controlsContent
	app.View(frame)
	painted = frame.Canvas.Buffer()
	for _, want := range []string{"FRAME TELEMETRY", "renderer"} {
		found := false
		for y := 0; y < 36 && !found; y++ {
			found = strings.Contains(rowText(painted, y), want)
		}
		if !found {
			t.Fatalf("%q was not rendered after scrolling the panel", want)
		}
	}
}

type interactiveBackend struct {
	events chan event.Event
	frames chan *render.Buffer
	width  int
	height int
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
func (b *interactiveBackend) Size() (int, int, error) {
	if b.width == 0 || b.height == 0 {
		return 120, 36, nil
	}
	return b.width, b.height, nil
}
func (*interactiveBackend) Close() error { return nil }

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
	// Wait until the modal hit/focus snapshot, not merely its pixels, is live.
	settle(t, backend.frames)
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, func(state showcaseState) bool { return !state.showHelp })
	backend.events <- event.RuneKey('q', event.ModCtrl)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("showcase did not quit")
	}
}

func TestShowcaseTabRevealsScrolledOutControl(t *testing.T) {
	// The panel is taller than its viewport at this size, so the action row and
	// telemetry start out of view.
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32), width: 120, height: 24}
	app := newShowcase()
	app.observe = make(chan showcaseState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	defer func() {
		backend.events <- event.RuneKey('q', event.ModCtrl)
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(hangTimeout):
			t.Error("showcase did not quit")
		}
	}()
	waitFrame(t, backend.frames)
	settle(t, backend.frames)

	// Walk focus past the visible controls; the panel must follow rather than
	// leave focus somewhere invisible.
	for range 4 {
		backend.events <- event.KeyPress(event.KeyTab, 0)
		settle(t, backend.frames)
	}
	revealed := waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.controls > 0 })
	if revealed.logs != 0 {
		t.Fatalf("revealing a control scrolled the nested log: %#v", revealed)
	}
}

func TestShowcaseFrameCarriesEditorCaret(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newShowcase()
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	deadline := time.After(hangTimeout)
	for {
		select {
		case frame := <-backend.frames:
			x, y, visible := frame.Caret()
			if !visible {
				continue
			}
			// The editor owns the terminal cursor: panel column plus the caret's
			// cell offset inside "LUX explorer", on the field's row.
			if x != 3+12 || y != controlsBodyTop+1 {
				t.Fatalf("caret = (%d,%d), want (%d,%d)", x, y, 15, controlsBodyTop+1)
			}
			backend.events <- event.RuneKey('q', event.ModCtrl)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(hangTimeout):
				t.Fatal("showcase did not quit")
			}
			return
		case <-deadline:
			t.Fatal("no frame requested the terminal caret")
		}
	}
}

func TestShowcaseNestedWheelBubblesToControlsPanel(t *testing.T) {
	// A short terminal makes the panel taller than its viewport, which is the
	// only case where nesting is observable.
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32), width: 120, height: 24}
	app := newShowcase()
	app.observe = make(chan showcaseState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	defer func() {
		backend.events <- event.RuneKey('q', event.ModCtrl)
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(hangTimeout):
			t.Error("showcase did not quit")
		}
	}()
	waitFrame(t, backend.frames)
	// The hit snapshot commits on the presented frame, so wait for it.
	settle(t, backend.frames)

	// Scrolling over the panel body moves the panel itself.
	backend.events <- wheelAt(5, 8, false)
	scrolled := waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.controls > 0 })
	painted := settle(t, backend.frames)

	logRow := controlsBodyTop + 12 - scrolled.controls
	if text := rowText(painted, logRow); !strings.Contains(text, "event ready") {
		t.Fatalf("row %d is not the nested log: %q", logRow, text)
	}
	// The nested log can still scroll down, so it keeps the wheel.
	backend.events <- wheelAt(5, logRow, false)
	inner := waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.logs > 0 })
	if inner.controls != scrolled.controls {
		t.Fatalf("panel moved while the log scrolled: %#v -> %#v", scrolled, inner)
	}
	settle(t, backend.frames)

	// At the log's top boundary the same wheel reaches the enclosing panel.
	backend.events <- wheelAt(5, logRow, true)
	waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.logs == 0 })
	settle(t, backend.frames)
	backend.events <- wheelAt(5, logRow, true)
	bubbled := waitStateWhere(t, app.observe, func(state showcaseState) bool { return state.controls < inner.controls })
	if bubbled.logs != 0 {
		t.Fatalf("log moved past its boundary: %#v", bubbled)
	}
}

// settle waits until the interaction snapshot of a freshly painted frame is
// committed. ui.Context commits through Frame.OnPresented, which the runtime
// runs immediately after a successful Present, so a following frame proves the
// previous commit already ran. It returns the last frame it observed.
func settle(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	for {
		select {
		case <-frames:
		default:
			waitFrame(t, frames)
			return waitFrame(t, frames)
		}
	}
}

func rowText(buffer *render.Buffer, y int) string {
	width, _ := buffer.Size()
	var row strings.Builder
	for x := 0; x < width; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

// controlsBodyTop is the first scrollable row of the controls panel: three header
// rows, one panel border row, and one heading row plus its blank separator.
const controlsBodyTop = 6

func wheelAt(x, y int, up bool) event.Event {
	button := event.MouseWheelDown
	if up {
		button = event.MouseWheelUp
	}
	return event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: x, Y: y, Action: event.MouseWheel, Button: button}}
}

func waitState(t *testing.T, states <-chan showcaseState) showcaseState {
	t.Helper()
	select {
	case state := <-states:
		return state
	case <-time.After(hangTimeout):
		t.Fatal("state timeout")
		return showcaseState{}
	}
}

func waitStateWhere(t *testing.T, states <-chan showcaseState, match func(showcaseState) bool) showcaseState {
	t.Helper()
	deadline := time.After(hangTimeout)
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

func waitFrame(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		t.Fatal("frame timeout")
		return nil
	}
}
