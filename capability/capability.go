// Package capability describes terminal features negotiated by a backend.
package capability

// ColorDepth is the strongest color representation a terminal accepts.
type ColorDepth uint8

const (
	Monochrome ColorDepth = iota
	ANSI16
	ANSI256
	TrueColor
)

// Graphics identifies an optional terminal image protocol. Cell rendering is
// always available and remains the portable fallback.
type Graphics uint8

const (
	GraphicsCells Graphics = iota
	GraphicsKitty
	GraphicsSixel
	GraphicsITerm
)

// Set is a backend's immutable feature snapshot. Applications should adapt to
// these flags rather than guessing from a terminal brand.
type Set struct {
	Terminal         string
	Interactive      bool
	Unicode          bool
	Color            ColorDepth
	AlternateScreen  bool
	CursorAddressing bool
	Mouse            bool
	Focus            bool
	BracketedPaste   bool
	// PixelMouse reports whether the terminal answered the DECRQM query for
	// pixel-precision mouse (mode 1016, the kitty SGR pixel encoding). When
	// set and the program opts in, the backend switches mouse tracking to
	// that mode and mouse events carry zero-based pixel coordinates in
	// PixelX and PixelY alongside cell coordinates.
	PixelMouse bool
	// KeyboardEnhanced reports whether the terminal answered the kitty
	// keyboard protocol flags query. When set, the backend pushes the
	// protocol's disambiguate flag: ambiguous keys (Esc, Alt+key, modified
	// Enter/Tab/Backspace, multi-modifier combos) arrive as complete CSI u
	// sequences instead of legacy byte-prefix hacks. The flag stays clear on
	// terminals that did not answer, which keeps legacy decoding untouched.
	KeyboardEnhanced bool
	// Win32Input reports whether the terminal answered the DECRPM query for
	// win32-input-mode (DEC private mode 9001, the Windows console
	// keyboard-record encoding). When set — and the terminal lacks the kitty
	// keyboard protocol, which wins when both answer because it carries more
	// — the backend enables the mode: key events arrive as complete CSI
	// underscore records carrying the virtual key code, UnicodeChar, key-down
	// flag, control-key state, and repeat count, the Windows counterpart of
	// kitty's disambiguate flag. Terminals that did not answer keep legacy
	// input decoding untouched.
	Win32Input bool
	// ScrollRegions reports whether the terminal implements scroll regions
	// (DECSTBM with insert/delete lines). Without it, a backend must repaint
	// scrolled rows instead of asking the terminal to move them.
	ScrollRegions      bool
	SynchronizedOutput bool
	Hyperlinks         bool
	Graphics           Graphics
}

// Portable returns conservative features suitable for redirected output.
func Portable() Set { return Set{Terminal: "dumb", Color: Monochrome} }
