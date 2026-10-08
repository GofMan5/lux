package render

import (
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Border defines the eight graphemes used to draw a box.
type Border struct {
	TopLeft     string
	Top         string
	TopRight    string
	Left        string
	Right       string
	BottomLeft  string
	Bottom      string
	BottomRight string
}

var (
	SingleBorder  = Border{"┌", "─", "┐", "│", "│", "└", "─", "┘"}
	RoundedBorder = Border{"╭", "─", "╮", "│", "│", "╰", "─", "╯"}
	DoubleBorder  = Border{"╔", "═", "╗", "║", "║", "╚", "═", "╝"}
	HeavyBorder   = Border{"┏", "━", "┓", "┃", "┃", "┗", "━", "┛"}
	ASCIIBorder   = Border{"+", "-", "+", "|", "|", "+", "-", "+"}
)

// Canvas draws local coordinates into a clipped Buffer view.
type Canvas struct {
	buffer        *Buffer
	originX       int
	originY       int
	width, height int
	clip          Rect
	widthPolicy   WidthPolicy
}

// NewCanvas returns a canvas covering buffer under the default width policy.
func NewCanvas(buffer *Buffer) *Canvas {
	return NewCanvasWithPolicy(buffer, WidthPolicy{})
}

// NewCanvasWithPolicy returns a canvas covering buffer that measures and
// draws graphemes under policy. Policy travels with the canvas through
// Subview and Clip, so every control drawn through it stays coherent with
// the target terminal without threading policy through each call.
func NewCanvasWithPolicy(buffer *Buffer, policy WidthPolicy) *Canvas {
	width, height := buffer.Size()
	return &Canvas{buffer: buffer, width: width, height: height, clip: Bounds(width, height), widthPolicy: policy}
}

// WidthPolicy returns the policy this canvas measures with. A nil canvas
// reports the default policy.
func (c *Canvas) WidthPolicy() WidthPolicy {
	if c == nil {
		return WidthPolicy{}
	}
	return c.widthPolicy
}

// GraphemeWidth returns the terminal width of the first grapheme in text
// under this canvas's policy. It matches what the canvas's own drawing
// (Text, Put, Fill) would advance for that grapheme.
func (c *Canvas) GraphemeWidth(text string) int {
	return GraphemeWidthWith(text, c.WidthPolicy())
}

// TextWidth returns the widest line in text in terminal cells under this
// canvas's policy, so clipping and layout decisions match drawing.
func (c *Canvas) TextWidth(text string) int {
	return TextWidthWith(text, c.WidthPolicy())
}

// Buffer returns the canvas's underlying buffer.
func (c *Canvas) Buffer() *Buffer {
	if c == nil {
		return nil
	}
	return c.buffer
}

// Size returns the canvas's local dimensions.
func (c *Canvas) Size() (width, height int) {
	if c == nil {
		return 0, 0
	}
	return c.width, c.height
}

// Bounds returns the local canvas rectangle.
func (c *Canvas) Bounds() Rect {
	width, height := c.Size()
	return Bounds(width, height)
}

// GlobalRect converts a local rectangle to terminal-buffer coordinates and
// clips it to both the canvas bounds and its active clip. Interactive controls
// use it to publish hit regions that exactly match what can be drawn.
func (c *Canvas) GlobalRect(rect Rect) Rect {
	if c == nil || c.buffer == nil {
		return Rect{}
	}
	local := rect.Intersect(c.Bounds())
	absolute := Rect{X: c.originX + local.X, Y: c.originY + local.Y, W: local.W, H: local.H}
	return absolute.Intersect(c.clip).Intersect(c.buffer.Bounds())
}

// GlobalPoint converts a local cell coordinate to terminal-buffer space.
// Unlike GlobalRect it preserves the original origin even when drawing is
// clipped, which lets controls report coordinates in their logical content.
func (c *Canvas) GlobalPoint(x, y int) (int, int) {
	if c == nil {
		return x, y
	}
	return c.originX + x, c.originY + y
}

// SetCaret asks the terminal to show its cursor at a local cell. A caret
// outside the canvas or its clip leaves the cursor hidden, so a scrolled-out
// control never steals the visible cursor.
func (c *Canvas) SetCaret(x, y int) {
	if c == nil || c.buffer == nil {
		return
	}
	spot := c.GlobalRect(Rect{X: x, Y: y, W: 1, H: 1})
	if spot.Empty() {
		return
	}
	c.buffer.SetCaret(spot.X, spot.Y)
}

// ClipRect returns the canvas's active clip in terminal-buffer coordinates.
// Controls drawn through the same canvas share it, which is what lets an
// interaction layer tell a container's own children from its neighbours.
func (c *Canvas) ClipRect() Rect {
	if c == nil || c.buffer == nil {
		return Rect{}
	}
	return c.clip.Intersect(c.buffer.Bounds())
}

// Subview returns a canvas whose origin is rect's clipped top-left corner.
func (c *Canvas) Subview(rect Rect) *Canvas {
	if c == nil {
		return &Canvas{}
	}
	local := rect.Intersect(c.Bounds())
	absolute := Rect{X: c.originX + local.X, Y: c.originY + local.Y, W: local.W, H: local.H}
	return &Canvas{
		buffer:      c.buffer,
		originX:     absolute.X,
		originY:     absolute.Y,
		width:       local.W,
		height:      local.H,
		clip:        absolute.Intersect(c.clip),
		widthPolicy: c.widthPolicy,
	}
}

// Clip restricts drawing to rect while preserving c's coordinate system.
func (c *Canvas) Clip(rect Rect) *Canvas {
	if c == nil {
		return &Canvas{}
	}
	local := rect.Intersect(c.Bounds())
	absolute := Rect{X: c.originX + local.X, Y: c.originY + local.Y, W: local.W, H: local.H}
	clipped := *c
	clipped.clip = c.clip.Intersect(absolute)
	return &clipped
}

// Clear fills the canvas with styled blanks.
func (c *Canvas) Clear(style Style) { c.Fill(c.Bounds(), " ", style) }

// Fill tiles rect with the first grapheme in grapheme. A wide grapheme leaves
// a styled blank when a final odd cell cannot contain it.
func (c *Canvas) Fill(rect Rect, grapheme string, style Style) {
	if c == nil || c.buffer == nil {
		return
	}
	grapheme, width := firstGrapheme(grapheme, c.widthPolicy)
	if width == 0 || !printable(grapheme) {
		return
	}
	rect = rect.Intersect(c.Bounds())
	for y := rect.Y; y < rect.Bottom(); y++ {
		for x := rect.X; x < rect.Right(); x += width {
			if x+width <= rect.Right() {
				c.Put(x, y, grapheme, style)
			} else {
				c.Put(x, y, " ", style)
			}
		}
	}
}

// Put writes the first grapheme at a local coordinate and returns its cursor
// advance. Wide graphemes are never split by a canvas or clipping edge.
func (c *Canvas) Put(x, y int, grapheme string, style Style) int {
	return c.put(x, y, grapheme, style, Link{})
}

// PutCell writes a cell's grapheme, style, and hyperlink at a local
// coordinate and returns the cursor advance. It is the only public way to
// attach a Link, since the grapheme's terminal width is derived rather than
// trusted from the cell. Continuation cells have no grapheme and write
// nothing.
func (c *Canvas) PutCell(x, y int, cell Cell) int {
	return c.put(x, y, cell.Grapheme, cell.Style, cell.Link)
}

func (c *Canvas) put(x, y int, grapheme string, style Style, link Link) int {
	grapheme, width := firstGrapheme(grapheme, c.WidthPolicy())
	if c == nil || c.buffer == nil || grapheme == "" || !printable(grapheme) {
		return width
	}
	if width == 0 {
		c.appendCombining(x, y, grapheme)
		return 0
	}
	if y < 0 || y >= c.height || x < 0 || x+width > c.width {
		return width
	}
	absoluteX, absoluteY := c.originX+x, c.originY+y
	if !c.clip.Contains(absoluteX, absoluteY) || !c.clip.Contains(absoluteX+width-1, absoluteY) {
		return width
	}
	if !c.buffer.replacementFits(c.clip, absoluteX, absoluteY, width) {
		return width
	}
	c.buffer.putLink(absoluteX, absoluteY, grapheme, width, style, link)
	return width
}

// Text draws text with grapheme-aware widths, newlines, carriage returns and
// four-column tab stops. It returns the widest input line.
func (c *Canvas) Text(x, y int, text string, style Style) int {
	startX, cursorX, cursorY, widest := x, x, y, 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch grapheme {
		case "\n", "\r\n":
			widest = max(widest, cursorX-startX)
			cursorX, cursorY = startX, cursorY+1
		case "\r":
			widest = max(widest, cursorX-startX)
			cursorX = startX
		case "\t":
			spaces := 4 - (cursorX-startX)%4
			for range spaces {
				c.Put(cursorX, cursorY, " ", style)
				cursorX++
			}
		default:
			if !printable(grapheme) {
				continue
			}
			width := terminalWidthOf(grapheme, c.WidthPolicy())
			if width == 0 {
				c.appendCombining(cursorX, cursorY, grapheme)
			} else {
				c.Put(cursorX, cursorY, grapheme, style)
				cursorX += width
			}
		}
	}
	return max(widest, cursorX-startX)
}

// Border draws a border inside rect and degrades to a line or one cell for
// tiny rectangles.
func (c *Canvas) Border(rect Rect, border Border, style Style) {
	if c == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if rect.W == 1 && rect.H == 1 {
		c.Put(rect.X, rect.Y, border.TopLeft, style)
		return
	}
	if rect.H == 1 {
		c.Fill(rect, border.Top, style)
		return
	}
	if rect.W == 1 {
		c.Fill(rect, border.Left, style)
		return
	}
	c.Put(rect.X, rect.Y, border.TopLeft, style)
	c.Put(rect.Right()-1, rect.Y, border.TopRight, style)
	c.Put(rect.X, rect.Bottom()-1, border.BottomLeft, style)
	c.Put(rect.Right()-1, rect.Bottom()-1, border.BottomRight, style)
	if rect.W > 2 {
		c.Fill(Rect{X: rect.X + 1, Y: rect.Y, W: rect.W - 2, H: 1}, border.Top, style)
		c.Fill(Rect{X: rect.X + 1, Y: rect.Bottom() - 1, W: rect.W - 2, H: 1}, border.Bottom, style)
	}
	if rect.H > 2 {
		c.Fill(Rect{X: rect.X, Y: rect.Y + 1, W: 1, H: rect.H - 2}, border.Left, style)
		c.Fill(Rect{X: rect.Right() - 1, Y: rect.Y + 1, W: 1, H: rect.H - 2}, border.Right, style)
	}
}

func (c *Canvas) appendCombining(x, y int, grapheme string) {
	if c == nil || c.buffer == nil || y < 0 || y >= c.height || x <= 0 || x > c.width {
		return
	}
	absoluteX, absoluteY := c.originX+x-1, c.originY+y
	cell, ok := c.buffer.CellOK(absoluteX, absoluteY)
	if !ok {
		return
	}
	if cell.Width == 0 {
		absoluteX--
	}
	if c.clip.Contains(absoluteX, absoluteY) {
		c.buffer.appendCombining(absoluteX, absoluteY, grapheme)
	}
}

func printable(grapheme string) bool {
	if !utf8.ValidString(grapheme) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(grapheme)
	return !unicode.IsControl(r)
}
