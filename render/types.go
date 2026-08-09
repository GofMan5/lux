// Package render provides a small, terminal-independent cell renderer.
package render

import "github.com/rivo/uniseg"

// ColorMode describes how a terminal color is encoded.
type ColorMode uint8

const (
	ColorDefault ColorMode = iota
	ColorANSI
	ColorIndexed
	ColorRGB
)

// Color is a comparable terminal color. Value contains an ANSI/index value or
// packed 0xRRGGBB for ColorRGB.
type Color struct {
	Mode  ColorMode
	Value uint32
}

// ANSIColor returns one of the terminal's first 16 colors.
func ANSIColor(index uint8) Color { return Color{Mode: ColorANSI, Value: uint32(index & 15)} }

// IndexedColor returns a color from the terminal's 256-color palette.
func IndexedColor(index uint8) Color { return Color{Mode: ColorIndexed, Value: uint32(index)} }

// RGBColor returns a 24-bit terminal color.
func RGBColor(red, green, blue uint8) Color {
	return Color{Mode: ColorRGB, Value: uint32(red)<<16 | uint32(green)<<8 | uint32(blue)}
}

// RGB returns the color channels and whether c is an RGB color.
func (c Color) RGB() (red, green, blue uint8, ok bool) {
	if c.Mode != ColorRGB {
		return 0, 0, 0, false
	}
	return uint8(c.Value >> 16), uint8(c.Value >> 8), uint8(c.Value), true
}

// Attr is a bit set of terminal text attributes.
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrReverse
	AttrHidden
	AttrStrike
)

// Style is deliberately comparable so cell diffs need no allocation.
type Style struct {
	Foreground Color
	Background Color
	Attrs      Attr
}

// Has reports whether all attributes are present.
func (s Style) Has(attrs Attr) bool { return s.Attrs&attrs == attrs }

// Cell is one terminal grid cell. Width is 1 or 2 for a grapheme's leading
// cell and 0 for the continuation of a width-2 grapheme.
type Cell struct {
	Grapheme string
	Width    uint8
	Style    Style
}

// BlankCell is the canonical empty terminal cell.
var BlankCell = Cell{Grapheme: " ", Width: 1}

// IsContinuation reports whether c is the trailing cell of a wide grapheme.
func (c Cell) IsContinuation() bool { return c.Width == 0 }

// Rect is a half-open rectangle: [X,X+W) by [Y,Y+H).
type Rect struct {
	X int
	Y int
	W int
	H int
}

// Bounds returns a rectangle rooted at zero. Negative dimensions become zero.
func Bounds(width, height int) Rect {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	return Rect{W: width, H: height}
}

// Right returns the exclusive right edge.
func (r Rect) Right() int { return r.X + max(r.W, 0) }

// Bottom returns the exclusive bottom edge.
func (r Rect) Bottom() int { return r.Y + max(r.H, 0) }

// Empty reports whether r has no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether a cell is inside r.
func (r Rect) Contains(x, y int) bool {
	return r.W > 0 && r.H > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Intersect returns the common area of r and other.
func (r Rect) Intersect(other Rect) Rect {
	x1, y1 := max(r.X, other.X), max(r.Y, other.Y)
	x2, y2 := min(r.Right(), other.Right()), min(r.Bottom(), other.Bottom())
	if x2 <= x1 || y2 <= y1 {
		return Rect{X: x1, Y: y1}
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// Inset shrinks r on all sides, clamping at its center.
func (r Rect) Inset(horizontal, vertical int) Rect {
	if horizontal < 0 {
		horizontal = 0
	}
	if vertical < 0 {
		vertical = 0
	}
	horizontal = min(horizontal, max(r.W, 0)/2)
	vertical = min(vertical, max(r.H, 0)/2)
	return Rect{X: r.X + horizontal, Y: r.Y + vertical, W: max(r.W, 0) - 2*horizontal, H: max(r.H, 0) - 2*vertical}
}

// SplitLeft splits r at a clamped column, which makes fixed sidebars degrade
// cleanly in narrow terminals.
func (r Rect) SplitLeft(width int) (left, rest Rect) {
	width = min(max(width, 0), max(r.W, 0))
	return Rect{X: r.X, Y: r.Y, W: width, H: max(r.H, 0)},
		Rect{X: r.X + width, Y: r.Y, W: max(r.W, 0) - width, H: max(r.H, 0)}
}

// SplitTop splits r at a clamped row.
func (r Rect) SplitTop(height int) (top, rest Rect) {
	height = min(max(height, 0), max(r.H, 0))
	return Rect{X: r.X, Y: r.Y, W: max(r.W, 0), H: height},
		Rect{X: r.X, Y: r.Y + height, W: max(r.W, 0), H: max(r.H, 0) - height}
}

// GraphemeWidth returns the terminal width of the first grapheme in text.
// Terminal cells support at most two columns, so larger library results clamp.
func GraphemeWidth(text string) int {
	_, width := firstGrapheme(text)
	return width
}

// TextWidth returns the widest line in text in terminal cells.
func TextWidth(text string) int {
	line, widest := 0, 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch grapheme {
		case "\n", "\r\n":
			widest = max(widest, line)
			line = 0
		case "\r":
			widest = max(widest, line)
			line = 0
		case "\t":
			line += 4 - line%4
		default:
			line += terminalWidth(grapheme)
		}
	}
	return max(widest, line)
}

func firstGrapheme(text string) (string, int) {
	graphemes := uniseg.NewGraphemes(text)
	if !graphemes.Next() {
		return "", 0
	}
	grapheme := graphemes.Str()
	return grapheme, terminalWidth(grapheme)
}

func terminalWidth(grapheme string) int { return min(max(uniseg.StringWidth(grapheme), 0), 2) }
