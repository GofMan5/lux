package ui

import (
	"fmt"
	"testing"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func scrollAction(next event.Event) Action { return Action{Kind: Scroll, Event: next} }

func TestScrollRangeClampAndEnsureVisible(t *testing.T) {
	state := ScrollState{Offset: 99}
	if got := state.Range(10, 3); got != (VisibleRange{First: 7, Last: 10}) || state.Offset != 7 {
		t.Fatalf("range = %#v state=%#v", got, state)
	}
	state.EnsureVisible(2, 10, 3)
	if state.Offset != 2 {
		t.Fatalf("ensure above = %#v", state)
	}
	state.EnsureVisible(8, 10, 3)
	if state.Offset != 6 {
		t.Fatalf("ensure below = %#v", state)
	}
	state.EnsureVisible(999, 10, 3)
	if state.Offset != 7 {
		t.Fatalf("ensure clamped index = %#v", state)
	}
	if got := state.Range(2, 8); got != (VisibleRange{First: 0, Last: 2}) || state.Offset != 0 {
		t.Fatalf("shrink = %#v state=%#v", got, state)
	}
	state.Offset = 5
	if got := state.Range(0, 0); got != (VisibleRange{}) || state.Offset != 0 {
		t.Fatalf("zero = %#v state=%#v", got, state)
	}
}

func TestScrollApplyKeysWheelClickAndNestedFallback(t *testing.T) {
	state := ScrollState{}
	if !state.Apply(scrollAction(event.KeyPress(event.KeyDown, 0)), 20, 6) || state.Offset != 1 {
		t.Fatalf("down = %#v", state)
	}
	state.Apply(scrollAction(event.KeyPress(event.KeyPageDown, 0)), 20, 6)
	if state.Offset != 7 {
		t.Fatalf("page down = %#v", state)
	}
	state.Apply(scrollAction(event.KeyPress(event.KeyEnd, 0)), 20, 6)
	if state.Offset != 14 {
		t.Fatalf("end = %#v", state)
	}
	if state.Apply(scrollAction(event.KeyPress(event.KeyDown, 0)), 20, 6) {
		t.Fatal("down at boundary was consumed")
	}
	wheelUp := scrollAction(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelUp}})
	if !state.Apply(wheelUp, 20, 6) || state.Offset != 12 {
		t.Fatalf("wheel = %#v", state)
	}
	click := scrollAction(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}})
	if state.Apply(click, 20, 6) || state.Offset != 12 {
		t.Fatalf("click scrolled = %#v", state)
	}
	var ctx Context
	ctx.generation, ctx.sizeKnown = 1, true
	ctx.commit(1, 1, snapshot{regions: []region{
		{id: "other", bounds: render.Rect{W: 2, H: 1}, flags: Focusable},
		{id: "viewport", bounds: render.Rect{Y: 1, W: 10, H: 4}, originY: 1, flags: Focusable | Scrollable, contentHeight: 4},
	}, focus: "other"})
	press := event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 1, Y: 2, Action: event.MousePress, Button: event.MouseLeft}}
	action, handled := ctx.Handle(press)
	if !handled || action.Kind != Scroll || action.Target != "viewport" || !ctx.Focused("viewport") || state.Apply(action, 20, 6) {
		t.Fatalf("click focus = %#v handled=%v focused=%v state=%#v", action, handled, ctx.Focused("viewport"), state)
	}

	child, parent := ScrollState{}, ScrollState{Offset: 4}
	if child.Apply(wheelUp, 3, 3) {
		t.Fatal("child consumed wheel at top")
	}
	if !parent.Apply(wheelUp, 20, 5) || parent.Offset != 3 {
		t.Fatalf("parent fallback = %#v", parent)
	}
}

func TestScrollViewportRegistersContentAndClippedOrigin(t *testing.T) {
	buffer := render.NewBuffer(10, 6)
	canvas := render.NewCanvas(buffer).Clip(render.Rect{X: 2, Y: 2, W: 8, H: 4})
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	state := ScrollState{Offset: 2}
	ctx.Begin(frame)
	view := ctx.ScrollViewport("scroll", canvas, render.Rect{W: 10, H: 5}, &state, 5, ScrollStyle{})
	if width, height := view.Size(); width != 10 || height != 5 {
		t.Fatalf("viewport = %dx%d", width, height)
	}
	if len(ctx.building) != 1 {
		t.Fatalf("regions = %d", len(ctx.building))
	}
	item := ctx.building[0]
	if item.bounds != (render.Rect{X: 2, Y: 2, W: 8, H: 3}) || item.originX != 0 || item.originY != 0 || item.contentHeight != 5 {
		t.Fatalf("region = %#v", item)
	}
	action := actionFor(item, Scroll, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{X: 2, Y: 2, Action: event.MouseWheel, Button: event.MouseWheelDown}})
	if action.X != 2 || action.Y != 2 {
		t.Fatalf("local action = %d,%d", action.X, action.Y)
	}
}

func TestScrollViewportTinyEmptyDisabledAndOverflow(t *testing.T) {
	buffer := render.NewBuffer(1, 1)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	state := ScrollState{Offset: int(^uint(0) >> 1)}
	ctx.Begin(frame)
	view := ctx.ScrollViewport("empty", canvas, canvas.Bounds(), &state, 0, ScrollStyle{Disabled: true})
	if view == nil || state.Offset != 0 || len(ctx.building) != 1 || ctx.building[0].contentHeight != 0 || ctx.building[0].flags&Disabled == 0 {
		t.Fatalf("empty viewport: view=%v state=%#v regions=%#v", view, state, ctx.building)
	}
	state.Offset = int(^uint(0)>>1) - 20
	if !state.Apply(scrollAction(event.KeyPress(event.KeyPageDown, 0)), int(^uint(0)>>1), 10) || state.Offset != int(^uint(0)>>1)-10 {
		t.Fatalf("overflow-safe page = %#v", state)
	}
	if got := (&ScrollState{Offset: -5}).Range(1, 1); got != (VisibleRange{First: 0, Last: 1}) {
		t.Fatalf("tiny range = %#v", got)
	}
}

func BenchmarkScrollRangeMillion(b *testing.B) {
	for _, rows := range []int{8, 24, 80} {
		b.Run(fmt.Sprint(rows), func(b *testing.B) {
			state := ScrollState{Offset: 500_000}
			total := 0
			b.ReportAllocs()
			for range b.N {
				visible := state.Range(1_000_000, rows)
				for index := visible.First; index < visible.Last; index++ {
					total += index & 1
				}
			}
			if total == -1 {
				b.Fatal(total)
			}
		})
	}
}
