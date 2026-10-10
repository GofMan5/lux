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
	// Suspend reports the terminal state being handed away: the backend has
	// written its leave sequences, restored cooked input, and (on Unix) is
	// about to stop the process for job control. Frames presented while
	// suspended are dropped, and the resume that follows repaints from
	// scratch, so a model usually only marks the pause and stops animations.
	Suspend
	// Resume reports the terminal state coming back: the backend has
	// re-entered raw mode and its enter sequences, and dropped its diff
	// baseline, so the next presented frame is a full repaint. A resize
	// riding the same wakeup is delivered separately before this event.
	Resume
	// Message delivers an application-defined payload from a Command back
	// to Model.Update. It is the seam for asynchronous application
	// results: unlike Error it never terminates the run, so recoverable
	// application failures travel as payloads the model renders, while
	// transport-level failures keep using Error. The payload's type and
	// ownership belong to the application; LUX only serializes delivery.
	Message
)

// String names the kind for logs and test failures; the names match the
// constants, lowercase.
func (k Kind) String() string {
	switch k {
	case None:
		return "none"
	case Key:
		return "key"
	case Text:
		return "text"
	case Paste:
		return "paste"
	case Mouse:
		return "mouse"
	case Resize:
		return "resize"
	case Focus:
		return "focus"
	case Frame:
		return "frame"
	case Quit:
		return "quit"
	case Error:
		return "error"
	case Suspend:
		return "suspend"
	case Resume:
		return "resume"
	case Message:
		return "message"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

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
	// Data carries the Message payload. It is nil for every other kind,
	// and the application owns its type: LUX treats it as opaque.
	Data any
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
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	KeyF21
	KeyF22
	KeyF23
	KeyF24
	KeyF25
	KeyF26
	KeyF27
	KeyF28
	KeyF29
	KeyF30
	KeyF31
	KeyF32
	KeyF33
	KeyF34
	KeyF35
	KeyCapsLock
	KeyScrollLock
	KeyNumLock
	KeyPrintScreen
	KeyPause
	KeyMenu
	KeyMediaPlay
	KeyMediaPause
	KeyMediaPlayPause
	KeyMediaReverse
	KeyMediaStop
	KeyMediaFastForward
	KeyMediaRewind
	KeyMediaTrackNext
	KeyMediaTrackPrevious
	KeyMediaRecord
	KeyVolumeDown
	KeyVolumeUp
	KeyVolumeMute
)

// Mod is a bit set of keyboard or pointer modifiers. The eight bits mirror
// the kitty keyboard protocol's modifier set: shift, alt, ctrl, and super
// carry intent; hyper and meta are their extended kin; the two lock bits
// report keyboard state that enhanced terminals attach to non-text keys.
type Mod uint8

const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
	ModHyper
	ModMeta
	ModCapsLock
	ModNumLock
)

// KeyAction distinguishes a key press from a platform-reported auto-repeat
// or release. Legacy input produces presses only, so ActionPress is the
// zero value and every KeyEvent built without a terminal-reported action
// decodes as a press.
type KeyAction uint8

const (
	ActionPress KeyAction = iota
	ActionRepeat
	ActionRelease
)

// KeyEvent describes one key event. Action separates press, auto-repeat,
// and release. Repeat is greater than one when a platform reports a
// coalesced auto-repeat burst; a release reports no presses, so its Repeat
// is zero.
type KeyEvent struct {
	Code   KeyCode
	Rune   rune
	Mod    Mod
	Action KeyAction
	Repeat uint16
}

// Is reports whether a key has code and every requested modifier.
func (k KeyEvent) Is(code KeyCode, modifiers Mod) bool {
	return k.Code == code && k.Mod&modifiers == modifiers
}

func (k KeyEvent) String() string {
	parts := make([]string, 0, 8)
	if k.Mod&ModCtrl != 0 {
		parts = append(parts, "ctrl")
	}
	if k.Mod&ModAlt != 0 {
		parts = append(parts, "alt")
	}
	if k.Mod&ModShift != 0 {
		parts = append(parts, "shift")
	}
	if k.Mod&ModSuper != 0 {
		parts = append(parts, "super")
	}
	if k.Mod&ModHyper != 0 {
		parts = append(parts, "hyper")
	}
	if k.Mod&ModMeta != 0 {
		parts = append(parts, "meta")
	}
	if k.Mod&ModCapsLock != 0 {
		parts = append(parts, "caps-lock")
	}
	if k.Mod&ModNumLock != 0 {
		parts = append(parts, "num-lock")
	}
	name := strings.Join(append(parts, keyName(k)), "+")
	if k.Action == ActionRelease {
		name = "released " + name
	}
	return name
}

func keyName(k KeyEvent) string {
	if k.Code == KeyRune {
		return string(k.Rune)
	}
	names := [...]string{
		"unknown", "rune", "enter", "escape", "backspace", "tab", "backtab", "up", "down", "left", "right", "home", "end",
		"pageup", "pagedown", "insert", "delete",
		"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12",
		"f13", "f14", "f15", "f16", "f17", "f18", "f19", "f20", "f21", "f22", "f23", "f24",
		"f25", "f26", "f27", "f28", "f29", "f30", "f31", "f32", "f33", "f34", "f35",
		"capslock", "scrolllock", "numlock", "printscreen", "pause", "menu",
		"mediaplay", "mediapause", "mediaplaypause", "mediareverse", "mediastop",
		"mediafastforward", "mediarewind", "mediatracknext", "mediatrackprevious", "mediarecord",
		"volumedown", "volumeup", "volumemute",
	}
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
	MouseBack
	MouseForward
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseEvent uses zero-based cell coordinates. Button names the involved
// button for presses, drags, and SGR releases; pure hover motion carries
// MouseNone. PixelX and PixelY carry zero-based pixel coordinates when
// pixel-precision mouse is active, and -1 otherwise. X and Y are -1 while
// the cell size is still unknown or when the pointer leaves the window.
type MouseEvent struct {
	X      int
	Y      int
	PixelX int
	PixelY int
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
