package ui

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func presented(c *Context, regions []region, active modal) {
	c.mu.Lock()
	if c.generation == 0 {
		c.generation = 1
	}
	c.sizeKnown = true
	c.sequence++
	generation, sequence := c.generation, c.sequence
	c.mu.Unlock()
	next := snapshot{regions: regions, modal: active}
	for index := range next.regions {
		if next.regions[index].contentHeight == 0 {
			next.regions[index].contentHeight = -1
		}
		if next.regions[index].originX == 0 && next.regions[index].originY == 0 {
			next.regions[index].originX = next.regions[index].bounds.X
			next.regions[index].originY = next.regions[index].bounds.Y
		}
	}
	next.focus = firstFocus(next)
	c.commit(generation, sequence, next)
}

func TestReverseHitCaptureAndKeyboardFocus(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "back", bounds: render.Rect{W: 8, H: 3}, flags: Focusable | Activatable},
		{id: "front", bounds: render.Rect{X: 2, Y: 1, W: 5, H: 2}, flags: Focusable | Activatable},
		{id: "disabled", bounds: render.Rect{X: 10, Y: 1, W: 5, H: 2}, flags: Focusable | Activatable | Disabled},
	}, modal{})

	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 3, Y: 1, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled := ctx.Handle(press)
	if !handled || action.Kind != Focus || action.Target != "front" || action.X != 1 || action.Y != 0 || !ctx.Pressed("front") {
		t.Fatalf("press = %#v handled=%v pressed=%v", action, handled, ctx.Pressed("front"))
	}
	release := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 3, Y: 1, Action: event.MouseRelease, Button: event.MouseNone}}
	action, handled = ctx.Handle(release)
	if !handled || action.Kind != Activate || action.Target != "front" || ctx.Pressed("front") {
		t.Fatalf("release = %#v handled=%v", action, handled)
	}

	action, handled = ctx.Handle(event.KeyPress(event.KeyTab, 0))
	if !handled || action.Kind != Focus || action.Target != "back" || !ctx.Focused("back") {
		t.Fatalf("tab = %#v handled=%v", action, handled)
	}
	action, handled = ctx.Handle(event.RuneKey(' ', 0))
	if !handled || action.Kind != Activate || action.Target != "back" {
		t.Fatalf("space = %#v handled=%v", action, handled)
	}
	if action, handled = ctx.Handle(event.RuneKey(' ', event.ModCtrl)); handled || action.Kind != None {
		t.Fatalf("ctrl-space = %#v handled=%v", action, handled)
	}

	ctx.Handle(press)
	release.Mouse.X = 12
	if action, handled = ctx.Handle(release); !handled || action.Kind == Activate || ctx.Pressed("front") {
		t.Fatalf("outside release = %#v handled=%v", action, handled)
	}
}

func TestHoverMotionWithoutButtonConsumed(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "list", bounds: render.Rect{Y: 4, W: 20, H: 5}, flags: Focusable | Scrollable},
	}, modal{})
	hover := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 3, Y: 6, Action: event.MouseMove, Button: event.MouseNone}}
	if _, handled := ctx.Handle(hover); !handled {
		t.Fatal("hover motion over a region was not consumed")
	}
	if !ctx.Hovered("list") {
		t.Fatal("hover motion did not update hover")
	}
}

func TestInputScrollAndFocusLoss(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "input", bounds: render.Rect{X: 4, Y: 2, W: 12, H: 1}, flags: Focusable | Editable},
		{id: "list", bounds: render.Rect{Y: 4, W: 20, H: 5}, flags: Focusable | Scrollable},
	}, modal{})

	action, handled := ctx.Handle(event.RuneKey('界', 0))
	if !handled || action.Kind != Input || action.Target != "input" {
		t.Fatalf("input = %#v handled=%v", action, handled)
	}
	click := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 7, Y: 2, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled = ctx.Handle(click)
	if !handled || action.Kind != Input || action.Target != "input" || action.X != 3 || action.Y != 0 {
		t.Fatalf("input click = %#v handled=%v", action, handled)
	}
	ctx.Handle(event.KeyPress(event.KeyTab, 0))
	wheel := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 3, Y: 6, Action: event.MouseWheel, Button: event.MouseWheelDown}}
	action, handled = ctx.Handle(wheel)
	if !handled || action.Kind != Scroll || action.Target != "list" || action.X != 3 || action.Y != 2 {
		t.Fatalf("wheel = %#v handled=%v", action, handled)
	}
	listClick := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 4, Y: 7, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled = ctx.Handle(listClick)
	if !handled || action.Kind != Scroll || action.Target != "list" || action.X != 4 || action.Y != 3 {
		t.Fatalf("list click = %#v handled=%v", action, handled)
	}
	ctx.Handle(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 3, Y: 6, Action: event.MouseMove}})
	if !ctx.Hovered("list") {
		t.Fatal("list was not hovered")
	}
	ctx.Handle(event.Event{Kind: event.Focus, Focused: false})
	if ctx.Hovered("list") {
		t.Fatal("focus loss retained hover")
	}
}

func TestScrollableBlankTailDoesNotSelect(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{{id: "list", bounds: render.Rect{W: 10, H: 5}, flags: Focusable | Scrollable | Activatable, contentHeight: 2}}, modal{})
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 4, Action: event.MousePress, Button: event.MouseLeft}}
	if action, handled := ctx.Handle(press); !handled || action.Kind != None {
		t.Fatalf("blank tail = %#v handled=%v", action, handled)
	}
	wheel := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 4, Action: event.MouseWheel, Button: event.MouseWheelDown}}
	if action, handled := ctx.Handle(wheel); !handled || action.Kind != Scroll {
		t.Fatalf("blank tail wheel = %#v handled=%v", action, handled)
	}
}

func TestEmptyScrollableHasNoPressableRows(t *testing.T) {
	var ctx Context
	ctx.generation, ctx.sizeKnown = 1, true
	next := snapshot{regions: []region{{id: "empty", bounds: render.Rect{W: 10, H: 3}, flags: Focusable | Scrollable | Activatable, contentHeight: 0}}, focus: "empty"}
	ctx.commit(1, 1, next)
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 0, Action: event.MousePress, Button: event.MouseLeft}}
	if action, handled := ctx.Handle(press); !handled || action.Kind != None {
		t.Fatalf("empty list = %#v handled=%v", action, handled)
	}
}

func TestDuplicateIDPanicsDuringBuild(t *testing.T) {
	buffer := render.NewBuffer(10, 2)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	ctx.Begin(frame)
	ctx.Region("same", canvas, render.Rect{W: 2, H: 1}, Focusable)
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate ID did not panic")
		}
	}()
	ctx.Region("same", canvas, render.Rect{X: 3, W: 2, H: 1}, Focusable)
}

func TestReleaseInBlankTailDoesNotActivate(t *testing.T) {
	var ctx Context
	ctx.generation, ctx.sizeKnown = 1, true
	ctx.commit(1, 1, snapshot{regions: []region{{id: "list", bounds: render.Rect{W: 10, H: 5}, flags: Focusable | Scrollable | Activatable, contentHeight: 2}}, focus: "list"})
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 1, Action: event.MousePress, Button: event.MouseLeft}}
	if _, handled := ctx.Handle(press); !handled {
		t.Fatal("press was not captured")
	}
	release := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 4, Action: event.MouseRelease}}
	if action, handled := ctx.Handle(release); !handled || action.Kind == Activate {
		t.Fatalf("blank release = %#v handled=%v", action, handled)
	}
}

func TestClippedRegionKeepsLogicalCoordinates(t *testing.T) {
	buffer := render.NewBuffer(10, 6)
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 2, Y: 2, W: 8, H: 4})
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	ctx.Begin(frame)
	ctx.RegionWithOptions("list", canvas, render.Rect{W: 10, H: 5}, Focusable|Scrollable|Activatable, RegionOptions{ContentHeight: 5})
	ctx.mu.Lock()
	item := ctx.building[0]
	ctx.mu.Unlock()
	if item.bounds != (render.Rect{X: 2, Y: 2, W: 8, H: 3}) || item.originX != 0 || item.originY != 0 {
		t.Fatalf("region = %#v", item)
	}
	action := actionFor(item, Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 2, Y: 2, Action: event.MousePress, Button: event.MouseLeft}})
	if action.X != 2 || action.Y != 2 {
		t.Fatalf("local coordinates = %d,%d", action.X, action.Y)
	}
}

func TestModalTrapDismissAndRestore(t *testing.T) {
	var ctx Context
	base := []region{
		{id: "base-a", bounds: render.Rect{W: 8, H: 2}, flags: Focusable | Activatable},
		{id: "base-b", bounds: render.Rect{X: 9, W: 8, H: 2}, flags: Focusable | Activatable},
	}
	presented(&ctx, base, modal{})
	if !ctx.Focus("base-b") {
		t.Fatal("could not focus base")
	}
	modalRegions := append(append([]region(nil), base...),
		region{id: "modal-a", bounds: render.Rect{X: 5, Y: 3, W: 8, H: 1}, flags: Focusable | Editable, modal: "dialog"},
		region{id: "modal-b", bounds: render.Rect{X: 5, Y: 5, W: 8, H: 1}, flags: Focusable | Activatable, modal: "dialog"},
	)
	presented(&ctx, modalRegions, modal{id: "dialog", bounds: render.Rect{X: 4, Y: 2, W: 10, H: 6}})
	if !ctx.Focused("modal-a") || ctx.Focus("base-a") {
		t.Fatalf("modal focus was not trapped")
	}
	action, handled := ctx.Handle(event.KeyPress(event.KeyBackTab, 0))
	if !handled || action.Target != "modal-b" || action.Kind != Focus {
		t.Fatalf("modal backtab = %#v handled=%v", action, handled)
	}
	action, handled = ctx.Handle(event.KeyPress(event.KeyEscape, 0))
	if !handled || action.Kind != Dismiss || action.Target != "dialog" {
		t.Fatalf("escape = %#v handled=%v", action, handled)
	}
	outside := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 1, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled = ctx.Handle(outside)
	if !handled || action.Kind != Dismiss || action.Target != "dialog" || action.X != -3 || action.Y != -1 {
		t.Fatalf("outside = %#v handled=%v", action, handled)
	}
	presented(&ctx, base, modal{})
	if !ctx.Focused("base-b") {
		t.Fatal("base focus was not restored")
	}
}

// TestContextKeyReleasesAreInert pins the widget contract for kitty event
// types: a release never activates, moves focus, scrolls, edits, or dismisses
// a modal, while a repeat keeps routing exactly like a press.
func TestContextKeyReleasesAreInert(t *testing.T) {
	var ctx Context
	presented(&ctx, []region{
		{id: "button", bounds: render.Rect{W: 8, H: 1}, flags: Focusable | Activatable},
		{id: "input", bounds: render.Rect{X: 10, Y: 0, W: 8, H: 1}, flags: Focusable | Editable},
		{id: "list", bounds: render.Rect{Y: 2, W: 20, H: 5}, flags: Focusable | Scrollable},
	}, modal{})
	release := func(e event.Event) event.Event {
		e.Key.Action = event.ActionRelease
		e.Key.Repeat = 0
		return e
	}

	if action, handled := ctx.Handle(release(event.KeyPress(event.KeyEnter, 0))); handled || action.Kind != None {
		t.Fatalf("enter release = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(release(event.RuneKey('x', 0))); handled || action.Kind != None {
		t.Fatalf("rune release = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(release(event.KeyPress(event.KeyTab, 0))); handled || action.Kind != None {
		t.Fatalf("tab release = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(release(event.KeyPress(event.KeyDown, 0))); handled || action.Kind != None {
		t.Fatalf("down release = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(release(event.KeyPress(event.KeyEscape, 0))); handled || action.Kind != None {
		t.Fatalf("escape release = %#v handled=%v", action, handled)
	}

	if action, handled := ctx.Handle(event.KeyPress(event.KeyEnter, 0)); !handled || action.Kind != Activate || action.Target != "button" {
		t.Fatalf("enter press = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(event.KeyPress(event.KeyTab, 0)); !handled || action.Kind != Focus || action.Target != "input" {
		t.Fatalf("tab press = %#v handled=%v", action, handled)
	}
	if action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x', Action: event.ActionRepeat, Repeat: 1}}); !handled || action.Kind != Input || action.Target != "input" {
		t.Fatalf("rune repeat = %#v handled=%v", action, handled)
	}

	var modalCtx Context
	presented(&modalCtx, []region{
		{id: "modal-a", bounds: render.Rect{X: 5, Y: 3, W: 8, H: 1}, flags: Focusable | Editable, modal: "dialog"},
	}, modal{id: "dialog", bounds: render.Rect{X: 4, Y: 2, W: 10, H: 6}})
	if action, handled := modalCtx.Handle(release(event.KeyPress(event.KeyEscape, 0))); handled || action.Kind != None {
		t.Fatalf("modal escape release = %#v handled=%v", action, handled)
	}
	if action, handled := modalCtx.Handle(event.KeyPress(event.KeyEscape, 0)); !handled || action.Kind != Dismiss || action.Target != "dialog" {
		t.Fatalf("modal escape press = %#v handled=%v", action, handled)
	}
}

func TestResizeRejectsStalePresentation(t *testing.T) {
	var ctx Context
	ctx.mu.Lock()
	ctx.generation = 1
	ctx.sequence = 1
	ctx.sizeKnown, ctx.width, ctx.height = true, 20, 5
	ctx.mu.Unlock()
	stale := snapshot{regions: []region{{id: "old", bounds: render.Rect{W: 4, H: 1}, flags: Focusable}}, focus: "old"}
	ctx.Handle(event.ResizeTo(40, 10))
	ctx.commit(1, 1, stale)
	if ctx.Focused("old") || ctx.Focus("old") {
		t.Fatal("pre-resize geometry committed")
	}
}

type gatedBackend struct {
	events  chan event.Event
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *gatedBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true}, nil
}
func (b *gatedBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}
func (b *gatedBackend) Present(ctx context.Context, _ *render.Buffer) error {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*gatedBackend) Size() (int, int, error) { return 20, 5, nil }
func (*gatedBackend) Close() error            { return nil }

type commitModel struct{ ui *Context }

func (m commitModel) Update(next event.Event) lux.Command {
	if next.Kind == event.Key && next.Key.Rune == 'q' {
		return lux.Quit()
	}
	return nil
}
func (m commitModel) View(frame *lux.Frame) {
	m.ui.Begin(frame)
	m.ui.Region("ready", frame.Canvas, render.Rect{W: 8, H: 1}, Focusable)
	m.ui.End(frame)
}

func TestEndCommitsOnlyAfterSuccessfulPresent(t *testing.T) {
	backend := &gatedBackend{events: make(chan event.Event, 1), started: make(chan struct{}), release: make(chan struct{})}
	var ctx Context
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), commitModel{ui: &ctx}, lux.WithBackend(backend)) }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("presentation did not start")
	}
	if ctx.Focused("ready") || ctx.Focus("ready") {
		t.Fatal("unpresented region became interactive")
	}
	close(backend.release)
	deadline := time.Now().Add(time.Second)
	for !ctx.Focused("ready") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !ctx.Focused("ready") {
		t.Fatal("presented region did not commit")
	}
	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}
}

func BenchmarkHitRouting(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			regions := make([]region, count)
			for index := range regions {
				regions[index] = region{id: ID(fmt.Sprint(index)), bounds: render.Rect{X: index, W: 1, H: 1}, flags: Focusable | Activatable}
			}
			var ctx Context
			presented(&ctx, regions, modal{})
			next := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: count - 1, Action: event.MouseMove}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx.Handle(next)
			}
		})
	}
}
