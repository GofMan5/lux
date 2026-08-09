// Package event defines the value events exchanged by LUX applications and backends.
package event

import (
	"fmt"
	"strings"
	"time"
)

// Kind identifies the payload carried by an Event.
type Kind uint8

const (
	None Kind = iota
	Key
	Text
	Paste
	Mouse
	Resize
	Focus
	Frame
	Quit
	Error
)

// Event is deliberately a compact tagged value instead of an interface tree.
// Fields unrelated to Kind are zero-valued.
type Event struct {
	Kind    Kind
	At      time.Time
	Delta   time.Duration
	Key     KeyEvent
	Text    string
	Mouse   MouseEvent
	Width   int
	Height  int
	Focused bool
	Err     error
}

// KeyCode identifies non-text keys. KeyRune uses KeyEvent.Rune.
type KeyCode uint16

const (
	KeyUnknown KeyCode = iota
	KeyRune
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyTab
	KeyBackTab
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Mod is a bit set of keyboard or pointer modifiers.
type Mod uint8

const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// KeyEvent describes one key press. Repeat is greater than one when the
// platform reports a coalesced auto-repeat.
type KeyEvent struct {
	Code   KeyCode
	Rune   rune
	Mod    Mod
	Repeat uint16
}

// Is reports whether a key has code and every requested modifier.
func (k KeyEvent) Is(code KeyCode, modifiers Mod) bool {
	return k.Code == code && k.Mod&modifiers == modifiers
}

func (k KeyEvent) String() string {
	parts := make([]string, 0, 3)
	if k.Mod&ModCtrl != 0 {
		parts = append(parts, "ctrl")
	}
	if k.Mod&ModAlt != 0 {
		parts = append(parts, "alt")
	}
	if k.Mod&ModShift != 0 {
		parts = append(parts, "shift")
	}
	return strings.Join(append(parts, keyName(k)), "+")
}

func keyName(k KeyEvent) string {
	if k.Code == KeyRune {
		return string(k.Rune)
	}
	names := [...]string{"unknown", "rune", "enter", "escape", "backspace", "tab", "backtab", "up", "down", "left", "right", "home", "end", "pageup", "pagedown", "insert", "delete", "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12"}
	if int(k.Code) < len(names) {
		return names[k.Code]
	}
	return fmt.Sprintf("key(%d)", k.Code)
}

// MouseAction identifies pointer motion, buttons, and wheels.
type MouseAction uint8

const (
	MouseMove MouseAction = iota
	MousePress
	MouseRelease
	MouseWheel
)

// MouseButton identifies terminal pointer buttons.
type MouseButton uint8

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseEvent uses zero-based cell coordinates.
type MouseEvent struct {
	X      int
	Y      int
	Action MouseAction
	Button MouseButton
	Mod    Mod
}

// Constructors keep application tests readable.
func KeyPress(code KeyCode, modifiers Mod) Event {
	return Event{Kind: Key, Key: KeyEvent{Code: code, Mod: modifiers, Repeat: 1}}
}

func RuneKey(r rune, modifiers Mod) Event {
	return Event{Kind: Key, Key: KeyEvent{Code: KeyRune, Rune: r, Mod: modifiers, Repeat: 1}}
}

func ResizeTo(width, height int) Event { return Event{Kind: Resize, Width: width, Height: height} }
