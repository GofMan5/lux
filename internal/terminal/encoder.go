package terminal

import (
	"strconv"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/render"
)

const (
	resetStyle = "\x1b[0m"
	showCaret  = "\x1b[?25h"
	hideCaret  = "\x1b[?25l"
	beginSync  = "\x1b[?2026h"
	endSync    = "\x1b[?2026l"
	eraseLine  = "\x1b[K"
	dropRegion = "\x1b[r"
	// OSC 8 opens and closes hyperlink regions. The string terminator is the
	// standard one, and the close form carries empty params and no URI.
	linkOpen  = "\x1b]8;"
	linkEnd   = "\x1b\\"
	linkClose = "\x1b]8;;\x1b\\"
)

// maxTrackedRows bounds the stack storage the scroll detector uses. A taller
// frame skips the optimization rather than allocate for it.
const maxTrackedRows = 512

// minScrollRows is the net number of rows a shift must repair before the region
// sequences pay for themselves.
const minScrollRows = 3

// appendFrame encodes the difference between terminal, which models what the
// terminal currently shows, and frame. A scrolling region moves rows the
// encoder never transmits, so terminal is updated to match: a caller that fails
// to write the result must discard its model instead of reusing it.
//
// scrollRegions gates the DECSTBM path: terminals that do not implement it get
// a row-diff repaint of the same frame, which is larger but never corrupts.
//
// links gates OSC 8 hyperlinks. The hyperlink specification defines no
// detection, so the caller opts in: frames encoded without links never emit
// OSC 8 and never let cell links influence the diff, which keeps the output
// byte-identical to a build that has no link support at all.
func appendFrame(dst []byte, frame, terminal *render.Buffer, depth capability.ColorDepth, scrollRegions, synchronized, links bool) []byte {
	width, height := frame.Size()
	previousWidth, previousHeight := terminal.Size()
	repaint := terminal == nil || width != previousWidth || height != previousHeight
	start := len(dst)
	if synchronized {
		dst = append(dst, beginSync...)
	}
	wrote := false
	style := styleWriter{links: links}
	var at cursor
	var dirty rowBits
	// Most rows of most frames are untouched, and proving that one row at a time
	// is far cheaper than proving it one cell at a time. Taller frames fall back
	// to comparing each row as the diff reaches it.
	tracked := !repaint && height <= maxTrackedRows
	if tracked {
		dirty.mark(frame, terminal, height)
		if scrollRegions && height > 1 && dirty.count >= minScrollRows {
			if top, bottom, delta, ok := detectScroll(frame, terminal, depth, width, height); ok {
				// Exposed rows are filled with the active background on terminals
				// with background-color erase, so the style is reset first.
				dst = style.reset(dst)
				dst = appendScrollRegion(dst, top, bottom, delta)
				terminal.ScrollRows(top, bottom, delta)
				dirty.mark(frame, terminal, height)
				at.known = false
				wrote = true
			}
		}
	}
	for y := 0; y < height; y++ {
		if tracked && !dirty.has(y) || !tracked && !repaint && frame.EqualRow(terminal, y) {
			continue
		}
		blankFrom := -1
		for x := 0; x < width; {
			cell := frame.Cell(x, y)
			if cell.Width == 0 || (!repaint && equalRenderedCell(frame, terminal, x, y, depth, links)) {
				x++
				continue
			}
			if blankFrom < 0 {
				blankFrom = erasableTail(frame, y, width, links)
			}
			runStart := x
			for x < width {
				cell = frame.Cell(x, y)
				if cell.Width == 0 {
					x++
					continue
				}
				if !repaint && equalRenderedCell(frame, terminal, x, y, depth, links) {
					break
				}
				x += max(int(cell.Width), 1)
			}
			dst = at.moveTo(dst, runStart, y)
			wrote = true
			column, erased := runStart, false
			for column < x {
				// Erasing the tail is shorter than transmitting its blanks, and it
				// also covers every later run on this row, which the frame wants
				// blank as well.
				if column >= blankFrom && x-column > len(eraseLine) {
					dst = style.apply(dst, render.Style{}, render.Link{}, depth)
					dst = append(dst, eraseLine...)
					erased = true
					break
				}
				cell = frame.Cell(column, y)
				if cell.Width == 0 {
					column++
					continue
				}
				dst = style.apply(dst, cell.Style, cell.Link, depth)
				grapheme := cell.Grapheme
				if grapheme == "" {
					grapheme = " "
				}
				dst = append(dst, grapheme...)
				column += max(int(cell.Width), 1)
			}
			at.arrive(column, y, width)
			if erased {
				break
			}
		}
	}
	// The frame never leaves a hyperlink region open: whatever the next
	// writer does — a later frame, a shell prompt, anything — must not
	// inherit the link.
	dst = style.endFrame(dst)
	caretX, caretY, caretOn := frame.Caret()
	oldX, oldY, caretWasOn := terminal.Caret()
	caretMoved := caretOn != caretWasOn || caretOn && (caretX != oldX || caretY != oldY)
	if !wrote && !caretMoved {
		return dst[:start]
	}
	switch {
	case caretOn:
		// Cell runs left the cursor wherever the last run ended, so the caret is
		// repositioned whenever anything was written, not only when it moved.
		dst = at.moveTo(dst, caretX, caretY)
		if !caretWasOn {
			dst = append(dst, showCaret...)
		}
	case caretWasOn:
		dst = append(dst, hideCaret...)
	}
	if synchronized {
		dst = append(dst, endSync...)
	}
	return dst
}

// erasableTail returns the first column of the trailing run of default-styled
// blanks on row y, or width when the row does not end in one. Erase-to-end-of-
// line is only equivalent to those cells: a background-color-erase terminal
// fills with the active background, so the encoder resets the style first and
// refuses tails that carry a style — or, with links on, a hyperlink — of
// their own.
func erasableTail(frame *render.Buffer, y, width int, links bool) int {
	column := width
	for column > 0 {
		cell := frame.Cell(column-1, y)
		if cell.Width != 1 || cell.Style != (render.Style{}) || (cell.Grapheme != " " && cell.Grapheme != "") {
			break
		}
		if links && !cell.Link.Empty() {
			// Erase does not carry a link, so a linked blank would stop
			// matching the erased cells.
			break
		}
		column--
	}
	return column
}

// rowBits records which rows differ, so the row comparison is paid for once
// even though both the scroll detector and the diff need the answer.
type rowBits struct {
	words [maxTrackedRows / 64]uint64
	count int
}

func (r *rowBits) mark(frame, terminal *render.Buffer, height int) {
	*r = rowBits{}
	for y := 0; y < height; y++ {
		if frame.EqualRow(terminal, y) {
			continue
		}
		r.words[y/64] |= 1 << uint(y%64)
		r.count++
	}
}

func (r *rowBits) has(y int) bool { return r.words[y/64]&(1<<uint(y%64)) != 0 }

// detectScroll looks for a vertical shift of full-width rows that the terminal
// can perform itself. Only full-width row ranges qualify, because a scrolling
// region has no portable horizontal margins.
//
// Rows are compared by hash, so a collision can only cost bytes: the emitted
// region is applied to the terminal model before the diff runs, and the diff
// then compares real cells.
func detectScroll(frame, terminal *render.Buffer, depth capability.ColorDepth, width, height int) (top, bottom, delta int, ok bool) {
	var frameRows, terminalRows [maxTrackedRows]uint64
	dirty := false
	for y := 0; y < height; y++ {
		frameRows[y] = hashRow(frame, y, width, depth)
		terminalRows[y] = hashRow(terminal, y, width, depth)
		dirty = dirty || frameRows[y] != terminalRows[y]
	}
	if !dirty {
		return 0, 0, 0, false
	}
	blank := hashBlankRow(width, depth)
	best := 0
	for candidate := -(height - 1); candidate <= height-1; candidate++ {
		if candidate == 0 {
			continue
		}
		first, last := -1, -1
		for y := max(0, -candidate); y < height && y+candidate < height; y++ {
			if frameRows[y] == terminalRows[y] || frameRows[y] != terminalRows[y+candidate] {
				continue
			}
			if first < 0 {
				first = y
			}
			last = y
		}
		if first < 0 {
			continue
		}
		regionTop, regionBottom := first, last
		if candidate > 0 {
			regionBottom = min(last+candidate, height-1)
		} else {
			regionTop = max(first+candidate, 0)
		}
		score := 0
		for y := regionTop; y <= regionBottom; y++ {
			after := blank
			if source := y + candidate; source >= regionTop && source <= regionBottom {
				after = terminalRows[source]
			}
			switch {
			case frameRows[y] != terminalRows[y] && frameRows[y] == after:
				score++
			case frameRows[y] == terminalRows[y] && frameRows[y] != after:
				score--
			}
		}
		if score > best {
			best, top, bottom, delta = score, regionTop, regionBottom, candidate
		}
	}
	return top, bottom, delta, best >= minScrollRows
}

// appendScrollRegion moves rows inside a scrolling region. Insert- and
// delete-line are used rather than SU and SD because DECSTBM with IL and DL
// is the subset even the Linux console implements. Terminals that rejected
// the mode during capability negotiation never reach this path: their frames
// are row-diffed instead, which costs bytes but not correctness.
func appendScrollRegion(dst []byte, top, bottom, delta int) []byte {
	dst = append(dst, '\x1b', '[')
	dst = strconv.AppendInt(dst, int64(top+1), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(bottom+1), 10)
	dst = append(dst, 'r')
	dst = appendCursor(dst, top+1, 1)
	dst = append(dst, '\x1b', '[')
	dst = strconv.AppendInt(dst, int64(max(delta, -delta)), 10)
	if delta > 0 {
		dst = append(dst, 'M')
	} else {
		dst = append(dst, 'L')
	}
	return append(dst, dropRegion...)
}

const (
	hashOffset = 14695981039346656037
	hashPrime  = 1099511628211
)

// hashSamples bounds how many cells identify a row. A scroll moves whole rows,
// so a spread sample distinguishes them well enough, and a collision can only
// cost bytes: the diff that follows the shift compares real cells.
const hashSamples = 32

func hashStride(width int) int { return max(1, (width+hashSamples-1)/hashSamples) }

func hashRow(buffer *render.Buffer, y, width int, depth capability.ColorDepth) uint64 {
	hash := uint64(hashOffset)
	for x, stride := 0, hashStride(width); x < width; x += stride {
		cell := buffer.Cell(x, y)
		hash = hashStyled(hash, cell.Grapheme, cell.Width, cell.Style, cell.Link, depth)
	}
	return hash
}

func hashBlankRow(width int, depth capability.ColorDepth) uint64 {
	hash := uint64(hashOffset)
	for x, stride := 0, hashStride(width); x < width; x += stride {
		hash = hashStyled(hash, render.BlankCell.Grapheme, render.BlankCell.Width, render.BlankCell.Style, render.BlankCell.Link, depth)
	}
	return hash
}

func hashStyled(hash uint64, grapheme string, width uint8, style render.Style, link render.Link, depth capability.ColorDepth) uint64 {
	for index := 0; index < len(grapheme); index++ {
		hash = (hash ^ uint64(grapheme[index])) * hashPrime
	}
	hash = (hash ^ uint64(width)) * hashPrime
	hash = (hash ^ uint64(style.Attrs)) * hashPrime
	if style.Attrs&render.AttrUnderline != 0 {
		// The underline shape rides the attribute, so it is part of the
		// rendered difference exactly when the attribute is on.
		hash = (hash ^ uint64(style.UnderlineStyle)) * hashPrime
	}
	// Links ride the cells whether or not the encoder transmits them: a row
	// that only changed its link is a different row, and a hash that is blind
	// to that can only cost bytes through a wasted scroll, never correctness.
	for index := 0; index < len(link.URI); index++ {
		hash = (hash ^ uint64(link.URI[index])) * hashPrime
	}
	for index := 0; index < len(link.ID); index++ {
		hash = (hash ^ uint64(link.ID[index])) * hashPrime
	}
	if depth == capability.Monochrome {
		// Colors are not transmitted, so two rows that differ only in color are
		// the same row as far as the encoder is concerned.
		return hash
	}
	foregroundMode, foregroundValue := renderedColor(style.Foreground, depth)
	backgroundMode, backgroundValue := renderedColor(style.Background, depth)
	hash = (hash ^ uint64(foregroundMode)) * hashPrime
	hash = (hash ^ uint64(foregroundValue)) * hashPrime
	hash = (hash ^ uint64(backgroundMode)) * hashPrime
	hash = (hash ^ uint64(backgroundValue)) * hashPrime
	if style.Attrs&render.AttrUnderline != 0 {
		// The underline color is transmitted like the text colors whenever
		// the underline is on.
		underlineMode, underlineValue := renderedColor(style.UnderlineColor, depth)
		hash = (hash ^ uint64(underlineMode)) * hashPrime
		hash = (hash ^ uint64(underlineValue)) * hashPrime
	}
	return hash
}

// cursor tracks where emitted output leaves the terminal cursor so the next
// move can be relative. It is deliberately conservative: any position the
// encoder cannot prove is forgotten, and absolute addressing is used instead.
type cursor struct {
	x, y  int
	known bool
}

func (c *cursor) moveTo(dst []byte, x, y int) []byte {
	if c.known && c.y == y {
		from, rewind := c.x, 0
		if x < from {
			// A carriage return is the cheapest way back to the left edge.
			from, rewind = 0, 1
		}
		if rewind+forwardCost(x-from) < 4+digits(y+1)+digits(x+1) {
			if rewind != 0 {
				dst = append(dst, '\r')
			}
			c.x = x
			return appendForward(dst, x-from)
		}
	}
	c.x, c.y, c.known = x, y, true
	return appendCursor(dst, y+1, x+1)
}

// arrive records where printing left the cursor. A run that reached the right
// edge leaves the terminal in its pending-wrap state, whose column is not
// portable, so the position is forgotten rather than guessed.
func (c *cursor) arrive(x, y, width int) {
	c.x, c.y, c.known = x, y, x < width
}

func forwardCost(steps int) int {
	switch {
	case steps <= 0:
		return 0
	case steps == 1:
		return 3
	default:
		return 3 + digits(steps)
	}
}

func appendForward(dst []byte, steps int) []byte {
	if steps <= 0 {
		return dst
	}
	dst = append(dst, '\x1b', '[')
	if steps > 1 {
		dst = strconv.AppendInt(dst, int64(steps), 10)
	}
	return append(dst, 'C')
}

func digits(value int) int {
	count := 1
	for value >= 10 {
		value /= 10
		count++
	}
	return count
}

func equalRenderedCell(frame, terminal *render.Buffer, x, y int, depth capability.ColorDepth, links bool) bool {
	current, currentOK := frame.CellOK(x, y)
	old, oldOK := terminal.CellOK(x, y)
	if currentOK != oldOK || !currentOK {
		return false
	}
	if current == old {
		// Identical cells always render identically, and most cells in a frame
		// are identical, so the downgrade comparison is only reached by the few
		// that actually differ.
		return true
	}
	if current.Grapheme != old.Grapheme || current.Width != old.Width || current.Style.Attrs != old.Style.Attrs {
		return false
	}
	if links && current.Link != old.Link {
		return false
	}
	if current.Style.Attrs&render.AttrUnderline != 0 {
		// The underline shape and color ride the attribute: without it they are
		// invisible, so cells that differ only there still match.
		if current.Style.UnderlineStyle != old.Style.UnderlineStyle {
			return false
		}
		if !equalRenderedColor(current.Style.UnderlineColor, old.Style.UnderlineColor, depth) {
			return false
		}
	}
	return equalRenderedColor(current.Style.Foreground, old.Style.Foreground, depth) && equalRenderedColor(current.Style.Background, old.Style.Background, depth)
}

func equalRenderedColor(left, right render.Color, depth capability.ColorDepth) bool {
	if depth == capability.Monochrome {
		return true
	}
	leftMode, leftValue := renderedColor(left, depth)
	rightMode, rightValue := renderedColor(right, depth)
	return leftMode == rightMode && leftValue == rightValue
}

func renderedColor(color render.Color, depth capability.ColorDepth) (render.ColorMode, uint32) {
	if color.Mode == render.ColorDefault {
		return render.ColorDefault, 0
	}
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			return render.ColorRGB, color.Value
		case capability.ANSI256:
			return render.ColorIndexed, uint32(rgb256(red, green, blue))
		default:
			return render.ColorANSI, uint32(rgb16(red, green, blue))
		}
	}
	if color.Mode == render.ColorIndexed && depth == capability.ANSI16 {
		return render.ColorANSI, uint32(indexed16(uint8(color.Value)))
	}
	if color.Mode == render.ColorANSI {
		return render.ColorANSI, color.Value & 15
	}
	return color.Mode, color.Value
}

func appendCursor(dst []byte, row, column int) []byte {
	dst = append(dst, '\x1b', '[')
	dst = strconv.AppendInt(dst, int64(row), 10)
	dst = append(dst, ';')
	dst = strconv.AppendInt(dst, int64(column), 10)
	return append(dst, 'H')
}

var attrCodes = [...]struct {
	mask render.Attr
	on   string
	off  string
}{
	{render.AttrBold, "1", "22"}, {render.AttrDim, "2", "22"}, {render.AttrItalic, "3", "23"},
	{render.AttrUnderline, "4", "24"}, {render.AttrBlink, "5", "25"}, {render.AttrReverse, "7", "27"},
	{render.AttrHidden, "8", "28"}, {render.AttrStrike, "9", "29"},
}

// intensity is the pair SGR 22 turns off together, so clearing one restates the
// other.
const intensity = render.AttrBold | render.AttrDim

// underlineCodes maps an UnderlineStyle to its SGR parameter, indexed by the
// style value. Single keeps the plain code the attribute table already
// carries; the rest ride the subparameter form, which terminals without styled
// underlines read as a plain underline when the plain code precedes them.
var underlineCodes = [...]string{"4", "4:2", "4:3", "4:4", "4:5"}

// underlineCode clamps values outside the enum to the plain underline, which
// is also what the zero value does.
func underlineCode(style render.UnderlineStyle) string {
	if int(style) < len(underlineCodes) {
		return underlineCodes[style]
	}
	return underlineCodes[render.UnderlineSingle]
}

// styleWriter emits the shortest SGR that turns the style the terminal already
// carries into the one a cell needs. A delta is usually shorter than restating
// the whole style, but not always: clearing several attributes costs more than
// a reset, so both forms are built and the shorter one is written. The scratch
// arrays hold the longest form either can produce, so building both is free.
//
// The writer also models the state SGR cannot reach: the OSC 8 region the
// terminal is inside, and the underline color it carries. SGR 24 turns the
// attribute off without resetting the color, and SGR 0 ends neither, so both
// are tracked as transmitted.
type styleWriter struct {
	current  render.Style
	link     render.Link
	links    bool
	known    bool
	fullBuf  [96]byte
	deltaBuf [96]byte
}

func (w *styleWriter) apply(dst []byte, style render.Style, link render.Link, depth capability.ColorDepth) []byte {
	if !w.links {
		link = render.Link{}
	}
	if w.known && w.current == style && w.link == link {
		return dst
	}
	if w.link != link {
		dst = w.setLink(dst, link)
	}
	if w.known && w.current == style {
		return dst
	}
	full := appendFullStyle(w.fullBuf[:0], style, depth)
	if w.known {
		// An empty delta means the terminal already renders this style at the
		// active color depth, which is why it is compared before the full form.
		before := w.current
		if delta := appendStyleDelta(w.deltaBuf[:0], before, style, depth); len(delta) < len(full) {
			w.current = style
			if style.Attrs&render.AttrUnderline == 0 || equalRenderedColor(before.UnderlineColor, style.UnderlineColor, depth) {
				// No underline color was transmitted, and SGR 24 does not
				// reset one, so the terminal keeps the color it had.
				w.current.UnderlineColor = before.UnderlineColor
			}
			return append(dst, delta...)
		}
	}
	w.current, w.known = style, true
	if style.Attrs&render.AttrUnderline == 0 {
		// SGR 0 reset the underline color, and none is transmitted without
		// the attribute, so the model records the default.
		w.current.UnderlineColor = render.Color{}
	}
	return append(dst, full...)
}

func (w *styleWriter) isDefault() bool { return w.known && w.current == render.Style{} }

// reset puts the terminal back to the default style, which is what an erase or
// a scrolling region needs before it exposes cells the encoder will not write.
func (w *styleWriter) reset(dst []byte) []byte {
	if !w.link.Empty() {
		// SGR 0 does not end a hyperlink region, and the erased or scrolled
		// cells would otherwise inherit the open link.
		dst = w.setLink(dst, render.Link{})
	}
	if w.isDefault() {
		return dst
	}
	w.current, w.known = render.Style{}, true
	return append(dst, resetStyle...)
}

// setLink switches the terminal into link's region, or closes the region when
// the link is empty. Switching straight from one target to another is legal,
// so no intermediate close is emitted, and an id travels only when the link
// carries one.
func (w *styleWriter) setLink(dst []byte, link render.Link) []byte {
	w.link = link
	if !w.links {
		return dst
	}
	if link.Empty() {
		return append(dst, linkClose...)
	}
	dst = append(dst, linkOpen...)
	if link.ID != "" {
		dst = append(dst, "id="...)
		dst = append(dst, link.ID...)
	}
	dst = append(dst, ';')
	dst = append(dst, link.URI...)
	return append(dst, linkEnd...)
}

// endFrame closes any hyperlink region the frame opened, so output the encoder
// does not write inherits nothing.
func (w *styleWriter) endFrame(dst []byte) []byte {
	if w.link.Empty() {
		return dst
	}
	return w.setLink(dst, render.Link{})
}

func appendFullStyle(dst []byte, style render.Style, depth capability.ColorDepth) []byte {
	dst = append(dst, '\x1b', '[', '0')
	first := false
	for _, attribute := range attrCodes {
		if style.Attrs&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.on)
		}
	}
	if style.Attrs&render.AttrUnderline != 0 {
		if style.UnderlineStyle != render.UnderlineSingle {
			// The plain code from the table keeps the underline on terminals
			// that ignore subparameters; the styled code refines the shape.
			dst = appendParam(dst, &first, underlineCode(style.UnderlineStyle))
		}
		dst = appendUnderlineColor(dst, &first, style.UnderlineColor, depth)
	}
	dst = appendColor(dst, &first, style.Foreground, depth, true)
	dst = appendColor(dst, &first, style.Background, depth, false)
	return append(dst, 'm')
}

func appendStyleDelta(dst []byte, from, to render.Style, depth capability.ColorDepth) []byte {
	start := len(dst)
	dst = append(dst, '\x1b', '[')
	first := true
	cleared, added := from.Attrs&^to.Attrs, to.Attrs&^from.Attrs
	if cleared&intensity != 0 {
		dst = appendParam(dst, &first, "22")
		added |= to.Attrs & intensity
	}
	for _, attribute := range attrCodes {
		if attribute.mask&intensity == 0 && cleared&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.off)
		}
	}
	for _, attribute := range attrCodes {
		if added&attribute.mask != 0 {
			dst = appendParam(dst, &first, attribute.on)
		}
	}
	// The styled underline is restated whenever the terminal cannot be proven
	// to already carry it. Subparameters have no off form, so the plain code
	// leads: a terminal that ignores them keeps a plain underline, and one
	// that answers them refines the shape. Turning back to the straight
	// underline re-issues the plain code, which sets the shape as well.
	fromShape := render.UnderlineSingle
	if from.Attrs&render.AttrUnderline != 0 {
		fromShape = from.UnderlineStyle
	}
	if to.Attrs&render.AttrUnderline != 0 && to.UnderlineStyle != fromShape {
		if added&render.AttrUnderline == 0 {
			dst = appendParam(dst, &first, "4")
		}
		if code := underlineCode(to.UnderlineStyle); code != "4" {
			dst = appendParam(dst, &first, code)
		}
	}
	if to.Attrs&render.AttrUnderline != 0 && !equalRenderedColor(from.UnderlineColor, to.UnderlineColor, depth) {
		dst = appendUnderlineColorChange(dst, &first, to.UnderlineColor, depth)
	}
	if !equalRenderedColor(from.Foreground, to.Foreground, depth) {
		dst = appendColorChange(dst, &first, to.Foreground, depth, true)
	}
	if !equalRenderedColor(from.Background, to.Background, depth) {
		dst = appendColorChange(dst, &first, to.Background, depth, false)
	}
	if first {
		return dst[:start]
	}
	return append(dst, 'm')
}

func appendColorChange(dst []byte, first *bool, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
	if color.Mode == render.ColorDefault {
		if foreground {
			return appendParam(dst, first, "39")
		}
		return appendParam(dst, first, "49")
	}
	return appendColor(dst, first, color, depth, foreground)
}

// appendUnderlineColorChange is the delta form for SGR 58/59: the underline
// color follows the same addressing scheme as the text colors, and 59 returns
// it to the terminal default.
func appendUnderlineColorChange(dst []byte, first *bool, color render.Color, depth capability.ColorDepth) []byte {
	if color.Mode == render.ColorDefault {
		return appendParam(dst, first, "59")
	}
	return appendUnderlineColor(dst, first, color, depth)
}

// appendUnderlineColor transmits the underline color. SGR 58 has no basic
// form, so unlike a text color it always rides the palette addressing, which
// every terminal that answers 58 accepts.
func appendUnderlineColor(dst []byte, first *bool, color render.Color, depth capability.ColorDepth) []byte {
	if color.Mode == render.ColorDefault || depth == capability.Monochrome {
		return dst
	}
	index := uint8(color.Value)
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			dst = appendSeparator(dst, first)
			dst = append(dst, "58;2;"...)
			dst = strconv.AppendInt(dst, int64(red), 10)
			dst = append(dst, ';')
			dst = strconv.AppendInt(dst, int64(green), 10)
			dst = append(dst, ';')
			return strconv.AppendInt(dst, int64(blue), 10)
		case capability.ANSI256:
			index = rgb256(red, green, blue)
		default:
			index = rgb16(red, green, blue)
		}
	} else if color.Mode == render.ColorIndexed && depth == capability.ANSI16 {
		index = indexed16(index)
	}
	dst = appendSeparator(dst, first)
	dst = append(dst, "58;5;"...)
	return strconv.AppendInt(dst, int64(index), 10)
}

func appendParam(dst []byte, first *bool, code string) []byte {
	return append(appendSeparator(dst, first), code...)
}

func appendSeparator(dst []byte, first *bool) []byte {
	if !*first {
		dst = append(dst, ';')
	}
	*first = false
	return dst
}

func appendColor(dst []byte, first *bool, color render.Color, depth capability.ColorDepth, foreground bool) []byte {
	if color.Mode == render.ColorDefault || depth == capability.Monochrome {
		return dst
	}
	index := uint8(color.Value)
	if color.Mode == render.ColorRGB {
		red, green, blue, _ := color.RGB()
		switch depth {
		case capability.TrueColor:
			dst = appendSeparator(dst, first)
			if foreground {
				dst = append(dst, "38;2;"...)
			} else {
				dst = append(dst, "48;2;"...)
			}
			dst = strconv.AppendInt(dst, int64(red), 10)
			dst = append(dst, ';')
			dst = strconv.AppendInt(dst, int64(green), 10)
			dst = append(dst, ';')
			return strconv.AppendInt(dst, int64(blue), 10)
		case capability.ANSI256:
			index = rgb256(red, green, blue)
		default:
			index = rgb16(red, green, blue)
		}
	} else if color.Mode == render.ColorIndexed && depth == capability.ANSI16 {
		index = indexed16(index)
	}
	if depth >= capability.ANSI256 && (color.Mode == render.ColorIndexed || color.Mode == render.ColorRGB) {
		dst = appendSeparator(dst, first)
		if foreground {
			dst = append(dst, "38;5;"...)
		} else {
			dst = append(dst, "48;5;"...)
		}
		return strconv.AppendInt(dst, int64(index), 10)
	}
	index &= 15
	code := 30 + int(index&7)
	if index >= 8 {
		code += 60
	}
	if !foreground {
		code += 10
	}
	dst = appendSeparator(dst, first)
	return strconv.AppendInt(dst, int64(code), 10)
}

func rgb256(red, green, blue uint8) uint8 {
	if red == green && green == blue {
		if red < 8 {
			return 16
		}
		if red > 248 {
			return 231
		}
		return uint8(232 + (int(red)-8)*24/247)
	}
	r := (int(red)*5 + 127) / 255
	g := (int(green)*5 + 127) / 255
	b := (int(blue)*5 + 127) / 255
	return uint8(16 + 36*r + 6*g + b)
}

func rgb16(red, green, blue uint8) uint8 {
	index := uint8(0)
	if red >= 128 {
		index |= 1
	}
	if green >= 128 {
		index |= 2
	}
	if blue >= 128 {
		index |= 4
	}
	if max(red, max(green, blue)) >= 192 {
		index |= 8
	}
	return index
}

func indexed16(index uint8) uint8 {
	if index < 16 {
		return index
	}
	if index >= 232 {
		gray := uint8(8 + (int(index)-232)*10)
		return rgb16(gray, gray, gray)
	}
	index -= 16
	return rgb16(uint8(index/36)*51, uint8(index/6%6)*51, uint8(index%6)*51)
}

func appendPlain(dst []byte, frame *render.Buffer) []byte {
	width, height := frame.Size()
	for y := 0; y < height; y++ {
		end := width
		for end > 0 {
			cell := frame.Cell(end-1, y)
			if cell.Width == 0 {
				end--
				continue
			}
			if cell.Grapheme != "" && cell.Grapheme != " " {
				break
			}
			end--
		}
		for x := 0; x < end; {
			cell := frame.Cell(x, y)
			if cell.Width == 0 {
				x++
				continue
			}
			if cell.Grapheme == "" {
				dst = append(dst, ' ')
			} else {
				dst = append(dst, cell.Grapheme...)
			}
			x += max(int(cell.Width), 1)
		}
		dst = append(dst, '\n')
	}
	return dst
}
