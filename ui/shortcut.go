package ui

import "github.com/lux-tui/lux/event"

// ShortcutScope selects where a key binding participates in routing.
type ShortcutScope uint8

const (
	ScopeFocused ShortcutScope = iota
	ScopeModal
	ScopeApp
)

// Shortcut routes an exact key to an ordinary Action. Repeat is deliberately
// ignored while Code, Rune, and the intent modifiers must match exactly;
// the caps-lock and num-lock bits report keyboard state rather than intent,
// so they are ignored on both the binding and the incoming key. The zero
// value is active; Disabled conditionally removes a binding without
// changing layout.
type Shortcut struct {
	Key event.KeyEvent
	// Owner is the focus/modal ID that enables this binding. Empty defaults to
	// Target; Target remains the emitted Action destination.
	Owner  ID
	Target ID
	Kind   ActionKind
	Scope  ShortcutScope
	// Priority is reserved for explicit application lifecycle commands such
	// as quit/help. It resolves before focused editor input and active modals.
	Priority bool
	Disabled bool
}

// Shortcut registers a binding for the frame being built. Focused bindings
// apply only while Owner (or Target when Owner is empty) owns focus; modal
// bindings use the active modal owner. App bindings are suppressed by modals
// unless explicitly marked Priority.
func (c *Context) Shortcut(binding Shortcut) {
	if c == nil || binding.Disabled || binding.Target == "" || binding.Kind == None {
		return
	}
	if binding.Scope > ScopeApp {
		panic("ui: invalid shortcut scope")
	}
	if binding.Priority && binding.Scope != ScopeApp {
		panic("ui: priority shortcut must use app scope")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.begun {
		return
	}
	for _, existing := range c.shortcuts {
		sameOwner := binding.Scope == ScopeApp || shortcutOwner(existing) == shortcutOwner(binding)
		if existing.Scope == binding.Scope && sameOwner && sameShortcutKey(existing.Key, binding.Key) {
			panic("ui: duplicate shortcut")
		}
	}
	c.shortcuts = append(c.shortcuts, binding)
}

func matchPriorityShortcut(state snapshot, key event.KeyEvent) (Shortcut, bool) {
	for index := len(state.shortcuts) - 1; index >= 0; index-- {
		binding := state.shortcuts[index]
		if binding.Scope == ScopeApp && binding.Priority && sameShortcutKey(binding.Key, key) {
			return binding, true
		}
	}
	return Shortcut{}, false
}

func matchShortcut(state snapshot, key event.KeyEvent, scope ShortcutScope, owner ID) (Shortcut, bool) {
	for index := len(state.shortcuts) - 1; index >= 0; index-- {
		binding := state.shortcuts[index]
		owned := scope == ScopeApp || shortcutOwner(binding) == owner
		if binding.Scope == scope && !binding.Priority && owned && sameShortcutKey(binding.Key, key) {
			return binding, true
		}
	}
	return Shortcut{}, false
}

func shortcutOwner(binding Shortcut) ID {
	if binding.Owner != "" {
		return binding.Owner
	}
	return binding.Target
}

// modLocks masks the keyboard-state bits: caps-lock and num-lock report
// what the keyboard is doing, not what the user asked for. Enhanced
// terminals attach them to real key events (win32-input-mode records carry
// NUMLOCK_ON; kitty reports the same pair), so a binding registered
// without them must still match a press that carries them — otherwise
// every functional-key shortcut dies the moment NumLock is on.
const modLocks = event.ModCapsLock | event.ModNumLock

// sameShortcutKey compares key intent: code, rune, and the shift, alt,
// ctrl, super, hyper, and meta bits must match exactly on both sides,
// while the lock bits ride along as state and never affect routing.
func sameShortcutKey(left, right event.KeyEvent) bool {
	return left.Code == right.Code && left.Rune == right.Rune &&
		left.Mod&^modLocks == right.Mod&^modLocks
}

func shortcutAction(binding Shortcut, next event.Event) Action {
	return Action{Target: binding.Target, Kind: binding.Kind, Event: next}
}
