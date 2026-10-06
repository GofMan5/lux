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
