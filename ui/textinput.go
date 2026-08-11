package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/rivo/uniseg"
)

// TextInput is the editable state of a one-line text field. Cursor, Anchor,
// and Offset are UTF-8 byte offsets at extended-grapheme boundaries. Equal
// Cursor and Anchor means there is no selection; the zero value is ready.
type TextInput struct {
	Value       string
	Cursor      int
	Anchor      int
	MaxBytes    int
	UndoBytes   int
	Offset      int
	Placeholder string

	undo      []textInputSnapshot
	redo      []textInputSnapshot
	undoBytes int
	redoBytes int
}

const defaultUndoBytes = 64 << 10

type textInputSnapshot struct {
	value          string
	cursor, anchor int
}

// InputStyle controls painting and whether a field accepts interaction. Zero
// Selection and Cursor styles inherit Normal with reverse video; a zero
// Placeholder style inherits Normal with dim text.
type InputStyle struct {
	Normal      render.Style
	Placeholder render.Style
	Selection   render.Style
	Cursor      render.Style
	ReadOnly    bool
	Disabled    bool
}

// SetValue replaces the value, applies the one-line input policy, moves the
// caret to the end, and clears selection.
func (t *TextInput) SetValue(value string) {
	if t == nil {
		return
	}
	t.Value = limitText(sanitizeInput(value), t.MaxBytes)
	t.Cursor, t.Anchor, t.Offset = len(t.Value), len(t.Value), 0
	t.clearHistory()
}

// Selection returns the ordered half-open selected byte range. Equal values
// mean no selection.
func (t *TextInput) Selection() (start, end int) {
	if t == nil {
		return 0, 0
	}
	t.normalize()
	if t.Cursor < t.Anchor {
		return t.Cursor, t.Anchor
	}
	return t.Anchor, t.Cursor
}

// SelectedText returns a detached copy of the selected text.
func (t *TextInput) SelectedText() string {
	if t == nil {
		return ""
	}
	start, end := t.Selection()
	return strings.Clone(t.Value[start:end])
}

// ReplaceSelection replaces the selection as one undoable edit. An empty
// value deletes the selection.
func (t *TextInput) ReplaceSelection(value string) bool {
	if t == nil {
		return false
	}
	t.normalize()
	t.prepareHistory()
	before := t.snapshot()
	state := t.state()
	deleteSelection := value == ""
	value = sanitizeInput(value)
	if value == "" && !deleteSelection {
		return state.changed(t)
	}
	if deleteSelection {
		start, end := t.selection()
		t.remove(start, end)
	} else {
		t.insert(value)
	}
	t.recordEdit(before)
	return state.changed(t)
}

// Undo restores the last value-changing edit.
func (t *TextInput) Undo() bool {
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
func (t *TextInput) Redo() bool {
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

// Apply applies one routed Input action and reports whether editable state
// changed. It accepts committed text, bounded paste, navigation, deletion,
// Ctrl+A, Ctrl+Left/Right UAX #29 word movement, and local mouse placement.
func (t *TextInput) Apply(action Action) bool {
	if t == nil || action.Kind != Input {
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
		t.insert(action.Event.Text)
	case event.Key:
		t.applyKey(action.Event.Key)
	case event.Mouse:
		t.applyMouse(action)
	}
	t.recordEdit(before)
	return original.changed(t)
}

// Input registers and paints a one-line text input. Read-only fields remain
// focusable but do not receive Input actions; disabled fields are inert.
func (c *Context) Input(id ID, canvas *render.Canvas, rect render.Rect, input *TextInput, style InputStyle) {
	if c == nil || canvas == nil || input == nil || id == "" {
		return
	}
	input.normalize()
	rect.H = min(max(rect.H, 0), 1)
	line := rect.Intersect(canvas.Bounds())
	flags := Focusable | Editable
	if style.ReadOnly {
		flags = Focusable
	}
	if style.Disabled {
		flags |= Disabled
	}
	// The field's own rectangle is registered, not the visible remainder, so a
	// container that scrolled it out of view can still reach and reveal it.
	c.Region(id, canvas, rect, flags)
	if line.Empty() {
		input.Offset = input.Cursor
		return
	}

	base := style.Normal
	if style.Disabled {
		base.Attrs |= render.AttrDim
	}
	canvas.Fill(line, " ", base)
	focused := c.Focused(id) && !style.ReadOnly && !style.Disabled
	input.ensureOffset(line.W, focused)
	if focused {
		// The real terminal cursor follows the caret so IME and screen readers
		// track it; a clipped or scrolled-out caret stays hidden.
		if column := textCells(input.Value[input.Offset:input.Cursor]); column < line.W {
			canvas.SetCaret(line.X+column, line.Y)
		}
	}

	placeholder := style.Placeholder
	if placeholder == (render.Style{}) {
		placeholder = base
		placeholder.Attrs |= render.AttrDim
	}
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

	if input.Value == "" {
		drawPlaceholder(canvas, line, input.Placeholder, placeholder, caret, focused)
		return
	}
	drawInput(canvas, line, input, base, selection, caret, focused)
}

func (t *TextInput) normalize() {
	t.Value = limitText(sanitizeInput(t.Value), t.MaxBytes)
	t.Cursor, t.Anchor, t.Offset = floorBoundaries(t.Value, t.Cursor, t.Anchor, t.Offset)
}

type textInputState struct {
	value                  string
	cursor, anchor, offset int
}

func (t *TextInput) state() textInputState {
	return textInputState{value: t.Value, cursor: t.Cursor, anchor: t.Anchor, offset: t.Offset}
}

func (s textInputState) changed(t *TextInput) bool {
	return s.value != t.Value || s.cursor != t.Cursor || s.anchor != t.Anchor || s.offset != t.Offset
}

func (t *TextInput) snapshot() textInputSnapshot {
	return textInputSnapshot{value: t.Value, cursor: t.Cursor, anchor: t.Anchor}
}

func (t *TextInput) restore(snapshot textInputSnapshot) {
	t.Value = limitText(sanitizeInput(snapshot.value), t.MaxBytes)
	t.Cursor, t.Anchor, _ = floorBoundaries(t.Value, snapshot.cursor, snapshot.anchor, 0)
	t.Offset = floorBoundary(t.Value, min(t.Offset, t.Cursor))
}

func (t *TextInput) historyLimit() int {
	if t.UndoBytes == 0 {
		return defaultUndoBytes
	}
	return t.UndoBytes
}

func (t *TextInput) prepareHistory() {
	limit := t.historyLimit()
	if limit < 0 {
		t.clearHistory()
		return
	}
	t.trimHistory(&t.undo, &t.undoBytes, limit)
	t.trimHistory(&t.redo, &t.redoBytes, limit)
}

func (t *TextInput) clearHistory() {
	clear(t.undo)
	clear(t.redo)
	t.undo, t.redo = nil, nil
	t.undoBytes, t.redoBytes = 0, 0
}

func (t *TextInput) recordEdit(before textInputSnapshot) {
	if before.value == t.Value {
		return
	}
	clear(t.redo)
	t.redo, t.redoBytes = nil, 0
	t.pushHistory(&t.undo, &t.undoBytes, before)
}

func (t *TextInput) pushHistory(stack *[]textInputSnapshot, bytes *int, snapshot textInputSnapshot) {
	limit := t.historyLimit()
	if limit < 0 {
		clear(*stack)
		*stack, *bytes = nil, 0
		return
	}
	snapshot.value = strings.Clone(snapshot.value)
	size := snapshotSize(snapshot)
	if size > limit {
		clear(*stack)
		*stack, *bytes = nil, 0
		return
	}
	*stack = append(*stack, snapshot)
	*bytes += size
	t.trimHistory(stack, bytes, limit)
}

func (t *TextInput) trimHistory(stack *[]textInputSnapshot, bytes *int, limit int) {
	remove := 0
	for *bytes > limit && remove < len(*stack) {
		*bytes -= snapshotSize((*stack)[remove])
		remove++
	}
	if remove == 0 {
		return
	}
	copy(*stack, (*stack)[remove:])
	clear((*stack)[len(*stack)-remove:])
	*stack = (*stack)[:len(*stack)-remove]
}

func (t *TextInput) popHistory(stack *[]textInputSnapshot, bytes *int) (textInputSnapshot, bool) {
	if len(*stack) == 0 {
		return textInputSnapshot{}, false
	}
	last := len(*stack) - 1
	snapshot := (*stack)[last]
	(*stack)[last] = textInputSnapshot{}
	*stack = (*stack)[:last]
	*bytes -= snapshotSize(snapshot)
	return snapshot, true
}

func snapshotSize(snapshot textInputSnapshot) int {
	return max(len(snapshot.value), 1)
}

func (t *TextInput) applyHistoryKey(key event.KeyEvent) (handled, changed bool) {
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

func (t *TextInput) applyKey(key event.KeyEvent) {
	shift := key.Mod&event.ModShift != 0
	ctrl := key.Mod&event.ModCtrl != 0
	if key.Mod&(event.ModAlt|event.ModSuper) != 0 {
		return
	}
	count := max(int(key.Repeat), 1)
	count = min(count, uniseg.GraphemeClusterCount(t.Value)+1)

	if key.Code == event.KeyRune && ctrl && unicode.ToLower(key.Rune) == 'a' {
		t.Anchor, t.Cursor = 0, len(t.Value)
		return
	}
	switch key.Code {
	case event.KeyLeft:
		for range count {
			start, end := t.selection()
			target := previousBoundary(t.Value, t.Cursor)
			if ctrl {
				target = previousWord(t.Value, t.Cursor)
			} else if !shift && start != end {
				target = start
			}
			t.move(target, shift)
		}
	case event.KeyRight:
		for range count {
			start, end := t.selection()
			target := nextBoundary(t.Value, t.Cursor)
			if ctrl {
				target = nextWord(t.Value, t.Cursor)
			} else if !shift && start != end {
				target = end
			}
			t.move(target, shift)
		}
	case event.KeyHome:
		t.move(0, shift)
	case event.KeyEnd:
		t.move(len(t.Value), shift)
	case event.KeyBackspace:
		for range count {
			start, end := t.selection()
			if start == end {
				if ctrl {
					start = previousWord(t.Value, t.Cursor)
				} else {
					start = previousBoundary(t.Value, t.Cursor)
				}
			}
			if start == end {
				break
			}
			t.remove(start, end)
		}
	case event.KeyDelete:
		for range count {
			start, end := t.selection()
			if start == end {
				if ctrl {
					end = nextWord(t.Value, t.Cursor)
				} else {
					end = nextBoundary(t.Value, t.Cursor)
				}
			}
			if start == end {
				break
			}
			t.remove(start, end)
		}
	case event.KeyRune:
		if !ctrl && !unicode.IsControl(key.Rune) {
			t.insert(strings.Repeat(string(key.Rune), count))
		}
	}
}

func (t *TextInput) applyMouse(action Action) {
	mouse := action.Event.Mouse
	if mouse.Button != event.MouseLeft {
		return
	}
	switch mouse.Action {
	case event.MousePress:
		target := byteAtCell(t.Value, t.Offset, action.X, action.Bounds.W)
		if mouse.Mod&event.ModShift != 0 {
			t.Cursor = target
		} else {
			t.Cursor, t.Anchor = target, target
		}
	case event.MouseMove:
		// A captured drag extends the selection and keeps the anchor; dragging
		// past an edge walks one grapheme per event so a narrow field scrolls.
		t.Cursor = t.dragTarget(action)
	}
}

func (t *TextInput) dragTarget(action Action) int {
	width := action.Bounds.W
	switch {
	case action.X < 0:
		return previousBoundary(t.Value, min(t.Cursor, t.Offset))
	case width > 0 && action.X >= width:
		if inside := byteAtCell(t.Value, t.Offset, width-1, width); inside > t.Cursor {
			return inside
		}
		return nextBoundary(t.Value, t.Cursor)
	default:
		return byteAtCell(t.Value, t.Offset, action.X, width)
	}
}

func (t *TextInput) move(target int, extend bool) {
	target = floorBoundary(t.Value, target)
	if extend {
		t.Cursor = target
		return
	}
	t.Cursor, t.Anchor = target, target
}

func (t *TextInput) insert(value string) {
	value = sanitizeInput(value)
	if value == "" {
		return
	}
	start, end := t.selection()
	limit := -1
	if t.MaxBytes > 0 {
		limit = t.MaxBytes - (len(t.Value) - (end - start))
	}
	value = graphemePrefix(value, limit)
	if value == "" {
		return
	}
	t.Value = t.Value[:start] + value + t.Value[end:]
	t.Cursor = ceilBoundary(t.Value, start+len(value))
	t.Anchor = t.Cursor
	t.Offset = floorBoundary(t.Value, min(t.Offset, t.Cursor))
}

func (t *TextInput) remove(start, end int) {
	if start < 0 || start >= end || end > len(t.Value) {
		return
	}
	t.Value = t.Value[:start] + t.Value[end:]
	t.Cursor = floorBoundary(t.Value, start)
	t.Anchor = t.Cursor
	t.Offset = floorBoundary(t.Value, min(t.Offset, t.Cursor))
}

// ensureOffset recomputes the horizontal window so the caret is visible. The
// window is the shortest suffix of the text before the caret that still fits,
// so one grapheme pass is enough; measuring the candidate window inside a loop
// would be quadratic in the value length and stall a frame on a pasted field.
func (t *TextInput) ensureOffset(width int, caret bool) {
	if width <= 0 {
		t.Offset = t.Cursor
		return
	}
	needed := 0
	if t.Cursor < len(t.Value) {
		needed = max(render.GraphemeWidth(t.Value[t.Cursor:]), 1)
	} else if caret {
		needed = 1
	}
	budget := width - needed
	if budget <= 0 {
		t.Offset = t.Cursor
		return
	}
	head := t.Value[:t.Cursor]
	total := textCells(head)
	if total <= budget {
		t.Offset = 0
		return
	}
	used := 0
	graphemes := uniseg.NewGraphemes(head)
	for graphemes.Next() {
		_, to := graphemes.Positions()
		used += render.GraphemeWidth(graphemes.Str())
		if total-used <= budget {
			t.Offset = to
			return
		}
	}
	t.Offset = t.Cursor
}

func drawInput(canvas *render.Canvas, line render.Rect, input *TextInput, normal, selection, caret render.Style, focused bool) {
	selectedStart, selectedEnd := input.selection()
	x, caretPending := 0, false
	graphemes := uniseg.NewGraphemes(input.Value[input.Offset:])
	for graphemes.Next() {
		from, to := graphemes.Positions()
		from, to = from+input.Offset, to+input.Offset
		if from == input.Cursor {
			caretPending = focused
		}
		cluster := graphemes.Str()
		width := render.GraphemeWidth(cluster)
		if width == 0 {
			canvas.Put(line.X+x, line.Y, cluster, normal)
			continue
		}
		if x+width > line.W {
			break
		}
		paint := normal
		if from >= selectedStart && to <= selectedEnd && selectedStart != selectedEnd {
			paint = selection
		}
		if caretPending {
			paint, caretPending = caret, false
		}
		canvas.Put(line.X+x, line.Y, cluster, paint)
		x += width
	}
	if !focused || !caretPending && input.Cursor != len(input.Value) {
		return
	}
	cursorX := textCells(input.Value[input.Offset:input.Cursor])
	if cursorX >= 0 && cursorX < line.W {
		canvas.Put(line.X+cursorX, line.Y, " ", caret)
	}
}

func drawPlaceholder(canvas *render.Canvas, line render.Rect, value string, placeholder, caret render.Style, focused bool) {
	x, first := 0, true
	graphemes := uniseg.NewGraphemes(sanitizeInput(value))
	for graphemes.Next() {
		cluster := graphemes.Str()
		width := render.GraphemeWidth(cluster)
		if width == 0 {
			continue
		}
		if x+width > line.W {
			break
		}
		paint := placeholder
		if focused && first {
			paint = caret
		}
		canvas.Put(line.X+x, line.Y, cluster, paint)
		x += width
		first = false
	}
	if focused && first && line.W > 0 {
		canvas.Put(line.X, line.Y, " ", caret)
	}
}

func sanitizeInput(value string) string {
	if utf8.ValidString(value) {
		clean := true
		for _, r := range value {
			if unicode.IsControl(r) {
				clean = false
				break
			}
		}
		if clean {
			return value
		}
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	var result strings.Builder
	result.Grow(len(value))
	previousCR := false
	for _, r := range value {
		switch r {
		case '\r':
			result.WriteByte(' ')
			previousCR = true
		case '\n':
			if !previousCR {
				result.WriteByte(' ')
			}
			previousCR = false
		case '\t':
			result.WriteByte(' ')
			previousCR = false
		default:
			previousCR = false
			if !unicode.IsControl(r) {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

func limitText(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	return graphemePrefix(value, maxBytes)
}

// graphemePrefix uses a negative limit for unlimited input and never cuts an
// extended grapheme cluster.
func graphemePrefix(value string, maxBytes int) string {
	if maxBytes < 0 || len(value) <= maxBytes {
		return value
	}
	end := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		_, next := graphemes.Positions()
		if next > maxBytes {
			break
		}
		end = next
	}
	return value[:end]
}

func floorBoundary(value string, position int) int {
	position = min(max(position, 0), len(value))
	if position == len(value) {
		return position
	}
	boundary := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		from, to := graphemes.Positions()
		if position < to {
			if position == to {
				return to
			}
			return from
		}
		boundary = to
	}
	return boundary
}

func floorBoundaries(value string, cursor, anchor, offset int) (int, int, int) {
	positions := [3]int{cursor, anchor, offset}
	result := [3]int{}
	maximum := 0
	for index := range positions {
		positions[index] = min(max(positions[index], 0), len(value))
		maximum = max(maximum, positions[index])
	}
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		_, to := graphemes.Positions()
		for index, position := range positions {
			if to <= position {
				result[index] = to
			}
		}
		if to >= maximum {
			break
		}
	}
	return result[0], result[1], result[2]
}

func (t *TextInput) selection() (start, end int) {
	if t.Cursor < t.Anchor {
		return t.Cursor, t.Anchor
	}
	return t.Anchor, t.Cursor
}

func ceilBoundary(value string, position int) int {
	position = min(max(position, 0), len(value))
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		from, to := graphemes.Positions()
		if position == from {
			return from
		}
		if position < to {
			return to
		}
	}
	return len(value)
}

func previousBoundary(value string, position int) int {
	position = floorBoundary(value, position)
	previous := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		from, _ := graphemes.Positions()
		if from >= position {
			break
		}
		previous = from
	}
	return previous
}

func nextBoundary(value string, position int) int {
	position = floorBoundary(value, position)
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		_, to := graphemes.Positions()
		if to > position {
			return to
		}
	}
	return len(value)
}

func wordLike(value string) bool {
	for _, r := range value {
		if r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) {
			return true
		}
	}
	return false
}

func previousWord(value string, position int) int {
	position = floorBoundary(value, position)
	start, target := 0, 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		_, end := graphemes.Positions()
		if !graphemes.IsWordBoundary() {
			continue
		}
		if wordLike(value[start:end]) {
			if start >= position {
				break
			}
			target = start
		}
		start = end
		if start >= position {
			break
		}
	}
	return target
}

func nextWord(value string, position int) int {
	position = floorBoundary(value, position)
	start := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		_, end := graphemes.Positions()
		if !graphemes.IsWordBoundary() {
			continue
		}
		if start > position && wordLike(value[start:end]) {
			return start
		}
		start = end
	}
	return len(value)
}

func byteAtCell(value string, offset, cell, width int) int {
	offset = floorBoundary(value, offset)
	if cell <= 0 || width <= 0 {
		return offset
	}
	x := 0
	graphemes := uniseg.NewGraphemes(value[offset:])
	for graphemes.Next() {
		from, to := graphemes.Positions()
		from, to = from+offset, to+offset
		cells := render.GraphemeWidth(graphemes.Str())
		if cells == 0 {
			continue
		}
		if x+cells > width {
			return from
		}
		if cell < x+cells {
			if cell-x >= (cells+1)/2 {
				return to
			}
			return from
		}
		x += cells
		if x >= width {
			return to
		}
	}
	return len(value)
}

func textCells(value string) int {
	width := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		width += render.GraphemeWidth(graphemes.Str())
	}
	return width
}
