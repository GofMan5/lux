package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func areaAction(next event.Event) Action {
	return Action{Kind: Input, Event: next, Bounds: render.Rect{W: 20, H: 10}}
}

// drawArea paints one focused frame and returns its buffer.
func drawArea(t *testing.T, area *TextArea, width, height int) *render.Buffer {
	t.Helper()
	buffer := render.NewBuffer(width, height)
	ctx := Context{focused: "area"}
	ctx.TextArea("area", render.NewCanvas(buffer), buffer.Bounds(), area, TextAreaStyle{})
	return buffer
}

// typed feeds committed text one event at a time, like a terminal driver.
func typed(area *TextArea, text string) {
	for _, r := range text {
		area.Apply(areaAction(event.Event{Kind: event.Text, Text: string(r)}))
	}
}

func press(area *TextArea, code event.KeyCode, modifiers event.Mod) {
	area.Apply(areaAction(event.KeyPress(code, modifiers)))
}

// areaBoundary reports whether offset is an extended-grapheme boundary of line.
func areaBoundary(line string, offset int) bool {
	return textBoundary(line, offset)
}

// validateLayout pins the wrap cache invariant: every wrapped row is a byte
// slice of its logical line between the recorded starts.
func validateLayout(t *testing.T, area *TextArea) {
	t.Helper()
	area.ensureLayout()
	l := &area.layout
	seen := 0
	for i, line := range area.Lines {
		if len(l.rows[i]) == 0 {
			if line != "" {
				t.Fatalf("line %d %q has no rows", i, line)
			}
			seen += 1
			continue
		}
		if l.starts[i][0] != 0 {
			t.Fatalf("line %d first row starts at %d; want 0", i, l.starts[i][0])
		}
		for r, row := range l.rows[i] {
			start := l.starts[i][r]
			if line[start:start+len(row)] != row {
				t.Fatalf("line %d row %d %q is not the slice at %d of %q", i, r, row, start, line)
			}
		}
		seen += len(l.rows[i])
	}
	if l.totalRows != seen {
		t.Fatalf("totalRows = %d; want %d", l.totalRows, seen)
	}
	for i := 1; i < len(area.Lines); i++ {
		if l.rowOfLine[i] < l.rowOfLine[i-1] {
			t.Fatalf("rowOfLine not monotonic at %d", i)
		}
	}
}

func TestTextAreaSetValueSanitizesAndBounds(t *testing.T) {
	area := &TextArea{MaxBytes: 12}
	area.SetValue("ab\r\nX\x00Y\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466\u754c")
	want := []string{"ab", "XY"}
	if strings.Join(area.Lines, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("lines = %q; want %q (control dropped, oversized cluster cut)", area.Lines, want)
	}
	if area.Text() != "ab\nXY" {
		t.Fatalf("Text() = %q", area.Text())
	}
	if area.Line != 1 || area.Offset != 2 {
		t.Fatalf("caret = %d:%d; want end 1:2", area.Line, area.Offset)
	}

	invalid := &TextArea{}
	invalid.SetValue("a\xffb")
	if invalid.Lines[0] != "a\uFFFDb" || !utf8.ValidString(invalid.Lines[0]) {
		t.Fatalf("invalid bytes = %q", invalid.Lines[0])
	}

	bounded := &TextArea{MaxBytes: 4}
	bounded.SetValue("abcde\nfgh")
	if bounded.Text() != "abcd" {
		t.Fatalf("bounded value = %q; want prefix abcd", bounded.Text())
	}
	if bounded.Line != 0 || bounded.Offset != 4 {
		t.Fatalf("caret = %d:%d", bounded.Line, bounded.Offset)
	}

	empty := &TextArea{}
	empty.SetValue("")
	if len(empty.Lines) != 1 || empty.Lines[0] != "" || empty.Line != 0 || empty.Offset != 0 {
		t.Fatalf("empty value = %#v", empty)
	}
}

func TestTextAreaEditsByExtendedGrapheme(t *testing.T) {
	area := &TextArea{}
	area.SetValue("x e\u0301 \u754c")
	press(area, event.KeyHome, 0)
	for range 3 {
		press(area, event.KeyRight, 0)
	}
	if area.Offset != len("x e\u0301") {
		t.Fatalf("offset after three rights = %d; want %d", area.Offset, len("x e\u0301"))
	}
	press(area, event.KeyBackspace, 0)
	if area.Lines[0] != "x  \u754c" || area.Offset != len("x ") {
		t.Fatalf("backspace = %q@%d; want whole cluster deleted", area.Lines[0], area.Offset)
	}
	press(area, event.KeyDelete, 0)
	if area.Lines[0] != "x \u754c" {
		t.Fatalf("delete = %q", area.Lines[0])
	}

	// Enter splits the line and keeps the tail on the new line.
	split := &TextArea{}
	split.SetValue("ab")
	press(split, event.KeyHome, 0)
	press(split, event.KeyRight, 0)
	press(split, event.KeyEnter, 0)
	if strings.Join(split.Lines, "\x00") != "a\x00b" || split.Line != 1 || split.Offset != 0 {
		t.Fatalf("split = %q caret %d:%d", split.Lines, split.Line, split.Offset)
	}
	press(split, event.KeyBackspace, 0)
	if split.Lines[0] != "ab" || split.Line != 0 || split.Offset != 1 {
		t.Fatalf("join = %q caret %d:%d", split.Lines, split.Line, split.Offset)
	}

	// A multi-part paste splices into the caret line; the tail follows the
	// last inserted part and the caret lands before it.
	splice := &TextArea{}
	splice.SetValue("ab")
	press(splice, event.KeyHome, 0)
	splice.Apply(areaAction(event.Event{Kind: event.Paste, Text: "P\nQ"}))
	if strings.Join(splice.Lines, "\x00") != "P\x00Qab" {
		t.Fatalf("splice = %q", splice.Lines)
	}
	if splice.Line != 1 || splice.Offset != 1 || splice.AnchorLine != 1 || splice.AnchorOffset != 1 {
		t.Fatalf("splice caret = %d:%d anchor %d:%d", splice.Line, splice.Offset, splice.AnchorLine, splice.AnchorOffset)
	}
}

func TestTextAreaSelectionWordAndReplacement(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world")
	press(area, event.KeyHome, 0)
	press(area, event.KeyRight, event.ModShift)
	if sL, sO, eL, eO := area.Selection(); sL != 0 || sO != 0 || eL != 0 || eO != 1 {
		t.Fatalf("shift right selection = %d:%d-%d:%d", sL, sO, eL, eO)
	}
	if area.SelectedText() != "h" {
		t.Fatalf("selected = %q", area.SelectedText())
	}

	// Selection across a line break survives the round trip.
	cross := &TextArea{}
	cross.SetValue("ab\ncd")
	press(cross, event.KeyHome, event.ModCtrl)
	press(cross, event.KeyDown, event.ModShift)
	if cross.SelectedText() != "ab\n" {
		t.Fatalf("cross-line selected = %q", cross.SelectedText())
	}
	press(cross, event.KeyEnd, event.ModShift)
	if cross.SelectedText() != "ab\ncd" {
		t.Fatalf("full drag selected = %q", cross.SelectedText())
	}

	// Ctrl+A selects the whole document; typing replaces it.
	all := &TextArea{}
	all.SetValue("ab\ncd")
	all.Apply(areaAction(event.RuneKey('a', event.ModCtrl)))
	if all.SelectedText() != "ab\ncd" {
		t.Fatalf("select all = %q", all.SelectedText())
	}
	typed(all, "z")
	if all.Text() != "z" {
		t.Fatalf("typed over selection = %q", all.Text())
	}

	// ReplaceSelection collapses lines and is one undo step.
	repl := &TextArea{}
	repl.SetValue("ab\ncd")
	repl.Apply(areaAction(event.RuneKey('a', event.ModCtrl)))
	if !repl.ReplaceSelection("P\nQ\nR") {
		t.Fatal("replace reported no change")
	}
	if repl.Text() != "P\nQ\nR" || repl.Line != 2 || repl.Offset != 1 {
		t.Fatalf("replace = %q caret %d:%d", repl.Text(), repl.Line, repl.Offset)
	}
	if !repl.Undo() || repl.Text() != "ab\ncd" {
		t.Fatalf("undo replace = %q", repl.Text())
	}
}

func TestTextAreaUndoRedoAndInvalidation(t *testing.T) {
	area := &TextArea{}
	typed(area, "abc")
	if !area.Undo() || area.Text() != "ab" {
		t.Fatalf("undo 1 = %q", area.Text())
	}
	if !area.Undo() || area.Text() != "a" {
		t.Fatalf("undo 2 = %q", area.Text())
	}
	if !area.Undo() || area.Text() != "" {
		t.Fatalf("undo 3 = %q", area.Text())
	}
	if area.Undo() {
		t.Fatal("undo past the beginning reported change")
	}
	for range 3 {
		area.Redo()
	}
	if area.Text() != "abc" {
		t.Fatalf("redo chain = %q", area.Text())
	}

	// A multi-line paste is one step and one undo.
	paste := &TextArea{}
	paste.Apply(areaAction(event.Event{Kind: event.Paste, Text: "1\n2\n3"}))
	if paste.Text() != "1\n2\n3" {
		t.Fatalf("paste = %q", paste.Text())
	}
	if !paste.Undo() || paste.Text() != "" {
		t.Fatalf("undo paste = %q", paste.Text())
	}
	if !paste.Redo() || paste.Text() != "1\n2\n3" {
		t.Fatalf("redo paste = %q", paste.Text())
	}

	// A new edit clears the redo stack; a rejected edit keeps it.
	paste.Undo()
	typed(paste, "z")
	if paste.Redo() {
		t.Fatal("redo survived a new edit")
	}
	paste.Undo()
	if paste.Apply(areaAction(event.KeyPress(event.KeyBackspace, 0))) {
		t.Fatal("backspace on an empty area reported change")
	}
	if !paste.Redo() || paste.Text() != "z" {
		t.Fatalf("rejected edit cleared redo: %q", paste.Text())
	}

	// Undo restores the caret of the recorded state.
	caret := &TextArea{}
	typed(caret, "abc")
	caret.Undo()
	if caret.Line != 0 || caret.Offset != 2 {
		t.Fatalf("undo caret = %d:%d; want 0:2", caret.Line, caret.Offset)
	}
}

func TestTextAreaUndoBoundsAndDisable(t *testing.T) {
	bounded := &TextArea{UndoBytes: 64}
	typed(bounded, strings.Repeat("x", 30))
	if bounded.undoBytes > 64 {
		t.Fatalf("undo stack = %d bytes; want <= 64", bounded.undoBytes)
	}
	undos := 0
	for bounded.Undo() {
		undos++
	}
	// Snapshot sizes grow with the document (2, 3, ... 32 bytes after each
	// keystroke), so a 64-byte budget keeps only the newest few.
	if undos < 2 {
		t.Fatalf("only %d undos survived a 64-byte budget", undos)
	}
	if bounded.undoBytes > 64 {
		t.Fatalf("undo stack grew back to %d bytes", bounded.undoBytes)
	}

	disabled := &TextArea{UndoBytes: -1}
	typed(disabled, "abc")
	if disabled.Undo() || disabled.Redo() {
		t.Fatal("negative UndoBytes must disable history")
	}

	big := &TextArea{}
	typed(big, "x")
	big.Apply(areaAction(event.Event{Kind: event.Paste, Text: strings.Repeat("y", 100<<10)}))
	if len(big.Lines[0]) != (100<<10)+1 {
		t.Fatalf("huge paste = %d bytes; want %d", len(big.Lines[0]), (100<<10)+1)
	}
	// The paste's own before-snapshot is tiny, so undo still works; the
	// oversize state lands on the redo stack and must be dropped there.
	if !big.Undo() || len(big.Lines[0]) != 1 {
		t.Fatal("huge paste must undo to the tiny before-state")
	}
	if big.Redo() {
		t.Fatal("oversize redo snapshot must be dropped")
	}
	// An edit whose before-state itself exceeds the budget clears history.
	big.Apply(areaAction(event.Event{Kind: event.Paste, Text: strings.Repeat("y", 100<<10)}))
	typed(big, "z")
	if big.Undo() {
		t.Fatal("oversize snapshot must clear history")
	}
}

func TestTextAreaUndoRedoRespectMaxBytes(t *testing.T) {
	area := &TextArea{MaxBytes: 4}
	area.SetValue("ab")
	area.Apply(areaAction(event.Event{Kind: event.Paste, Text: "XYZWQ"}))
	if area.Text() != "abXY" || area.Line != 0 || area.Offset != 4 {
		t.Fatalf("bounded paste = %q caret %d:%d", area.Text(), area.Line, area.Offset)
	}
	if area.Apply(areaAction(event.Event{Kind: event.Paste, Text: "Z"})) {
		t.Fatal("paste at the byte budget reported change")
	}
	if area.Text() != "abXY" {
		t.Fatalf("rejected paste changed text to %q", area.Text())
	}
	if !area.Undo() || area.Text() != "ab" {
		t.Fatalf("undo bounded paste = %q", area.Text())
	}

	// The budget never splits a cluster: a 3-byte cell cannot fit 2 bytes.
	cluster := &TextArea{MaxBytes: 2}
	cluster.Apply(areaAction(event.Event{Kind: event.Paste, Text: "\u754cx"}))
	if cluster.Text() != "" {
		t.Fatalf("cluster-only paste = %q; want rejected", cluster.Text())
	}
}

func TestTextAreaWrappedNavigation(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world")
	drawArea(t, area, 5, 2) // rows: "hello" / "world"
	press(area, event.KeyHome, 0)

	press(area, event.KeyDown, 0)
	if area.Offset != 6 {
		t.Fatalf("down offset = %d; want 6 (start of second row)", area.Offset)
	}
	press(area, event.KeyDown, 0)
	if area.Offset != 6 {
		t.Fatalf("down past the last row moved to %d", area.Offset)
	}
	press(area, event.KeyUp, 0)
	if area.Offset != 0 {
		t.Fatalf("up offset = %d; want 0", area.Offset)
	}

	// End stops at the end of the visual row, before the consumed space.
	press(area, event.KeyEnd, 0)
	if area.Offset != 5 {
		t.Fatalf("end offset = %d; want 5", area.Offset)
	}
	press(area, event.KeyRight, 0)
	if area.Offset != 6 {
		t.Fatalf("right over the gap offset = %d; want 6", area.Offset)
	}
	press(area, event.KeyHome, 0)
	if area.Offset != 6 {
		t.Fatalf("home on second row offset = %d; want 6", area.Offset)
	}
	press(area, event.KeyHome, event.ModCtrl)
	if area.Line != 0 || area.Offset != 0 {
		t.Fatalf("ctrl home = %d:%d", area.Line, area.Offset)
	}
	press(area, event.KeyEnd, event.ModCtrl)
	if area.Line != 0 || area.Offset != 11 {
		t.Fatalf("ctrl end = %d:%d", area.Line, area.Offset)
	}

	// The goal column sticks through short rows.
	sticky := &TextArea{}
	sticky.SetValue("aaa\na")
	drawArea(t, sticky, 10, 4)
	press(sticky, event.KeyHome, event.ModCtrl)
	press(sticky, event.KeyEnd, 0)
	press(sticky, event.KeyDown, 0)
	if sticky.Line != 1 || sticky.Offset != 1 {
		t.Fatalf("down to short line = %d:%d; want 1:1", sticky.Line, sticky.Offset)
	}
	press(sticky, event.KeyUp, 0)
	if sticky.Line != 0 || sticky.Offset != 3 {
		t.Fatalf("up restores goal column = %d:%d; want 0:3", sticky.Line, sticky.Offset)
	}

	// PageUp and PageDown move by the routed bounds height.
	area.Apply(areaAction(event.KeyPress(event.KeyPageDown, 0)))
	if area.Offset != 11 {
		t.Fatalf("page down offset = %d; want 11", area.Offset)
	}
	area.Apply(areaAction(event.KeyPress(event.KeyPageUp, 0)))
	if area.Offset != 5 {
		t.Fatalf("page up offset = %d; want 5 (row 0 end)", area.Offset)
	}
}

func TestTextAreaCaretAndMouseAcrossSoftBreaks(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world")
	drawArea(t, area, 5, 2)

	// Routed mouse coordinates arrive region-local on the Action itself.
	click := func(target *TextArea, x, y int, mod event.Mod) {
		ev := event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: x, Y: y, Mod: mod}
		action := areaAction(event.Event{Kind: event.Mouse, Mouse: ev})
		action.X, action.Y = x, y
		target.Apply(action)
	}
	drag := func(target *TextArea, x, y int) {
		ev := event.MouseEvent{Action: event.MouseMove, Button: event.MouseLeft, X: x, Y: y}
		action := areaAction(event.Event{Kind: event.Mouse, Mouse: ev})
		action.X, action.Y = x, y
		target.Apply(action)
	}
	click(area, 4, 0, 0)
	if area.Offset != 4 {
		t.Fatalf("click row 0 col 4 offset = %d", area.Offset)
	}
	click(area, 0, 1, 0)
	if area.Offset != 6 {
		t.Fatalf("click row 1 col 0 offset = %d; want 6", area.Offset)
	}
	click(area, 7, 0, 0)
	if area.Offset != 5 {
		t.Fatalf("click past a short row offset = %d; want 5 (row end)", area.Offset)
	}
	click(area, 9, 9, 0)
	if area.Offset != 11 {
		t.Fatalf("click beyond the area offset = %d; want doc end", area.Offset)
	}

	// A drag extends the selection and keeps the anchor.
	click(area, 0, 0, 0)
	drag(area, 0, 1)
	if area.SelectedText() != "hello " {
		t.Fatalf("drag selected = %q", area.SelectedText())
	}

	// Shift-click extends from the caret.
	click(area, 0, 0, 0)
	click(area, 0, 1, event.ModShift)
	if area.SelectedText() != "hello " {
		t.Fatalf("shift-click selected = %q", area.SelectedText())
	}

	// Wide cells map to the cell they painted.
	wide := &TextArea{}
	wide.SetValue("a\u754c\u754cb")
	drawArea(t, wide, 3, 2)
	click(wide, 1, 0, 0)
	if wide.Offset != len("a") {
		t.Fatalf("wide left-half click = %d", wide.Offset)
	}
	click(wide, 2, 0, 0)
	if wide.Offset != len("a\u754c") {
		t.Fatalf("wide right-half click = %d", wide.Offset)
	}

	// A tab in a consumed gap positions on the row it belongs to.
	tabs := &TextArea{}
	tabs.SetValue("ab\tcd")
	drawArea(t, tabs, 3, 2)
	click(tabs, 0, 1, 0)
	if tabs.Offset != 3 {
		t.Fatalf("click after tab gap = %d; want 3", tabs.Offset)
	}
	click(tabs, 2, 0, 0)
	if tabs.Offset != 2 {
		t.Fatalf("click row 0 end = %d; want 2", tabs.Offset)
	}
}

func TestTextAreaScrollFollowsCaretAndWheel(t *testing.T) {
	area := &TextArea{}
	area.SetValue("1\n2\n3\n4\n5")
	buffer := drawArea(t, area, 10, 2)
	if area.Scroll.Offset != 3 {
		t.Fatalf("scroll offset = %d; want 3 (caret row revealed)", area.Scroll.Offset)
	}
	if got := buffer.Cell(0, 0).Grapheme + buffer.Cell(0, 1).Grapheme; got != "45" {
		t.Fatalf("viewport = %q; want 45", got)
	}

	press(area, event.KeyHome, event.ModCtrl)
	buffer = drawArea(t, area, 10, 2)
	if area.Scroll.Offset != 0 || buffer.Cell(0, 0).Grapheme != "1" {
		t.Fatalf("home scroll = %d cell %q", area.Scroll.Offset, buffer.Cell(0, 0).Grapheme)
	}

	wheel := event.MouseEvent{Action: event.MouseWheel, Button: event.MouseWheelDown}
	if !area.Apply(Action{Kind: Scroll, Event: event.Event{Kind: event.Mouse, Mouse: wheel}, Bounds: render.Rect{W: 10, H: 2}}) {
		t.Fatal("wheel down did not scroll")
	}
	if area.Scroll.Offset != 1 {
		t.Fatalf("wheel offset = %d; want 1", area.Scroll.Offset)
	}
	for range 4 {
		area.Apply(Action{Kind: Scroll, Event: event.Event{Kind: event.Mouse, Mouse: wheel}, Bounds: render.Rect{W: 10, H: 2}})
	}
	if area.Scroll.Offset != 3 {
		t.Fatalf("wheel clamped at = %d; want 3", area.Scroll.Offset)
	}
	if area.Apply(Action{Kind: Scroll, Event: event.Event{Kind: event.Mouse, Mouse: wheel}, Bounds: render.Rect{W: 10, H: 2}}) {
		t.Fatal("wheel at the bottom reported movement")
	}
	wheel.Button = event.MouseWheelUp
	for range 10 {
		area.Apply(Action{Kind: Scroll, Event: event.Event{Kind: event.Mouse, Mouse: wheel}, Bounds: render.Rect{W: 10, H: 2}})
	}
	if area.Scroll.Offset != 0 {
		t.Fatalf("wheel up clamped at = %d", area.Scroll.Offset)
	}

	// Drawing after an out-of-range offset clamps it back. The area's caret
	// sits on the last row so the reveal does not pull the offset up.
	fresh := &TextArea{}
	fresh.SetValue("1\n2\n3\n4\n5")
	drawArea(t, fresh, 10, 2)
	fresh.Scroll.Offset = 99
	buffer = drawArea(t, fresh, 10, 2)
	if fresh.Scroll.Offset != 3 {
		t.Fatalf("draw clamped offset = %d; want 3", fresh.Scroll.Offset)
	}
	if buffer.Cell(0, 0).Grapheme != "4" {
		t.Fatalf("clamped viewport = %q", buffer.Cell(0, 0).Grapheme)
	}
}

func TestTextAreaDrawsWrappedRowsAndCaret(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world")
	press(area, event.KeyHome, 0)
	buffer := drawArea(t, area, 5, 2)
	if got := buffer.Cell(0, 0).Grapheme + buffer.Cell(1, 0).Grapheme + buffer.Cell(2, 0).Grapheme + buffer.Cell(3, 0).Grapheme + buffer.Cell(4, 0).Grapheme; got != "hello" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := buffer.Cell(0, 1).Grapheme + buffer.Cell(1, 1).Grapheme + buffer.Cell(2, 1).Grapheme + buffer.Cell(3, 1).Grapheme + buffer.Cell(4, 1).Grapheme; got != "world" {
		t.Fatalf("row 1 = %q", got)
	}
	if !buffer.Cell(0, 0).Style.Has(render.AttrReverse) {
		t.Fatalf("caret cell = %#v; want reversed 'h'", buffer.Cell(0, 0))
	}
	if x, y, visible := buffer.Caret(); !visible || x != 0 || y != 0 {
		t.Fatalf("buffer caret = %d,%d visible=%v; want 0,0", x, y, visible)
	}

	// The caret in the consumed-space gap sits at the end of row 0 and its
	// terminal position flows to the next wrapped row.
	press(area, event.KeyEnd, 0)
	buffer = drawArea(t, area, 5, 2)
	if x, y, visible := buffer.Caret(); !visible || x != 0 || y != 1 {
		t.Fatalf("row-end caret = %d,%d visible=%v; want next row start", x, y, visible)
	}

	// Tab stops are 4 cells from the row start.
	tabs := &TextArea{}
	tabs.SetValue("a\tb")
	press(tabs, event.KeyHome, 0)
	press(tabs, event.KeyRight, 0)
	press(tabs, event.KeyRight, 0)
	buffer = drawArea(t, tabs, 5, 1)
	if got := buffer.Cell(0, 0).Grapheme; got != "a" {
		t.Fatalf("tab row[0] = %q", got)
	}
	for col := 1; col <= 3; col++ {
		if got := buffer.Cell(col, 0).Grapheme; got != " " {
			t.Fatalf("tab fill col %d = %q", col, got)
		}
	}
	if got := buffer.Cell(4, 0).Grapheme; got != "b" {
		t.Fatalf("tab row[4] = %q", got)
	}
	if !buffer.Cell(4, 0).Style.Has(render.AttrReverse) {
		t.Fatalf("caret after tab = %#v; want reversed 'b'", buffer.Cell(4, 0))
	}

	// Selection paints in reverse video, exactly the selected rows.
	selecting := &TextArea{}
	selecting.SetValue("hello world")
	drawArea(t, selecting, 5, 2)
	press(selecting, event.KeyHome, event.ModCtrl)
	press(selecting, event.KeyEnd, event.ModShift)
	if selecting.SelectedText() != "hello" {
		t.Fatalf("selected = %q", selecting.SelectedText())
	}
	buffer = drawArea(t, selecting, 5, 2)
	for col := 0; col < 5; col++ {
		if !buffer.Cell(col, 0).Style.Has(render.AttrReverse) {
			t.Fatalf("selected cell %d = %#v", col, buffer.Cell(col, 0))
		}
		if buffer.Cell(col, 1).Style.Has(render.AttrReverse) {
			t.Fatalf("row 1 cell %d selected", col)
		}
	}
}

func TestTextAreaDrawFollowsCanvasWidthPolicy(t *testing.T) {
	ambiguous := render.WidthPolicy{AmbiguousWidth: 2}
	wideBuffer := render.NewBuffer(5, 2)
	wideCanvas := render.NewCanvasWithPolicy(wideBuffer, ambiguous)
	area := &TextArea{}
	area.SetValue("\u00b1\u00b1\u00b1")
	ctx := Context{focused: "area"}
	ctx.TextArea("area", wideCanvas, wideBuffer.Bounds(), area, TextAreaStyle{})
	if area.layout.totalRows != 2 {
		t.Fatalf("ambiguous totalRows = %d; want 2", area.layout.totalRows)
	}
	// Continuation cells carry an empty grapheme; the two painted ± sit at
	// columns 0 and 2.
	if got := wideBuffer.Cell(0, 0).Grapheme + wideBuffer.Cell(2, 0).Grapheme; got != "\u00b1\u00b1" {
		t.Fatalf("ambiguous row 0 = %q", got)
	}
	if !wideBuffer.Cell(1, 0).IsContinuation() || !wideBuffer.Cell(3, 0).IsContinuation() {
		t.Fatalf("ambiguous wide cells = %#v", wideBuffer.Cell(1, 0))
	}
	if area.widthPolicy != ambiguous {
		t.Fatalf("widthPolicy = %#v", area.widthPolicy)
	}

	legacyBuffer := render.NewBuffer(5, 2)
	legacy := &TextArea{}
	legacy.SetValue("\u00b1\u00b1\u00b1")
	legacyCtx := Context{focused: "area"}
	legacyCtx.TextArea("area", render.NewCanvas(legacyBuffer), legacyBuffer.Bounds(), legacy, TextAreaStyle{})
	if legacy.layout.totalRows != 1 {
		t.Fatalf("legacy totalRows = %d; want 1", legacy.layout.totalRows)
	}
	if got := legacyBuffer.Cell(0, 0).Grapheme + legacyBuffer.Cell(1, 0).Grapheme + legacyBuffer.Cell(2, 0).Grapheme; got != "\u00b1\u00b1\u00b1" {
		t.Fatalf("legacy row = %q", got)
	}

	// Clicks map onto the cells the policy painted: under ambiguous-2 the
	// second ± spans columns 2-3, so X=3 lands after it.
	clickAction := areaAction(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 3, Y: 0}})
	clickAction.X = 3
	area.Apply(clickAction)
	if area.Offset != len("\u00b1\u00b1") {
		t.Fatalf("ambiguous click offset = %d; want 4", area.Offset)
	}
}

func TestTextAreaWrapModeChangeInvalidates(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world")
	drawArea(t, area, 5, 3)
	if area.layout.totalRows != 2 {
		t.Fatalf("word totalRows = %d", area.layout.totalRows)
	}
	area.Wrap = render.WrapChar
	buffer := drawArea(t, area, 5, 3)
	if area.layout.totalRows != 3 {
		t.Fatalf("char totalRows = %d; want 3", area.layout.totalRows)
	}
	if got := buffer.Cell(0, 1).Grapheme + buffer.Cell(1, 1).Grapheme + buffer.Cell(2, 1).Grapheme + buffer.Cell(3, 1).Grapheme + buffer.Cell(4, 1).Grapheme; got != " worl" {
		t.Fatalf("char row 1 = %q", got)
	}
	if got := buffer.Cell(0, 2).Grapheme; got != "d" {
		t.Fatalf("char row 2 = %q", got)
	}
	area.Wrap = render.WrapWord
	drawArea(t, area, 5, 3)
	if area.layout.totalRows != 2 {
		t.Fatalf("word totalRows after switch back = %d", area.layout.totalRows)
	}
}

func TestTextAreaLayoutContiguityAndDirtyRange(t *testing.T) {
	area := &TextArea{}
	area.SetValue("hello world\nwrapped lines here\ttab\nunicode \U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466 end")
	drawArea(t, area, 9, 20)
	validateLayout(t, area)
	firstRows := append([]string(nil), area.layout.rows[0]...)

	// Edit the last line: the untouched line's cached rows must survive.
	press(area, event.KeyEnd, event.ModCtrl)
	typed(area, "!")
	validateLayout(t, area)
	if strings.Join(area.layout.rows[0], "\x00") != strings.Join(firstRows, "\x00") {
		t.Fatalf("unedited rows rebuilt: %q vs %q", area.layout.rows[0], firstRows)
	}

	// Splitting a line shifts every following row, not just the edited one.
	press(area, event.KeyHome, event.ModCtrl)
	press(area, event.KeyDown, 0)
	press(area, event.KeyEnd, 0)
	press(area, event.KeyEnter, 0)
	validateLayout(t, area)
	if len(area.Lines) != 4 {
		t.Fatalf("lines = %d; want 4", len(area.Lines))
	}
	drawArea(t, area, 9, 30)
	validateLayout(t, area)

	// Undo and redo keep the invariant as well.
	area.Undo()
	drawArea(t, area, 9, 30)
	validateLayout(t, area)
	area.Redo()
	drawArea(t, area, 9, 30)
	validateLayout(t, area)
}

func TestTextAreaInteractionModesAndFlags(t *testing.T) {
	canvas := render.NewCanvas(render.NewBuffer(8, 3))
	var ctx Context
	ctx.Begin(&lux.Frame{Canvas: canvas})
	area := &TextArea{}
	area.SetValue("hi")

	ctx.TextArea("normal", canvas, canvas.Bounds(), area, TextAreaStyle{})
	if f := ctx.building[0].flags; f&(Focusable|Editable|Scrollable) != Focusable|Editable|Scrollable {
		t.Fatalf("normal flags = %#v", f)
	}
	ctx.TextArea("read", canvas, canvas.Bounds(), area, TextAreaStyle{ReadOnly: true})
	if f := ctx.building[1].flags; f != Focusable {
		t.Fatalf("read-only flags = %#v", f)
	}
	ctx.TextArea("off", canvas, canvas.Bounds(), area, TextAreaStyle{Disabled: true})
	if f := ctx.building[2].flags; f&(Disabled|Focusable|Editable|Scrollable) != Disabled|Focusable|Editable|Scrollable {
		t.Fatalf("disabled flags = %#v", f)
	}

	// An unfocused area paints text without a caret.
	plain := &TextArea{}
	plain.SetValue("hi")
	plainCtx := Context{focused: "other"}
	buffer := render.NewBuffer(4, 1)
	plainCtx.TextArea("area", render.NewCanvas(buffer), buffer.Bounds(), plain, TextAreaStyle{})
	if buffer.Cell(0, 0).Grapheme != "h" || buffer.Cell(0, 0).Style.Has(render.AttrReverse) {
		t.Fatalf("unfocused cell = %#v", buffer.Cell(0, 0))
	}
	if _, _, visible := buffer.Caret(); visible {
		t.Fatal("unfocused area painted a caret")
	}
}

// A coalesced auto-repeat burst (win32/kitty Repeat > 1) deletes or moves no
// more than the document holds: moveBound caps the burst by the grapheme
// count plus line breaks, and the loop stops once nothing is left.
func TestTextAreaRepeatsAreBounded(t *testing.T) {
	area := &TextArea{}
	area.SetValue("ab\ncd\nef")
	burst := areaAction(event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyBackspace, Repeat: 500}})
	area.Apply(burst)
	if text := area.Text(); text != "" {
		t.Fatalf("repeat burst left text %q", text)
	}

	area.SetValue("ab\ncd")
	area.Apply(areaAction(event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyLeft, Repeat: 500}}))
	if area.Line != 0 || area.Offset != 0 {
		t.Fatalf("repeat move landed at %d:%d", area.Line, area.Offset)
	}
	validateLayout(t, area)
}

// A static app paints exactly one frame before any event arrives, so the
// first focusable control must own the caret already there: focus is only
// committed when that frame is presented, and no repaint would follow.
func TestTextAreaFirstFramePaintsCaret(t *testing.T) {
	canvas := render.NewCanvas(render.NewBuffer(4, 1))
	var ctx Context
	ctx.Begin(&lux.Frame{Canvas: canvas})
	area := &TextArea{}
	area.SetValue("hi")
	ctx.TextArea("area", canvas, canvas.Bounds(), area, TextAreaStyle{})
	if _, _, visible := canvas.Buffer().Caret(); !visible {
		t.Fatal("first frame did not paint the caret")
	}
}

func FuzzTextAreaInvariants(f *testing.F) {
	f.Add([]byte("e\u0301\U0001F468\u200D\U0001F469\u754c\n\tab"), uint8(3), uint8(7), uint8(11), uint8(5), uint8(2))
	f.Add([]byte{0xff, '\r', '\t', 0x1b, 0}, uint8(255), uint8(0), uint8(4), uint8(9), uint8(64))
	f.Fuzz(func(t *testing.T, raw []byte, line, offset, width, operation, maxBytes uint8) {
		area := &TextArea{MaxBytes: int(maxBytes % 64)}
		area.SetValue(string(raw))
		area.Line, area.Offset = int(line%8), int(offset%32)
		area.AnchorLine, area.AnchorOffset = 0, 0

		action := areaAction(event.KeyPress(event.KeyLeft, event.ModShift))
		switch operation % 13 {
		case 0:
			action.Event = event.KeyPress(event.KeyBackspace, 0)
		case 1:
			action.Event = event.KeyPress(event.KeyDelete, event.ModCtrl)
		case 2:
			action.Event = event.Event{Kind: event.Text, Text: string(raw)}
		case 3:
			action.Event = event.Event{Kind: event.Paste, Text: string(raw)}
		case 4:
			action.Event = event.KeyPress(event.KeyEnter, 0)
		case 5:
			action.Event = event.KeyPress(event.KeyRight, event.ModCtrl|event.ModShift)
		case 6:
			action.Event = event.KeyPress(event.KeyDown, event.ModShift)
		case 7:
			action.Event = event.KeyPress(event.KeyEnd, event.ModCtrl)
		case 8:
			action.Event = event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: int(offset%24) - 4, Y: int(line%8) - 2}}
		case 9:
			action.Event = event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseMove, Button: event.MouseLeft, X: int(offset%32) - 8, Y: int(line%8) - 4}}
		case 10:
			action.Event = event.KeyPress(event.KeyUp, 0)
		case 11:
			area.Undo()
			action.Event = event.KeyPress(event.KeyPageDown, 0)
		case 12:
			area.Redo()
			action.Event = event.KeyPress(event.KeyHome, event.ModCtrl)
		}
		area.Apply(action)

		buffer := render.NewBuffer(int(width%24)+1, 6)
		ctx := Context{focused: "area"}
		ctx.TextArea("area", render.NewCanvas(buffer), buffer.Bounds(), area, TextAreaStyle{})

		if area.Line < 0 || area.Line >= len(area.Lines) || area.AnchorLine < 0 || area.AnchorLine >= len(area.Lines) {
			t.Fatalf("caret %d:%d anchor %d:%d out of %d lines", area.Line, area.Offset, area.AnchorLine, area.AnchorOffset, len(area.Lines))
		}
		total := 0
		for _, l := range area.Lines {
			if !utf8.ValidString(l) || strings.ContainsAny(l, "\r\n") {
				t.Fatalf("line %q is invalid", l)
			}
			total += len(l)
		}
		if area.MaxBytes > 0 && total > area.MaxBytes {
			t.Fatalf("total %d exceeds MaxBytes %d", total, area.MaxBytes)
		}
		if !areaBoundary(area.Lines[area.Line], area.Offset) || !areaBoundary(area.Lines[area.AnchorLine], area.AnchorOffset) {
			t.Fatalf("offsets %d:%d %d:%d not on grapheme boundaries", area.Line, area.Offset, area.AnchorLine, area.AnchorOffset)
		}
		if area.Offset > len(area.Lines[area.Line]) || area.AnchorOffset > len(area.Lines[area.AnchorLine]) {
			t.Fatalf("offsets beyond line lengths")
		}
		validateLayout(t, area)
		if area.Scroll.Offset < 0 {
			t.Fatalf("negative scroll offset %d", area.Scroll.Offset)
		}
	})
}

func BenchmarkTextAreaLayout(b *testing.B) {
	multiline := make([]string, 400)
	for i := range multiline {
		multiline[i] = strings.Repeat("hello wrapped line number ", 1+(i%3)) + fmt.Sprint(i)
	}
	single := strings.Repeat("hello wrapped line ", 6554) // ~128 KiB, one line
	for _, bench := range []struct {
		name  string
		lines []string
	}{
		{"many-lines-400", multiline},
		{"single-line-128KiB", []string{single}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				area := &TextArea{}
				area.SetValue(strings.Join(bench.lines, "\n"))
				area.buildLayout(80, render.WidthPolicy{})
			}
		})
	}
}

func BenchmarkTextAreaKeystroke(b *testing.B) {
	// Dirty-range: editing line 200 re-wraps only that line.
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = strings.Repeat("hello wrapped line ", 3) + fmt.Sprint(i)
	}
	many := &TextArea{}
	many.SetValue(strings.Join(lines, "\n"))
	many.buildLayout(80, render.WidthPolicy{})
	many.Line, many.Offset = 200, 10
	typeKey := areaAction(event.KeyPress(event.KeyRune, 0))
	typeKey.Event.Key.Rune = 'x'
	b.Run("many-lines", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			many.Apply(typeKey)
			many.Apply(areaAction(event.KeyPress(event.KeyBackspace, 0)))
		}
	})

	// Pathological: one giant logical line re-wraps per keystroke.
	single := &TextArea{}
	single.SetValue(strings.Repeat("hello wrapped line ", 6554))
	single.buildLayout(80, render.WidthPolicy{})
	single.Offset = previousBoundary(single.Lines[0], len(single.Lines[0])/2)
	single.AnchorOffset = single.Offset
	b.Run("single-line-128KiB", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			single.Apply(typeKey)
			single.Apply(areaAction(event.KeyPress(event.KeyBackspace, 0)))
		}
	})
}

func BenchmarkTextAreaDraw(b *testing.B) {
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = strings.Repeat("hello wrapped line ", 3) + fmt.Sprint(i)
	}
	area := &TextArea{}
	area.SetValue(strings.Join(lines, "\n"))
	buffer := render.NewBuffer(80, 24)
	ctx := Context{focused: "area"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx.TextArea("area", render.NewCanvas(buffer), buffer.Bounds(), area, TextAreaStyle{})
	}
}
