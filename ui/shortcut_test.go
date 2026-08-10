package ui

import (
	"testing"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func shortcutSnapshot(t *testing.T, ctx *Context, modalID ID, bindings ...Shortcut) {
	t.Helper()
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(20, 5))}
	ctx.Begin(frame)
	ctx.Region("field", frame.Canvas, render.Rect{W: 10, H: 1}, Focusable|Editable)
	if modalID != "" {
		ctx.BeginModal(modalID, frame.Canvas, render.Rect{W: 20, H: 5})
		ctx.Region("modal-button", frame.Canvas, render.Rect{Y: 2, W: 10, H: 1}, Focusable|Activatable)
	}
	for _, binding := range bindings {
		ctx.Shortcut(binding)
	}
	if modalID != "" {
		ctx.EndModal()
	}
	ctx.End(frame)
	commitBuilt(ctx)
}

func commitBuilt(ctx *Context) {
	ctx.mu.Lock()
	next := snapshot{regions: append([]region(nil), ctx.building...), shortcuts: append([]Shortcut(nil), ctx.shortcuts...), modal: ctx.buildingModal, focus: ctx.viewFocus}
	generation, sequence := ctx.buildingGen, ctx.sequence
	ctx.mu.Unlock()
	ctx.commit(generation, sequence, next)
}

func TestShortcutScopePriorityAndExactKey(t *testing.T) {
	var ctx Context
	key := event.KeyEvent{Code: event.KeyRune, Rune: 'k', Mod: event.ModCtrl}
	shortcutSnapshot(t, &ctx, "",
		Shortcut{Key: key, Target: "field", Kind: Activate, Scope: ScopeFocused},
		Shortcut{Key: key, Target: "app", Kind: Dismiss, Scope: ScopeApp},
	)
	action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'k', Mod: event.ModCtrl, Repeat: 9}})
	if !handled || action.Target != "field" || action.Kind != Activate {
		t.Fatalf("focused shortcut = %#v handled=%v", action, handled)
	}
	if action, handled = ctx.Handle(event.RuneKey('k', event.ModCtrl|event.ModShift)); !handled || action.Kind != Input || action.Target != "field" {
		t.Fatalf("extra modifier shortcut matched = %#v handled=%v", action, handled)
	}
}

func TestNativeEditorBeatsModalAndAppButFocusedMayOverride(t *testing.T) {
	var ctx Context
	shortcutSnapshot(t, &ctx, "",
		Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x'}, Target: "app", Kind: Activate, Scope: ScopeApp},
	)
	action, handled := ctx.Handle(event.RuneKey('x', 0))
	if !handled || action.Target != "field" || action.Kind != Input {
		t.Fatalf("editor input = %#v handled=%v", action, handled)
	}

	shortcutSnapshot(t, &ctx, "",
		Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x'}, Target: "field", Kind: Activate, Scope: ScopeFocused},
	)
	action, handled = ctx.Handle(event.RuneKey('x', 0))
	if !handled || action.Target != "field" || action.Kind != Activate {
		t.Fatalf("explicit focused override = %#v handled=%v", action, handled)
	}
}

func TestModalShortcutSuppressesAppAndEscapeWins(t *testing.T) {
	var ctx Context
	key := event.KeyEvent{Code: event.KeyF1}
	shortcutSnapshot(t, &ctx, "dialog",
		Shortcut{Key: key, Target: "dialog", Kind: Activate, Scope: ScopeModal},
		Shortcut{Key: key, Target: "app", Kind: Activate, Scope: ScopeApp},
		Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "dialog", Kind: Activate, Scope: ScopeModal},
	)
	action, handled := ctx.Handle(event.KeyPress(event.KeyF1, 0))
	if !handled || action.Target != "dialog" || action.Kind != Activate {
		t.Fatalf("modal shortcut = %#v handled=%v", action, handled)
	}
	action, handled = ctx.Handle(event.KeyPress(event.KeyEscape, 0))
	if !handled || action.Target != "dialog" || action.Kind != Dismiss {
		t.Fatalf("escape = %#v handled=%v", action, handled)
	}
}

func TestModalShortcutBeatsNativeEditableAndScrollable(t *testing.T) {
	key := event.KeyEvent{Code: event.KeyF1}
	for _, flags := range []Flags{Focusable | Editable, Focusable | Scrollable} {
		var ctx Context
		ctx.generation, ctx.sizeKnown = 1, true
		ctx.commit(1, 1, snapshot{
			regions:   []region{{id: "control", bounds: render.Rect{W: 10, H: 1}, flags: flags, modal: "dialog", contentHeight: -1}},
			shortcuts: []Shortcut{{Key: key, Target: "dialog", Kind: Activate, Scope: ScopeModal}},
			modal:     modal{id: "dialog", bounds: render.Rect{W: 20, H: 5}},
			focus:     "control",
		})
		action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: key})
		if !handled || action.Target != "dialog" || action.Kind != Activate {
			t.Fatalf("flags %v: action=%#v handled=%v", flags, action, handled)
		}
	}
}

func TestShortcutDisabledDuplicateAndPresentationBinding(t *testing.T) {
	var ctx Context
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(10, 2))}
	ctx.Begin(frame)
	key := event.KeyEvent{Code: event.KeyF2}
	ctx.Shortcut(Shortcut{Key: key, Target: "app", Kind: Activate, Scope: ScopeApp, Disabled: true})
	ctx.Shortcut(Shortcut{Key: key, Target: "app", Kind: Activate, Scope: ScopeApp})
	ctx.End(frame)
	if action, handled := ctx.Handle(event.KeyPress(event.KeyF2, 0)); handled || action.Kind != None {
		t.Fatalf("unpresented shortcut = %#v handled=%v", action, handled)
	}
	commitBuilt(&ctx)
	if action, handled := ctx.Handle(event.KeyPress(event.KeyF2, 0)); !handled || action.Target != "app" {
		t.Fatalf("presented shortcut = %#v handled=%v", action, handled)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate shortcut did not panic")
		}
	}()
	ctx.Begin(frame)
	ctx.Shortcut(Shortcut{Key: key, Target: "one", Kind: Activate, Scope: ScopeApp})
	ctx.Shortcut(Shortcut{Key: event.KeyEvent{Code: event.KeyF2, Repeat: 99}, Target: "two", Kind: Dismiss, Scope: ScopeApp})
}

func TestShortcutResizeInvalidatesCommittedBinding(t *testing.T) {
	var ctx Context
	shortcutSnapshot(t, &ctx, "", Shortcut{Key: event.KeyEvent{Code: event.KeyF3}, Target: "app", Kind: Activate, Scope: ScopeApp})
	ctx.Handle(event.ResizeTo(40, 10))
	if action, handled := ctx.Handle(event.KeyPress(event.KeyF3, 0)); handled || action.Kind != None {
		t.Fatalf("stale shortcut = %#v handled=%v", action, handled)
	}
}

func TestPriorityAppShortcutBeatsEditorAndModal(t *testing.T) {
	var ctx Context
	key := event.KeyEvent{Code: event.KeyRune, Rune: 'q', Mod: event.ModCtrl}
	shortcutSnapshot(t, &ctx, "dialog", Shortcut{Key: key, Target: "quit", Kind: Activate, Scope: ScopeApp, Priority: true})
	action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: key})
	if !handled || action.Target != "quit" || action.Kind != Activate {
		t.Fatalf("priority = %#v handled=%v", action, handled)
	}
}

func TestAppShortcutBeatsEditorWhenNoModal(t *testing.T) {
	var ctx Context
	key := event.KeyEvent{Code: event.KeyF1}
	shortcutSnapshot(t, &ctx, "", Shortcut{Key: key, Target: "help", Kind: Activate, Scope: ScopeApp, Priority: true})
	action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: key})
	if !handled || action.Target != "help" {
		t.Fatalf("app shortcut = %#v handled=%v", action, handled)
	}
}

func TestFocusedControlsMayShareAChord(t *testing.T) {
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(20, 2))}
	var ctx Context
	ctx.Begin(frame)
	key := event.KeyEvent{Code: event.KeyRune, Rune: 'c', Mod: event.ModCtrl}
	ctx.Shortcut(Shortcut{Key: key, Target: "one", Kind: Activate, Scope: ScopeFocused})
	ctx.Shortcut(Shortcut{Key: key, Target: "two", Kind: Activate, Scope: ScopeFocused})
}

func TestShortcutOwnerMayDifferFromActionTarget(t *testing.T) {
	var ctx Context
	key := event.KeyEvent{Code: event.KeyRune, Rune: 'c', Mod: event.ModCtrl}
	shortcutSnapshot(t, &ctx, "", Shortcut{Key: key, Owner: "field", Target: "copy", Kind: Activate, Scope: ScopeFocused})
	action, handled := ctx.Handle(event.Event{Kind: event.Key, Key: key})
	if !handled || action.Target != "copy" {
		t.Fatalf("owned shortcut = %#v handled=%v", action, handled)
	}
}

func TestPriorityNonAppPanics(t *testing.T) {
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(10, 2))}
	var ctx Context
	ctx.Begin(frame)
	defer func() {
		if recover() == nil {
			t.Fatal("invalid priority shortcut did not panic")
		}
	}()
	ctx.Shortcut(Shortcut{Key: event.KeyEvent{Code: event.KeyF1}, Target: "x", Kind: Activate, Scope: ScopeFocused, Priority: true})
}
