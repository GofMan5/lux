package ui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/rivo/uniseg"
)

// TextArea is the editable state of a soft-wrapped multiline text field.
// Lines holds the logical lines with no embedded newlines; the caret is the
// Line index plus a UTF-8 byte Offset at an extended-grapheme boundary within
// that line, and AnchorLine/AnchorOffset mark the selection's other end.
// Equal caret and anchor mean no selection. Wrap selects the soft-wrap mode
// used for layout; the wrap width is whatever the widget was last drawn at.
// The zero value is ready and empty.
type TextArea struct {
	Lines        []string
	Line         int
	Offset       int
	AnchorLine   int
	AnchorOffset int
	MaxBytes     int
	UndoBytes    int
	Wrap         render.WrapMode

	// Scroll is the vertical viewport over the wrapped rows. Drawing keeps
	// the caret inside it; wheel and scroll keys move it independently.
	Scroll ScrollState

	// widthPolicy and layout mirror the canvas the field was last drawn on,
	// so event-time cell math (arrows, mouse mapping) measures the same
	// widths the user saw painted. A field that was never drawn lays out
	// with hard lines only until the first frame.
	widthPolicy render.WidthPolicy
	layout      textAreaLayout

	// goalCol is the sticky column Up and Down return to; -1 means recompute
	// it from the caret before the next vertical move.
	goalCol int

	undo      []textAreaSnapshot
	redo      []textAreaSnapshot
	undoBytes int
	redoBytes int
}

// textAreaLayout is the cached soft wrap of the current content. Rows of one
// logical line are contiguous input slices: rows[i][r] equals
// Lines[i][starts[i][r] : starts[i][r]+len(rows[i][r])], byte for byte; only
// between rows can word-mode whitespace disappear. That contiguity is what
// caret mapping, mouse mapping, and selection painting all lean on.
type textAreaLayout struct {
	width     int // wrap width in cells; 0 lays out hard lines only
	mode      render.WrapMode
	policy    render.WidthPolicy
	valid     bool
	dirtyFrom int // first logical line whose rows need rebuilding
	dirtyTo   int // one past the last dirty logical line

	rows      [][]string
	starts    [][]int
	rowOfLine []int
	totalRows int
}

type textAreaSnapshot struct {
	lines                                  []string
	line, offset, anchorLine, anchorOffset int
}

// TextAreaStyle controls painting and whether an area accepts interaction.
// Zero Selection and Cursor styles inherit Normal with reverse video.
type TextAreaStyle struct {
	Normal    render.Style
	Selection render.Style
	Cursor    render.Style
	ReadOnly  bool
	Disabled  bool
}

// SetValue replaces the document, applies the multiline input policy, moves
// the caret to the end, and clears selection and history.
func (t *TextArea) SetValue(value string) {
	if t == nil {
		return
	}
	lines := sanitizeMultiline(value)
	lines = limitLines(lines, t.MaxBytes)
	if len(lines) == 0 {
		lines = []string{""}
	}
	t.Lines = lines
	t.Line = len(t.Lines) - 1
	t.Offset = len(t.Lines[t.Line])
	t.AnchorLine, t.AnchorOffset = t.Line, t.Offset
	t.clearHistory()
	t.invalidate()
}

// Text returns the document with logical lines joined by newlines.
func (t *TextArea) Text() string {
	if t == nil || len(t.Lines) == 0 {
		return ""
	}
	return strings.Join(t.Lines, "\n")
}

// Selection returns the ordered selected position range as
// (startLine, startOffset, endLine, endOffset). Equal positions mean no
// selection.
func (t *TextArea) Selection() (startLine, startOffset, endLine, endOffset int) {
	if t == nil {
		return 0, 0, 0, 0
	}
	t.normalize()
	if caretBefore(t.Line, t.Offset, t.AnchorLine, t.AnchorOffset) {
		return t.Line, t.Offset, t.AnchorLine, t.AnchorOffset
	}
	return t.AnchorLine, t.AnchorOffset, t.Line, t.Offset
}

// SelectedText returns a detached copy of the selected text with the deleted
// line breaks restored.
func (t *TextArea) SelectedText() string {
	if t == nil {
		return ""
	}
	sL, sO, eL, eO := t.Selection()
	if sL == eL {
		return strings.Clone(t.Lines[sL][sO:eO])
	}
	var out strings.Builder
	out.WriteString(t.Lines[sL][sO:])
	for line := sL + 1; line < eL; line++ {
		out.WriteByte('\n')
		out.WriteString(t.Lines[line])
	}
	out.WriteByte('\n')
	out.WriteString(t.Lines[eL][:eO])
	return out.String()
}

// ReplaceSelection replaces the selection as one undoable edit. An empty
// value deletes the selection.
func (t *TextArea) ReplaceSelection(value string) bool {
	if t == nil {
		return false
	}
	t.normalize()
	t.prepareHistory()
	before := t.snapshot()
	state := t.state()
	t.insertLines(value)
	t.recordEdit(before)
	return state.changed(t)
}

// Undo restores the last content-changing edit.
func (t *TextArea) Undo() bool {
	if t == nil {
		return false
	}
	t.normalize()
	t.prepareHistory()
	previous, ok := t.popHistory(&t.undo, &t.undoBytes)
	if !ok {
		return false
	}
	t.pushHistory(&t.redo, &t.redoBytes, t.snapshot())
	t.restore(previous)
	return true
}

// Redo reapplies the last undone edit.
func (t *TextArea) Redo() bool {
	if t == nil {
		return false
	}
	t.normalize()
	t.prepareHistory()
	next, ok := t.popHistory(&t.redo, &t.redoBytes)
	if !ok {
		return false
	}
	t.pushHistory(&t.undo, &t.undoBytes, t.snapshot())
	t.restore(next)
	return true
}

// Apply applies one routed Input or Scroll action and reports whether state
// changed. Input covers committed text, multiline paste, navigation and
// deletion over line breaks, Enter, Ctrl+A, Ctrl+Z/Y, UAX #29 word movement,
// vertical movement over wrapped rows, and local mouse placement. Scroll
// covers the wheel and scroll keys over the wrapped rows. Key releases are
// inert.
func (t *TextArea) Apply(action Action) bool {
	if t == nil {
		return false
	}
	if action.Kind == Scroll {
		return t.applyScroll(action)
	}
	if action.Kind != Input {
		return false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false
	}
	original := t.state()
	t.normalize()
	t.prepareHistory()
	if action.Event.Kind == event.Key {
		if handled, changed := t.applyHistoryKey(action.Event.Key); handled {
			return changed || original.changed(t)
		}
	}
	before := t.snapshot()
	switch action.Event.Kind {
	case event.Text, event.Paste:
		t.insertLines(action.Event.Text)
	case event.Key:
		t.applyKey(action.Event.Key, action.Bounds)
	case event.Mouse:
		t.applyMouse(action)
	}
	t.recordEdit(before)
	return original.changed(t)
}

// TextArea registers and paints a soft-wrapped multiline editor. Read-only
// areas remain focusable but do not receive Input actions; disabled areas are
// inert. Drawing lays the content out at the clipped width, keeps the caret
// row inside the viewport, and draws only the visible wrapped rows.
func (c *Context) TextArea(id ID, canvas *render.Canvas, rect render.Rect, area *TextArea, style TextAreaStyle) {
	if c == nil || canvas == nil || area == nil || id == "" {
		return
	}
	area.widthPolicy = canvas.WidthPolicy()
	area.normalize()
	line := rect.Intersect(canvas.Bounds())
	area.buildLayout(max(line.W, 0), canvas.WidthPolicy())
	area.ensureLayout()

	flags := Focusable | Editable | Scrollable
	if style.ReadOnly {
		flags = Focusable
	}
	if style.Disabled {
		flags |= Disabled
	}
	// Tab can move focus to a row this viewport has scrolled out of view, so
	// reveal it before the visible range is measured.
	area.Scroll.Offset += c.revealDelta(id)
	rows := max(line.H, 0)
	// Focus is only authoritative once this region has registered and claimed
	// it, but the caret reveal must run first to record correct content
	// limits. Until anything claims focus, the next focusable region will, so
	// the prediction holds for the first frame and stays false afterwards.
	focused := (c.Focused(id) || c.focusPending()) && !style.ReadOnly && !style.Disabled
	if focused {
		caretRow, _ := area.layout.caretPosition(area.Line, area.Offset)
		area.Scroll.EnsureVisible(caretRow, area.layout.totalRows, rows)
	}
	visible := area.Scroll.Range(area.layout.totalRows, rows)
	// The full rectangle is registered, not the clipped remainder, so a
	// container that scrolled the area away can still reach and reveal it.
	c.RegionWithOptions(id, canvas, rect, flags, RegionOptions{
		ContentHeight: visible.Last - visible.First,
		AtTop:         area.Scroll.Offset <= 0,
		AtBottom:      area.Scroll.Offset >= max(max(area.layout.totalRows, 0)-rows, 0),
	})
	focused = c.Focused(id) && !style.ReadOnly && !style.Disabled
	if line.Empty() || rows <= 0 {
		return
	}

	base := style.Normal
	if style.Disabled {
		base.Attrs |= render.AttrDim
	}
	canvas.Fill(line, " ", base)
	selection := style.Selection
	if selection == (render.Style{}) {
		selection = base
		selection.Attrs |= render.AttrReverse
	}
	caret := style.Cursor
	if caret == (render.Style{}) {
		caret = base
		caret.Attrs |= render.AttrReverse
	}

	sL, sO, eL, eO := area.Selection()
	// caretPosition maps the logical caret to a visual row and cell once;
	// every consumer below shares the same numbers so measure and paint agree.
	caretRow, caretCol := area.layout.caretPosition(area.Line, area.Offset)
	if !focused {
		caretRow, caretCol = -1, -1
	}
	for row := visible.First; row < visible.Last; row++ {
		l, r := area.layout.lineRowOf(row)
		if l < 0 {
			break
		}
		col := -1
		if row == caretRow {
			col = caretCol
		}
		drawAreaRow(canvas, line, row-visible.First, area.layout.rows[l][r], area.layout.starts[l][r],
			l, sL, sO, eL, eO, col, base, selection, caret)
	}
	if caretRow >= visible.First && caretRow < visible.Last {
		switch {
		case caretCol < line.W:
			canvas.SetCaret(line.X+caretCol, line.Y+caretRow-visible.First)
		case caretRow+1 < visible.Last:
			// The caret at the end of a width-filling row flows to the next
			// wrapped row's first cell; that is where the text continues.
			canvas.SetCaret(line.X, line.Y+caretRow+1-visible.First)
		}
	}
}

func (t *TextArea) applyScroll(action Action) bool {
	t.ensureLayout()
	rows := action.Bounds.H
	if rows <= 0 {
		rows = 1
	}
	return t.Scroll.Apply(action, t.layout.totalRows, rows)
}

func (t *TextArea) applyHistoryKey(key event.KeyEvent) (handled, changed bool) {
	if key.Code != event.KeyRune || key.Mod&event.ModCtrl == 0 || key.Mod&(event.ModAlt|event.ModSuper) != 0 {
		return false, false
	}
	r := unicode.ToLower(key.Rune)
	undo := r == 'z' && key.Mod&event.ModShift == 0
	redo := r == 'y' && key.Mod&event.ModShift == 0 || r == 'z' && key.Mod&event.ModShift != 0
	if !undo && !redo {
		return false, false
	}
	count := min(max(int(key.Repeat), 1), len(t.undo)+len(t.redo)+1)
	for range count {
		if undo {
			changed = t.Undo() || changed
		} else {
			changed = t.Redo() || changed
		}
	}
	return true, changed
}

func (t *TextArea) applyKey(key event.KeyEvent, bounds render.Rect) {
	shift := key.Mod&event.ModShift != 0
	ctrl := key.Mod&event.ModCtrl != 0
	if key.Mod&(event.ModAlt|event.ModSuper) != 0 {
		return
	}
	count := min(max(int(key.Repeat), 1), 4096)

	if key.Code == event.KeyRune && ctrl && unicode.ToLower(key.Rune) == 'a' {
		t.Line, t.Offset = len(t.Lines)-1, len(t.Lines[len(t.Lines)-1])
		t.AnchorLine, t.AnchorOffset = 0, 0
		t.goalCol = -1
		return
	}
	switch key.Code {
	case event.KeyRune:
		if !ctrl && !unicode.IsControl(key.Rune) {
			t.insertLines(strings.Repeat(string(key.Rune), count))
		}
	case event.KeyEnter:
		for range count {
			t.insertLines("\n")
		}
	case event.KeyBackspace:
		for range t.moveBound(count) {
			sL, sO, eL, eO := t.Selection()
			if sL == eL && sO == eO {
				if t.Offset > 0 {
					start := previousBoundary(t.Lines[t.Line], t.Offset)
					if ctrl {
						start = previousWord(t.Lines[t.Line], t.Offset)
					}
					sL, sO, eL, eO = t.Line, start, t.Line, t.Offset
				} else if t.Line > 0 {
					sL, sO, eL, eO = t.Line-1, len(t.Lines[t.Line-1]), t.Line, 0
				}
			}
			if sL == eL && sO == eO {
				break
			}
			t.deleteRange(sL, sO, eL, eO)
		}
	case event.KeyDelete:
		for range t.moveBound(count) {
			sL, sO, eL, eO := t.Selection()
			if sL == eL && sO == eO {
				if t.Offset < len(t.Lines[t.Line]) {
					end := nextBoundary(t.Lines[t.Line], t.Offset)
					if ctrl {
						end = nextWord(t.Lines[t.Line], t.Offset)
					}
					sL, sO, eL, eO = t.Line, t.Offset, t.Line, end
				} else if t.Line < len(t.Lines)-1 {
					sL, sO, eL, eO = t.Line, t.Offset, t.Line+1, 0
				}
			}
			if sL == eL && sO == eO {
				break
			}
			t.deleteRange(sL, sO, eL, eO)
		}
	case event.KeyLeft:
		for range t.moveBound(count) {
			sL, sO, _, _ := t.Selection()
			line, offset := t.Line, t.Offset
			if !shift && (sL != t.Line || sO != t.Offset) {
				line, offset = sL, sO
			} else if t.Offset > 0 {
				if ctrl {
					offset = previousWord(t.Lines[t.Line], t.Offset)
				} else {
					offset = previousBoundary(t.Lines[t.Line], t.Offset)
				}
			} else if t.Line > 0 {
				line = t.Line - 1
				offset = len(t.Lines[line])
			}
			t.moveTo(line, offset, shift)
		}
	case event.KeyRight:
		for range t.moveBound(count) {
			_, _, eL, eO := t.Selection()
			line, offset := t.Line, t.Offset
			if !shift && (eL != t.Line || eO != t.Offset) {
				line, offset = eL, eO
			} else if t.Offset < len(t.Lines[t.Line]) {
				if ctrl {
					offset = nextWord(t.Lines[t.Line], t.Offset)
				} else {
					offset = nextBoundary(t.Lines[t.Line], t.Offset)
				}
			} else if t.Line < len(t.Lines)-1 {
				line = t.Line + 1
				offset = 0
			}
			t.moveTo(line, offset, shift)
		}
	case event.KeyUp:
		t.moveVertical(-min(count, max(t.layout.totalRows, 1)), shift)
	case event.KeyDown:
		t.moveVertical(min(count, max(t.layout.totalRows, 1)), shift)
	case event.KeyHome:
		if ctrl {
			t.moveTo(0, 0, shift)
			return
		}
		t.ensureLayout()
		row, _ := t.layout.caretPosition(t.Line, t.Offset)
		line, offset := t.layout.positionAt(row, 0)
		t.moveTo(line, offset, shift)
	case event.KeyEnd:
		if ctrl {
			t.moveTo(len(t.Lines)-1, len(t.Lines[len(t.Lines)-1]), shift)
			return
		}
		t.ensureLayout()
		row, _ := t.layout.caretPosition(t.Line, t.Offset)
		l, r := t.layout.lineRowOf(row)
		if l < 0 {
			return
		}
		rowText := t.layout.rows[l][r]
		width := rowCellWidth(rowText, t.layout.policy)
		line, offset := t.layout.positionAt(row, width)
		t.moveTo(line, offset, shift)
	case event.KeyPageUp:
		t.moveVertical(-max(bounds.H-1, 1), shift)
	case event.KeyPageDown:
		t.moveVertical(max(bounds.H-1, 1), shift)
	}
}

func (t *TextArea) applyMouse(action Action) {
	mouse := action.Event.Mouse
	if mouse.Button != event.MouseLeft {
		return
	}
	switch mouse.Action {
	case event.MousePress:
		line, offset := t.positionAtPoint(action.X, action.Y, action.Bounds)
		if mouse.Mod&event.ModShift != 0 {
			t.Line, t.Offset = line, offset
		} else {
			t.moveTo(line, offset, false)
		}
	case event.MouseMove:
		// A captured drag extends the selection and keeps the anchor.
		line, offset := t.positionAtPoint(action.X, action.Y, action.Bounds)
		t.Line, t.Offset = line, offset
	}
}

func (t *TextArea) positionAtPoint(x, y int, bounds render.Rect) (int, int) {
	t.ensureLayout()
	l := &t.layout
	rows := max(bounds.H, 1)
	row := t.Scroll.Offset + clamp(y, 0, rows-1)
	row = clamp(row, 0, max(l.totalRows-1, 0))
	col := clamp(x, 0, max(bounds.W-1, 0))
	return l.positionAt(row, col)
}

func (t *TextArea) moveVertical(delta int, extend bool) {
	t.ensureLayout()
	l := &t.layout
	if l.totalRows <= 1 && len(t.Lines) <= 1 {
		return
	}
	row, col := l.caretPosition(t.Line, t.Offset)
	if t.goalCol < 0 {
		t.goalCol = col
	}
	target := clamp(row+delta, 0, max(l.totalRows-1, 0))
	if target == row {
		return
	}
	line, offset := l.positionAt(target, t.goalCol)
	t.Line, t.Offset = line, offset
	if !extend {
		t.AnchorLine, t.AnchorOffset = line, offset
	}
}

func (t *TextArea) moveTo(line, offset int, extend bool) {
	if !extend {
		t.AnchorLine, t.AnchorOffset = line, offset
	}
	t.Line, t.Offset = line, offset
	t.goalCol = -1
}

func (t *TextArea) graphemeCount() int {
	total := 0
	for _, line := range t.Lines {
		total += uniseg.GraphemeClusterCount(line)
	}
	return total
}

// moveBound caps a repeated key press by the document size. The common
// single-press case skips the full-document grapheme scan entirely: one move
// is always in range because graphemeCount plus the line count is at least 1.
func (t *TextArea) moveBound(count int) int {
	if count <= 1 {
		return 1
	}
	return min(count, t.graphemeCount()+len(t.Lines))
}

func (t *TextArea) insertLines(value string) {
	parts := sanitizeMultiline(value)
	if len(parts) == 1 && parts[0] == "" {
		// Replacing a selection with empty text is still a deletion: an
		// empty paste or a Cut must clear the selected range.
		sL, sO, eL, eO := t.Selection()
		if sL != eL || sO != eO {
			t.deleteRange(sL, sO, eL, eO)
		}
		return
	}
	sL, sO, eL, eO := t.Selection()
	// The dirty range must start at the caret line before any mutation:
	// multi-part inserts advance t.Line past the lines they splice in.
	start := t.Line
	t.deleteRange(sL, sO, eL, eO)

	if t.MaxBytes > 0 {
		budget := t.MaxBytes - t.totalBytes()
		parts = trimPartsToBudget(parts, budget)
		if len(parts) == 0 {
			return
		}
	}

	line := t.Lines[t.Line]
	first := parts[0]
	line = line[:t.Offset] + first + line[t.Offset:]
	newOffset := t.Offset + len(first)
	if len(parts) == 1 {
		t.Lines[t.Line] = line
		t.Offset = newOffset
		t.invalidateRange(t.Line, t.Line+1)
	} else {
		tail := line[newOffset:]
		t.Lines[t.Line] = line[:newOffset]
		rest := parts[1:]
		rest[len(rest)-1] += tail
		merged := make([]string, 0, len(t.Lines)+len(rest))
		merged = append(merged, t.Lines[:t.Line+1]...)
		merged = append(merged, rest...)
		merged = append(merged, t.Lines[t.Line+1:]...)
		t.Lines = merged
		// Shift the cached rows with the spliced lines so the untouched
		// tail keeps its wrap; only the merged and inserted lines rebuild.
		t.spliceRows(t.Line+1, 0, len(rest))
		t.Line = t.Line + len(rest)
		t.Offset = len(t.Lines[t.Line]) - len(tail)
		t.invalidateRange(start, t.Line+1)
	}
	t.AnchorLine, t.AnchorOffset = t.Line, t.Offset
}

func (t *TextArea) deleteRange(startLine, startOffset, endLine, endOffset int) {
	startLine = clamp(startLine, 0, len(t.Lines)-1)
	endLine = clamp(endLine, 0, len(t.Lines)-1)
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	startOffset = clamp(floorBoundary(t.Lines[startLine], startOffset), 0, len(t.Lines[startLine]))
	if startLine == endLine {
		endOffset = clamp(floorBoundary(t.Lines[endLine], endOffset), startOffset, len(t.Lines[endLine]))
		if startOffset == endOffset {
			t.Line, t.Offset = startLine, startOffset
			t.AnchorLine, t.AnchorOffset = startLine, startOffset
			return
		}
		t.Lines[startLine] = t.Lines[startLine][:startOffset] + t.Lines[startLine][endOffset:]
		t.invalidateRange(startLine, startLine+1)
	} else {
		endOffset = clamp(floorBoundary(t.Lines[endLine], endOffset), 0, len(t.Lines[endLine]))
		t.Lines[startLine] = t.Lines[startLine][:startOffset] + t.Lines[endLine][endOffset:]
		// Merged lines: shift the cached rows down with the surviving tail
		// instead of re-wrapping it.
		t.spliceRows(startLine+1, endLine-startLine, 0)
		t.Lines = append(t.Lines[:startLine+1], t.Lines[endLine+1:]...)
		t.invalidateRange(startLine, startLine+1)
	}
	t.Line, t.Offset = startLine, startOffset
	t.AnchorLine, t.AnchorOffset = startLine, startOffset
	t.goalCol = -1
}

func (t *TextArea) normalize() {
	if len(t.Lines) == 0 {
		t.Lines = []string{""}
	}
	total := 0
	for i, line := range t.Lines {
		t.Lines[i] = sanitizeLine(line)
		total += len(t.Lines[i])
	}
	if t.MaxBytes > 0 && total > t.MaxBytes {
		t.Lines = limitLines(t.Lines, t.MaxBytes)
	}
	t.Line = clamp(t.Line, 0, len(t.Lines)-1)
	t.Offset = clamp(floorBoundary(t.Lines[t.Line], t.Offset), 0, len(t.Lines[t.Line]))
	t.AnchorLine = clamp(t.AnchorLine, 0, len(t.Lines)-1)
	t.AnchorOffset = clamp(floorBoundary(t.Lines[t.AnchorLine], t.AnchorOffset), 0, len(t.Lines[t.AnchorLine]))
}

func (t *TextArea) totalBytes() int {
	total := 0
	for _, line := range t.Lines {
		total += len(line)
	}
	return total
}

// invalidate drops the whole cached layout; undo, restore, and SetValue use
// it because they rewrite the document in one step.
func (t *TextArea) invalidate() {
	t.layout.valid = false
	t.goalCol = -1
}

// invalidateRange marks a contiguous logical-line range for lazy rebuilding.
// Lines outside it keep their cached wrap; a keystroke only re-wraps the
// lines it actually touched.
func (t *TextArea) invalidateRange(from, to int) {
	t.goalCol = -1
	l := &t.layout
	if !l.valid {
		return
	}
	from = clamp(from, 0, len(t.Lines))
	to = clamp(to, from, len(t.Lines))
	if l.dirtyTo > l.dirtyFrom {
		l.dirtyFrom = min(l.dirtyFrom, from)
		l.dirtyTo = max(l.dirtyTo, to)
	} else {
		l.dirtyFrom, l.dirtyTo = from, to
	}
}

// spliceRows keeps the cached rows parallel to Lines when line count changes:
// insert n slots at index from, or drop count slots at from when n is 0 and
// count > 0. Untouched tail lines keep their wrap at the shifted index.
func (t *TextArea) spliceRows(from, count, n int) {
	l := &t.layout
	if !l.valid {
		return
	}
	if count > 0 && n == 0 {
		l.rows = slices.Delete(l.rows, from, from+count)
		l.starts = slices.Delete(l.starts, from, from+count)
		return
	}
	if n > 0 {
		empty := make([][]string, n)
		for i := range empty {
			empty[i] = []string{""}
		}
		l.rows = slices.Insert(l.rows, from, empty...)
		emptyStarts := make([][]int, n)
		for i := range emptyStarts {
			emptyStarts[i] = []int{0}
		}
		l.starts = slices.Insert(l.starts, from, emptyStarts...)
	}
}

func (t *TextArea) buildLayout(width int, policy render.WidthPolicy) {
	l := &t.layout
	if l.width != width {
		l.width = width
		l.valid = false
	}
	if l.policy != policy {
		l.policy = policy
		l.valid = false
	}
	if l.mode != t.Wrap {
		l.mode = t.Wrap
		l.valid = false
	}
	t.ensureLayout()
}

// ensureLayout rebuilds the cached wrap on demand, at draw time and again at
// event time, using the last drawn width and policy.
func (t *TextArea) ensureLayout() {
	l := &t.layout
	if l.mode != t.Wrap {
		l.mode = t.Wrap
		l.valid = false
	}
	// Any path that rewrote Lines without splicing rows leaves the cache
	// out of step; fall back to a full rebuild instead of trusting it.
	if l.valid && (len(l.rows) != len(t.Lines) || len(l.starts) != len(t.Lines)) {
		l.valid = false
	}
	if !l.valid {
		l.dirtyFrom, l.dirtyTo = 0, len(t.Lines)
		l.valid = true
	}
	if l.dirtyTo <= l.dirtyFrom {
		return
	}
	from := clamp(l.dirtyFrom, 0, max(len(t.Lines)-1, 0))
	to := clamp(l.dirtyTo, from, len(t.Lines))
	if cap(l.rows) < len(t.Lines) || cap(l.starts) < len(t.Lines) || cap(l.rowOfLine) < len(t.Lines) {
		grown := make([][]string, len(t.Lines))
		copy(grown, l.rows)
		l.rows = grown
		grownStarts := make([][]int, len(t.Lines))
		copy(grownStarts, l.starts)
		l.starts = grownStarts
		grownOf := make([]int, len(t.Lines))
		copy(grownOf, l.rowOfLine)
		l.rowOfLine = grownOf
	}
	l.rows = l.rows[:len(t.Lines)]
	l.starts = l.starts[:len(t.Lines)]
	l.rowOfLine = l.rowOfLine[:len(t.Lines)]
	opts := render.WrapOptions{Mode: l.mode}
	for i := from; i < to; i++ {
		var rows []string
		if l.width > 0 {
			rows = render.WrapLinesWith(t.Lines[i], l.width, opts, l.policy)
		} else {
			rows = []string{t.Lines[i]} // unmeasured: hard lines only
		}
		if len(rows) == 0 {
			rows = []string{""}
		}
		l.rows[i] = rows
		l.starts[i] = rowStarts(t.Lines[i], rows, l.mode)
	}
	l.dirtyFrom, l.dirtyTo = 0, 0
	t.recountRows()
}

func (t *TextArea) recountRows() {
	l := &t.layout
	total := 0
	for i := range t.Lines {
		l.rowOfLine[i] = total
		total += len(l.rows[i])
	}
	l.totalRows = total
}

// caretPosition maps a caret on a logical line to its wrapped row and cell
// column. A caret standing inside whitespace that a word-mode soft break
// consumed, or just before it, lands at the end of the preceding row.
func (l *textAreaLayout) caretPosition(line, offset int) (row, col int) {
	if line < 0 || line >= len(l.rows) {
		return 0, 0
	}
	rows, starts := l.rows[line], l.starts[line]
	r := 0
	for r < len(rows)-1 {
		if offset < starts[r]+len(rows[r]) {
			break
		}
		if offset < starts[r+1] {
			return l.rowOfLine[line] + r, rowCellWidth(rows[r], l.policy)
		}
		r++
	}
	local := clamp(offset-starts[r], 0, len(rows[r]))
	return l.rowOfLine[line] + r, rowCellWidth(rows[r][:local], l.policy)
}

// positionAt maps a wrapped row and cell column back to a logical caret.
// The column is rounded to the nearest grapheme, wide graphemes included.
func (l *textAreaLayout) positionAt(row, col int) (line, offset int) {
	if row < 0 || row >= l.totalRows || len(l.rows) == 0 {
		return 0, 0
	}
	l2, r := l.lineRowOf(row)
	if l2 < 0 {
		return 0, 0
	}
	return l2, l.starts[l2][r] + byteAtRowCell(l.rows[l2][r], col, l.policy)
}

// lineRowOf resolves a global wrapped row into its logical line and the
// row's index within that line.
func (l *textAreaLayout) lineRowOf(row int) (line, index int) {
	if row < 0 || row >= l.totalRows {
		return -1, 0
	}
	line = 0
	for i := 1; i < len(l.rowOfLine); i++ {
		if l.rowOfLine[i] > row {
			break
		}
		line = i
	}
	return line, row - l.rowOfLine[line]
}

// rowStarts recovers each wrapped row's start offset in its logical line.
// Rows are contiguous input slices; the only gap is whitespace a word-mode
// soft break consumed, which is skipped between rows. A row that itself
// starts with whitespace (kept indentation) stops the skip immediately.
func rowStarts(line string, rows []string, mode render.WrapMode) []int {
	starts := make([]int, len(rows))
	if len(rows) == 0 {
		return starts
	}
	starts[0] = 0
	if mode == render.WrapChar {
		for r := 1; r < len(rows); r++ {
			starts[r] = starts[r-1] + len(rows[r-1])
		}
		return starts
	}
	pos := 0
	for r := 1; r < len(rows); r++ {
		pos += len(rows[r-1])
		next := firstGraphemeOf(rows[r])
		for pos < len(line) {
			grapheme := graphemeAt(line, pos)
			if grapheme == next || !isWordWrapSpace(grapheme) {
				break
			}
			pos += len(grapheme)
		}
		starts[r] = pos
	}
	return starts
}

func firstGraphemeOf(text string) string {
	graphemes := uniseg.NewGraphemes(text)
	if graphemes.Next() {
		return graphemes.Str()
	}
	return ""
}

func graphemeAt(text string, offset int) string {
	graphemes := uniseg.NewGraphemes(text[offset:])
	if graphemes.Next() {
		return graphemes.Str()
	}
	return ""
}

func isWordWrapSpace(grapheme string) bool {
	return grapheme == " " || grapheme == "\t"
}

// rowCellWidth measures a wrapped row exactly as it is drawn: tabs advance
// to four-column stops relative to the row start, every other grapheme pays
// its policy width, unprintables pay nothing.
func rowCellWidth(row string, policy render.WidthPolicy) int {
	col := 0
	graphemes := uniseg.NewGraphemes(row)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		if grapheme == "\t" {
			col += 4 - col%4
			continue
		}
		col += render.GraphemeWidthWith(grapheme, policy)
	}
	return col
}

// byteAtRowCell maps a cell column to a byte offset inside one wrapped row,
// rounding inside wide graphemes and tabs the way the one-line field does.
func byteAtRowCell(row string, cell int, policy render.WidthPolicy) int {
	if cell <= 0 {
		return 0
	}
	x := 0
	graphemes := uniseg.NewGraphemes(row)
	for graphemes.Next() {
		from, to := graphemes.Positions()
		grapheme := graphemes.Str()
		width := render.GraphemeWidthWith(grapheme, policy)
		if grapheme == "\t" {
			width = 4 - x%4
		}
		if width == 0 {
			continue
		}
		if x+width > cell {
			if cell-x >= (width+1)/2 {
				return to
			}
			return from
		}
		x += width
		if x >= cell {
			return to
		}
	}
	return len(row)
}

// drawAreaRow paints one wrapped row. The caret is given as a visual cell
// column (-1 when this row has no caret): caretPosition already mapped
// consumed-whitespace gaps to the preceding row's end, so the cell math here
// stays free of soft-break bookkeeping. Selection bounds stay logical, in
// line-relative byte offsets, because a selection can span wrapped rows.
func drawAreaRow(canvas *render.Canvas, view render.Rect, y int, row string, start int,
	line, selLine, selStart, selEndLine, selEnd, caretCol int,
	normal, selection, caretStyle render.Style) {
	x := 0
	graphemes := uniseg.NewGraphemes(row)
	for graphemes.Next() {
		from, to := graphemes.Positions()
		absFrom, absTo := start+from, start+to
		grapheme := graphemes.Str()
		selected := selLine != selEndLine || selStart != selEnd
		if selected {
			selected = (line > selLine || line == selLine && absFrom >= selStart) &&
				(line < selEndLine || line == selEndLine && absTo <= selEnd)
		}
		paint := normal
		if selected {
			paint = selection
		}
		if grapheme == "\t" {
			stops := 4 - x%4
			for range stops {
				if x >= view.W {
					return
				}
				canvas.Put(view.X+x, view.Y+y, " ", paint)
				x++
			}
			continue
		}
		width := canvas.GraphemeWidth(grapheme)
		if width == 0 {
			canvas.Put(view.X+x, view.Y+y, grapheme, paint)
			continue
		}
		if caretCol >= 0 && x <= caretCol && caretCol < x+width {
			paint = caretStyle
		}
		if x+width > view.W {
			return
		}
		canvas.Put(view.X+x, view.Y+y, grapheme, paint)
		x += width
	}
	if caretCol >= 0 && caretCol == rowCellWidth(row, canvas.WidthPolicy()) && x < view.W {
		canvas.Put(view.X+x, view.Y+y, " ", caretStyle)
	}
}

type textAreaState struct {
	lines                                                []string
	line, offset, anchorLine, anchorOffset, scrollOffset int
}

func (t *TextArea) state() textAreaState {
	return textAreaState{
		lines: t.Lines, line: t.Line, offset: t.Offset,
		anchorLine: t.AnchorLine, anchorOffset: t.AnchorOffset, scrollOffset: t.Scroll.Offset,
	}
}

func (s textAreaState) changed(t *TextArea) bool {
	return !linesEqual(s.lines, t.Lines) || s.line != t.Line || s.offset != t.Offset ||
		s.anchorLine != t.AnchorLine || s.anchorOffset != t.AnchorOffset ||
		s.scrollOffset != t.Scroll.Offset
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (t *TextArea) snapshot() textAreaSnapshot {
	return textAreaSnapshot{
		lines: append([]string(nil), t.Lines...), line: t.Line, offset: t.Offset,
		anchorLine: t.AnchorLine, anchorOffset: t.AnchorOffset,
	}
}

func (t *TextArea) restore(snapshot textAreaSnapshot) {
	t.Lines = append([]string(nil), snapshot.lines...)
	for i, line := range t.Lines {
		t.Lines[i] = sanitizeLine(line)
	}
	t.Line = clamp(snapshot.line, 0, len(t.Lines)-1)
	t.Offset = clamp(floorBoundary(t.Lines[t.Line], snapshot.offset), 0, len(t.Lines[t.Line]))
	t.AnchorLine = clamp(snapshot.anchorLine, 0, len(t.Lines)-1)
	t.AnchorOffset = clamp(floorBoundary(t.Lines[t.AnchorLine], snapshot.anchorOffset), 0, len(t.Lines[t.AnchorLine]))
	t.invalidate()
}

func (t *TextArea) historyLimit() int {
	if t.UndoBytes == 0 {
		return defaultUndoBytes
	}
	return t.UndoBytes
}

func (t *TextArea) prepareHistory() {
	limit := t.historyLimit()
	if limit < 0 {
		t.clearHistory()
		return
	}
	t.trimHistory(&t.undo, &t.undoBytes, limit)
	t.trimHistory(&t.redo, &t.redoBytes, limit)
}

func (t *TextArea) clearHistory() {
	clear(t.undo)
	clear(t.redo)
	t.undo, t.redo = nil, nil
	t.undoBytes, t.redoBytes = 0, 0
}

func (t *TextArea) recordEdit(before textAreaSnapshot) {
	if linesEqual(before.lines, t.Lines) {
		return
	}
	clear(t.redo)
	t.redo, t.redoBytes = nil, 0
	t.pushHistory(&t.undo, &t.undoBytes, before)
}

func (t *TextArea) pushHistory(stack *[]textAreaSnapshot, bytes *int, snapshot textAreaSnapshot) {
	limit := t.historyLimit()
	if limit < 0 {
		clear(*stack)
		*stack, *bytes = nil, 0
		return
	}
	snapshot.lines = append([]string(nil), snapshot.lines...)
	size := textAreaSnapshotSize(snapshot)
	if size > limit {
		clear(*stack)
		*stack, *bytes = nil, 0
		return
	}
	*stack = append(*stack, snapshot)
	*bytes += size
	t.trimHistory(stack, bytes, limit)
}

func (t *TextArea) trimHistory(stack *[]textAreaSnapshot, bytes *int, limit int) {
	remove := 0
	for *bytes > limit && remove < len(*stack) {
		*bytes -= textAreaSnapshotSize((*stack)[remove])
		remove++
	}
	if remove == 0 {
		return
	}
	copy(*stack, (*stack)[remove:])
	clear((*stack)[len(*stack)-remove:])
	*stack = (*stack)[:len(*stack)-remove]
}

func (t *TextArea) popHistory(stack *[]textAreaSnapshot, bytes *int) (textAreaSnapshot, bool) {
	if len(*stack) == 0 {
		return textAreaSnapshot{}, false
	}
	last := len(*stack) - 1
	snapshot := (*stack)[last]
	(*stack)[last] = textAreaSnapshot{}
	*stack = (*stack)[:last]
	*bytes -= textAreaSnapshotSize(snapshot)
	return snapshot, true
}

func textAreaSnapshotSize(snapshot textAreaSnapshot) int {
	total := len(snapshot.lines)
	for _, line := range snapshot.lines {
		total += len(line)
	}
	return max(total, 1)
}

func caretBefore(line, offset, anchorLine, anchorOffset int) bool {
	if line != anchorLine {
		return line < anchorLine
	}
	return offset < anchorOffset
}

func clamp(v, low, high int) int {
	if high < low {
		return low
	}
	return min(max(v, low), high)
}

// sanitizeMultiline cleans pasted or committed text and splits it into
// logical lines. Carriage returns normalize to newlines, tabs survive as
// content, every other control is dropped, invalid bytes become U+FFFD.
func sanitizeMultiline(value string) []string {
	if utf8.ValidString(value) {
		clean := true
		for _, r := range value {
			if r != '\t' && r != '\n' && r != '\r' && unicode.IsControl(r) {
				clean = false
				break
			}
		}
		if clean {
			return strings.Split(normalizeBreaks(value), "\n")
		}
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	return strings.Split(normalizeBreaks(value), "\n")
}

func normalizeBreaks(value string) string {
	if !strings.ContainsRune(value, '\r') {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	previousCR := false
	for _, r := range value {
		switch r {
		case '\r':
			out.WriteByte('\n')
			previousCR = true
		case '\n':
			if !previousCR {
				out.WriteByte('\n')
			}
			previousCR = false
		case '\t':
			out.WriteByte('\t')
			previousCR = false
		default:
			previousCR = false
			if !unicode.IsControl(r) {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// sanitizeLine is the defensive per-line pass for state that did not come
// through insert or SetValue. Line breaks cannot exist here, so a stray one
// becomes a space; tabs survive; other controls drop.
func sanitizeLine(line string) string {
	if utf8.ValidString(line) {
		clean := true
		for _, r := range line {
			if r != '\t' && unicode.IsControl(r) {
				clean = false
				break
			}
		}
		if clean {
			return line
		}
	}
	line = strings.ToValidUTF8(line, "\uFFFD")
	var out strings.Builder
	out.Grow(len(line))
	previousCR := false
	for _, r := range line {
		switch r {
		case '\r':
			out.WriteByte(' ')
			previousCR = true
		case '\n':
			if !previousCR {
				out.WriteByte(' ')
			}
			previousCR = false
		case '\t':
			out.WriteByte('\t')
			previousCR = false
		default:
			previousCR = false
			if !unicode.IsControl(r) {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// limitLines keeps the prefix of a document whose total line bytes exceed
// maxBytes, cutting only at extended-grapheme boundaries.
func limitLines(lines []string, maxBytes int) []string {
	if maxBytes <= 0 {
		return lines
	}
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	if total <= maxBytes {
		return lines
	}
	kept := make([]string, 0, len(lines))
	total = 0
	for _, line := range lines {
		budget := maxBytes - total
		if budget <= 0 {
			break
		}
		line = graphemePrefix(line, budget)
		if line == "" {
			break
		}
		kept = append(kept, line)
		total += len(line)
	}
	if len(kept) == 0 {
		kept = []string{""}
	}
	return kept
}

func trimPartsToBudget(parts []string, budget int) []string {
	if budget < 0 {
		return parts
	}
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	if total <= budget {
		return parts
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if budget <= 0 {
			break
		}
		part = graphemePrefix(part, budget)
		if part == "" {
			break
		}
		kept = append(kept, part)
		budget -= len(part)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
