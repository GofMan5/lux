package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/rivo/uniseg"
)

func inputAction(next event.Event) Action {
	return Action{Kind: Input, Event: next, Bounds: render.Rect{W: 20, H: 1}}
}

// TestTextInputIgnoresKeyReleases pins the contract for callers that feed
// TextInput.Apply directly: a kitty release edits nothing, while a repeat
// edits exactly like a press.
func TestTextInputIgnoresKeyReleases(t *testing.T) {
	input := TextInput{}
	input.SetValue("ab")
	release := event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'c', Action: event.ActionRelease}}
	if input.Apply(inputAction(release)) {
		t.Fatal("a rune release was applied as an edit")
	}
	if input.Value != "ab" || input.Cursor != 2 {
		t.Fatalf("release mutated input: %#v", input)
	}
	releaseBackspace := event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyBackspace, Action: event.ActionRelease}}
	if input.Apply(inputAction(releaseBackspace)) || input.Value != "ab" {
		t.Fatalf("backspace release deleted: %q", input.Value)
	}
	repeat := event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'c', Action: event.ActionRepeat, Repeat: 1}}
	if !input.Apply(inputAction(repeat)) || input.Value != "abc" {
		t.Fatalf("rune repeat = %q", input.Value)
	}
}

func TestTextInputSanitizesAndBoundsWholeGraphemes(t *testing.T) {
	input := TextInput{MaxBytes: 4}
	input.SetValue("a\u754cb")
	if input.Value != "a\u754c" || input.Cursor != len(input.Value) || input.Anchor != input.Cursor {
		t.Fatalf("bounded value = %#v", input)
	}
	input.MaxBytes = 0
	input.SetValue("a\r\nb\tc\x1bd" + string([]byte{0xff}) + "z")
	if want := "a b cd\uFFFDz"; input.Value != want || !utf8.ValidString(input.Value) {
		t.Fatalf("sanitized = %q, want %q", input.Value, want)
	}
}

func TestTextInputEditsByExtendedGrapheme(t *testing.T) {
	value := "e\u0301\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466\u754c"
	input := TextInput{}
	input.SetValue(value)
	if !input.Apply(inputAction(event.KeyPress(event.KeyLeft, event.ModShift))) {
		t.Fatal("shift-left did not change selection")
	}
	start, end := input.Selection()
	if got := input.Value[start:end]; got != "\u754c" {
		t.Fatalf("selection = %q", got)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyBackspace, 0)))
	if input.Value != "e\u0301\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466" {
		t.Fatalf("selection delete = %q", input.Value)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyBackspace, 0)))
	if input.Value != "e\u0301" {
		t.Fatalf("ZWJ delete split cluster: %q", input.Value)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyBackspace, 0)))
	if input.Value != "" || input.Cursor != 0 || input.Anchor != 0 {
		t.Fatalf("combining delete = %#v", input)
	}

	input.SetValue("e")
	input.Apply(inputAction(event.RuneKey('\u0301', 0)))
	if input.Value != "e\u0301" || uniseg.GraphemeClusterCount(input.Value) != 1 || !textBoundary(input.Value, input.Cursor) {
		t.Fatalf("combining insertion = %#v", input)
	}
}

func TestTextInputSelectionWordAndReplacement(t *testing.T) {
	input := TextInput{}
	input.SetValue("hello, \u4E16\u754C next")
	input.Cursor, input.Anchor = 0, 0
	input.Apply(inputAction(event.KeyPress(event.KeyRight, event.ModCtrl)))
	if input.Cursor != len("hello, ") {
		t.Fatalf("ctrl-right cursor = %d", input.Cursor)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyLeft, event.ModCtrl|event.ModShift)))
	start, end := input.Selection()
	if start != 0 || end != len("hello, ") {
		t.Fatalf("ctrl-shift selection = %d:%d", start, end)
	}
	input.Apply(inputAction(event.Event{Kind: event.Text, Text: "go"}))
	if input.Value != "go\u4E16\u754C next" || input.Cursor != 2 || input.Anchor != 2 {
		t.Fatalf("replacement = %#v", input)
	}
	input.Apply(inputAction(event.RuneKey('A', event.ModCtrl)))
	start, end = input.Selection()
	if start != 0 || end != len(input.Value) {
		t.Fatalf("ctrl-a = %d:%d", start, end)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyDelete, 0)))
	if input.Value != "" {
		t.Fatalf("delete selection = %q", input.Value)
	}
}

func TestTextInputUndoRedoAndInvalidation(t *testing.T) {
	var input TextInput
	input.SetValue("a")
	input.Apply(inputAction(event.RuneKey('b', 0)))
	input.Apply(inputAction(event.RuneKey('c', 0)))
	if !input.Apply(inputAction(event.RuneKey('z', event.ModCtrl))) || input.Value != "ab" {
		t.Fatalf("undo = %#v", input)
	}
	if !input.Apply(inputAction(event.RuneKey('z', event.ModCtrl|event.ModShift))) || input.Value != "abc" {
		t.Fatalf("shift redo = %#v", input)
	}
	input.Undo()
	input.Apply(inputAction(event.RuneKey('x', 0)))
	if input.Redo() || input.Value != "abx" {
		t.Fatalf("redo survived branch = %#v", input)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyLeft, 0)))
	if !input.Undo() || input.Value != "ab" {
		t.Fatalf("navigation entered history = %#v", input)
	}
	input.SetValue("reset")
	if input.Undo() || input.Redo() {
		t.Fatal("SetValue did not reset history")
	}
}

// TestTextInputHistoryKeyBindings pins the same two binding families as
// the textarea: ctrl+z undo, ctrl+y and ctrl+shift+z redo, and the pure
// alt+u undo alias for terminals where the runtime's suspend handshake
// owns ctrl+z. Impure alt chords and super/hyper/meta are never history.
func TestTextInputHistoryKeyBindings(t *testing.T) {
	newInput := func() *TextInput {
		input := &TextInput{}
		input.SetValue("a")
		input.Apply(inputAction(event.RuneKey('b', 0)))
		input.Apply(inputAction(event.RuneKey('c', 0)))
		return input
	}

	input := newInput()
	if !input.Apply(inputAction(event.RuneKey('u', event.ModAlt))) || input.Value != "ab" {
		t.Fatalf("alt+u undo = %q", input.Value)
	}
	if !input.Apply(inputAction(event.RuneKey('y', event.ModCtrl))) || input.Value != "abc" {
		t.Fatalf("ctrl+y redo = %q", input.Value)
	}
	if !input.Apply(inputAction(event.RuneKey('z', event.ModCtrl))) || input.Value != "ab" {
		t.Fatalf("ctrl+z undo = %q", input.Value)
	}
	if !input.Apply(inputAction(event.RuneKey('Z', event.ModCtrl))) || input.Value != "a" {
		t.Fatalf("legacy ctrl+z undo = %q", input.Value)
	}
	if !input.Apply(inputAction(event.RuneKey('z', event.ModCtrl|event.ModShift))) || input.Value != "ab" {
		t.Fatalf("ctrl+shift+z redo = %q", input.Value)
	}
	// Caps lock does not disqualify the alias.
	caps := newInput()
	if !caps.Apply(inputAction(event.RuneKey('U', event.ModAlt|event.ModCapsLock))) || caps.Value != "ab" {
		t.Fatalf("alt+capslock+u undo = %q", caps.Value)
	}

	for _, tc := range []struct {
		name string
		mod  event.Mod
	}{
		{"ctrl+alt+u is not the alias", event.ModAlt | event.ModCtrl},
		{"alt+shift+u is not the alias", event.ModAlt | event.ModShift},
		{"super+u never touches history", event.ModSuper},
		{"hyper+u never touches history", event.ModHyper},
		{"meta+u never touches history", event.ModMeta},
	} {
		input := newInput()
		input.Apply(inputAction(event.RuneKey('u', tc.mod)))
		if input.Value != "abc" {
			t.Fatalf("%s: value = %q", tc.name, input.Value)
		}
		input.Apply(inputAction(event.RuneKey('z', event.ModCtrl|tc.mod)))
		if input.Value != "abc" {
			t.Fatalf("%s: ctrl family stolen; value = %q", tc.name, input.Value)
		}
	}
	// A held key with repeats over-undoes and stops at the beginning.
	burst := newInput()
	burst.Apply(inputAction(event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'u', Mod: event.ModAlt, Repeat: 3}}))
	if burst.Value != "a" {
		t.Fatalf("repeated alt+u undo = %q; want %q", burst.Value, "a")
	}
}

func TestTextInputUndoBoundsAndDisable(t *testing.T) {
	input := TextInput{UndoBytes: 2}
	input.SetValue("a")
	input.Apply(inputAction(event.RuneKey('b', 0))) // snapshot "a"
	input.Apply(inputAction(event.RuneKey('c', 0))) // snapshot "ab", evicts "a"
	input.Apply(inputAction(event.RuneKey('d', 0))) // snapshot "abc" cannot fit, clears
	if input.Undo() {
		t.Fatal("oversize snapshot was retained")
	}
	input.UndoBytes = -1
	input.Apply(inputAction(event.RuneKey('e', 0)))
	if input.Undo() {
		t.Fatal("negative UndoBytes retained history")
	}
}

func TestUndoRedoRespectChangedMaxBytes(t *testing.T) {
	var input TextInput
	input.SetValue("abc")
	input.Apply(inputAction(event.RuneKey('d', 0)))
	input.MaxBytes = 1
	if !input.Undo() || input.Value != "a" || len(input.Value) > input.MaxBytes || !textBoundary(input.Value, input.Cursor) {
		t.Fatalf("bounded undo = %#v", input)
	}
	if !input.Redo() || input.Value != "a" || len(input.Value) > input.MaxBytes {
		t.Fatalf("bounded redo = %#v", input)
	}
}

func TestTextInputUndoRestoresSelectionNotViewport(t *testing.T) {
	var input TextInput
	input.SetValue("abcdef")
	input.Cursor, input.Anchor, input.Offset = 4, 1, 1
	input.ReplaceSelection("X")
	input.Offset = 0
	if !input.Undo() || input.Value != "abcdef" || input.Cursor != 4 || input.Anchor != 1 || input.Offset != 0 {
		t.Fatalf("restored state = %#v", input)
	}
}

func TestTextInputRejectedEditDoesNotInvalidateRedo(t *testing.T) {
	input := TextInput{MaxBytes: 1}
	input.SetValue("a")
	input.Cursor, input.Anchor = 0, len(input.Value)
	input.ReplaceSelection("b")
	input.Undo()
	if input.ReplaceSelection("\U0001f642") {
		t.Fatal("oversize edit was accepted")
	}
	if !input.Redo() || input.Value != "b" {
		t.Fatalf("rejected edit invalidated redo: %#v", input)
	}
}

func TestTextInputReplaceSelectionAndWordDelete(t *testing.T) {
	input := TextInput{}
	input.SetValue("go e\u0301lan, \u4e16\u754c next")
	input.Cursor, input.Anchor = len("go e\u0301lan"), len("go ")
	if got := input.SelectedText(); got != "e\u0301lan" {
		t.Fatalf("selection = %q", got)
	}
	if !input.ReplaceSelection("\U0001f642") || input.Value != "go \U0001f642, \u4e16\u754c next" {
		t.Fatalf("replace = %#v", input)
	}
	input.Apply(inputAction(event.KeyPress(event.KeyBackspace, event.ModCtrl)))
	if input.Value != ", \u4e16\u754c next" || !textBoundary(input.Value, input.Cursor) {
		t.Fatalf("ctrl-backspace = %#v", input)
	}
	input.Cursor, input.Anchor = len(", "), len(", ")
	input.Apply(inputAction(event.KeyPress(event.KeyDelete, event.ModCtrl)))
	if input.Value != ", \u754c next" || !textBoundary(input.Value, input.Cursor) {
		t.Fatalf("ctrl-delete = %#v", input)
	}
	if !input.Undo() || input.Value != ", \u4e16\u754c next" {
		t.Fatalf("word delete undo = %#v", input)
	}
}

func TestTextInputFollowsCanvasWidthPolicy(t *testing.T) {
	// A CJK-locale canvas measures ± as two cells, so the field must scroll
	// further and park the caret one column later than a legacy canvas.
	buffer := render.NewBuffer(5, 1)
	canvas := render.NewCanvasWithPolicy(buffer, render.WidthPolicy{AmbiguousWidth: 2})
	ctx := Context{focused: "field"}
	input := TextInput{}
	input.SetValue("±±±")
	ctx.Input("field", canvas, canvas.Bounds(), &input, InputStyle{})
	if input.widthPolicy != (render.WidthPolicy{AmbiguousWidth: 2}) {
		t.Fatalf("input widthPolicy = %#v; want the canvas policy", input.widthPolicy)
	}
	if input.Offset != 2 {
		t.Fatalf("offset = %d; want 2 (one ± scrolled out)", input.Offset)
	}
	if got := buffer.Cell(0, 0); got.Grapheme != "±" || got.Width != 2 {
		t.Fatalf("viewport cell = %#v; want wide ±", got)
	}
	if !buffer.Cell(4, 0).Style.Has(render.AttrReverse) {
		t.Fatalf("caret cell = %#v; want reversed caret at column 4", buffer.Cell(4, 0))
	}

	legacyBuffer := render.NewBuffer(5, 1)
	legacy := render.NewCanvas(legacyBuffer)
	legacyInput := TextInput{}
	legacyInput.SetValue("±±±")
	ctx.Input("field", legacy, legacy.Bounds(), &legacyInput, InputStyle{})
	if legacyInput.widthPolicy != (render.WidthPolicy{}) {
		t.Fatalf("legacy input widthPolicy = %#v; want zero", legacyInput.widthPolicy)
	}
	if legacyInput.Offset != 0 {
		t.Fatalf("legacy offset = %d; want 0", legacyInput.Offset)
	}
	if !legacyBuffer.Cell(3, 0).Style.Has(render.AttrReverse) {
		t.Fatalf("legacy caret cell = %#v; want caret at column 3", legacyBuffer.Cell(3, 0))
	}
}

func TestTextInputMouseMappingUsesWidthPolicy(t *testing.T) {
	// Clicks map onto the cells the policy painted: under ambiguous-2 the
	// second ± spans columns 2-3, so X=2 stays before it and X=3 lands after.
	input := TextInput{}
	input.SetValue("±±±")
	input.widthPolicy = render.WidthPolicy{AmbiguousWidth: 2}
	click := inputAction(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}})
	click.Bounds.W = 6
	click.X = 2
	input.Apply(click)
	if input.Cursor != 2 {
		t.Fatalf("click on left half cursor = %d; want 2", input.Cursor)
	}
	click.X = 3
	input.Apply(click)
	if input.Cursor != 4 {
		t.Fatalf("click on right half cursor = %d; want 4", input.Cursor)
	}

	// Legacy mapping in the same geometry: three single cells.
	legacy := TextInput{}
	legacy.SetValue("±±±")
	legacy.widthPolicy = render.WidthPolicy{}
	legacy.Apply(click)
	if legacy.Cursor != 6 {
		t.Fatalf("legacy click past the value cursor = %d; want 6 (end)", legacy.Cursor)
	}
}

func TestTextInputPasteLimitDoesNotSplitOrDeleteOnReject(t *testing.T) {
	input := TextInput{MaxBytes: 4}
	input.SetValue("ab")
	input.Cursor, input.Anchor = 0, len(input.Value)
	if input.Apply(inputAction(event.Event{Kind: event.Paste, Text: "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466"})) {
		t.Fatalf("oversize paste changed input: %#v", input)
	}
	input.Apply(inputAction(event.Event{Kind: event.Paste, Text: "\u754cx"}))
	if input.Value != "\u754cx" || len(input.Value) != 4 {
		t.Fatalf("bounded paste = %q", input.Value)
	}
}

func TestTextInputMouseMapsWideCellsAndShiftExtends(t *testing.T) {
	input := TextInput{}
	input.SetValue("a\u754cb")
	click := inputAction(event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}})
	click.Bounds.W = 4
	click.X = 1
	input.Apply(click)
	if input.Cursor != len("a") {
		t.Fatalf("wide first cell cursor = %d", input.Cursor)
	}
	click.X = 2
	input.Apply(click)
	if input.Cursor != len("a\u754c") {
		t.Fatalf("wide second cell cursor = %d", input.Cursor)
	}
	click.X = 0
	click.Event.Mouse.Mod = event.ModShift
	input.Apply(click)
	start, end := input.Selection()
	if start != 0 || end != len("a\u754c") {
		t.Fatalf("shift click selection = %d:%d", start, end)
	}
}

func TestContextInputScrollsPaintsCaretAndTinyWideValue(t *testing.T) {
	buffer := render.NewBuffer(3, 1)
	canvas := render.NewCanvas(buffer)
	ctx := Context{focused: "field"}
	input := TextInput{}
	input.SetValue("ab\u754ccd")
	ctx.Input("field", canvas, canvas.Bounds(), &input, InputStyle{})
	if input.Offset != len("ab\u754c") {
		t.Fatalf("offset = %d", input.Offset)
	}
	if got := buffer.Cell(0, 0).Grapheme + buffer.Cell(1, 0).Grapheme; got != "cd" {
		t.Fatalf("viewport = %q", got)
	}
	if !buffer.Cell(2, 0).Style.Has(render.AttrReverse) {
		t.Fatal("end caret was not painted")
	}

	tiny := render.NewBuffer(1, 1)
	tinyCanvas := render.NewCanvas(tiny)
	input.SetValue("\u754c")
	input.Cursor, input.Anchor = 0, 0
	ctx.Input("field", tinyCanvas, tinyCanvas.Bounds(), &input, InputStyle{})
	cell := tiny.Cell(0, 0)
	if cell.Grapheme != " " || cell.Width != 1 || !cell.Style.Has(render.AttrReverse) {
		t.Fatalf("tiny wide caret = %#v", cell)
	}
}

func TestContextInputPlaceholderAndInteractionModes(t *testing.T) {
	buffer := render.NewBuffer(8, 1)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	var ctx Context
	ctx.Begin(frame)
	var input TextInput
	ctx.Input("read", canvas, canvas.Bounds(), &input, InputStyle{ReadOnly: true})
	if len(ctx.building) != 1 || ctx.building[0].flags != Focusable {
		t.Fatalf("read-only flags = %#v", ctx.building)
	}
	ctx.Input("off", canvas, canvas.Bounds(), &input, InputStyle{Disabled: true})
	if len(ctx.building) != 2 || ctx.building[1].flags&Disabled == 0 || ctx.building[1].flags&Editable == 0 {
		t.Fatalf("disabled flags = %#v", ctx.building)
	}

	ctx.focused = "hint"
	input.Placeholder = "\u754c hint"
	ctx.Input("hint", canvas, canvas.Bounds(), &input, InputStyle{})
	if buffer.Cell(0, 0).Grapheme != "\u754c" || !buffer.Cell(0, 0).Style.Has(render.AttrDim) || !buffer.Cell(1, 0).IsContinuation() {
		t.Fatalf("placeholder caret = %#v %#v", buffer.Cell(0, 0), buffer.Cell(1, 0))
	}
}

func FuzzTextInputInvariants(f *testing.F) {
	f.Add([]byte("e\u0301\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466\u754c"), uint8(9), uint8(3), uint8(32), uint8(1))
	f.Add([]byte{0xff, '\r', '\n', '\t', 0x1b}, uint8(255), uint8(0), uint8(4), uint8(4))
	f.Fuzz(func(t *testing.T, raw []byte, cursor, anchor, limit, operation uint8) {
		input := TextInput{Value: string(raw), Cursor: int(cursor), Anchor: int(anchor), Offset: int(anchor), MaxBytes: int(limit)}
		action := inputAction(event.KeyPress(event.KeyLeft, event.ModShift))
		switch operation % 7 {
		case 0:
			action.Event = event.KeyPress(event.KeyBackspace, 0)
		case 1:
			action.Event = event.KeyPress(event.KeyDelete, 0)
		case 2:
			action.Event = event.Event{Kind: event.Text, Text: string(raw)}
		case 3:
			action.Event = event.Event{Kind: event.Paste, Text: string(raw)}
		case 4:
			action.Event = event.KeyPress(event.KeyRight, event.ModCtrl|event.ModShift)
		case 5:
			action.Event = event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft}}
			action.X, action.Bounds.W = int(cursor%16), int(anchor%16)
		case 6:
			// A captured drag reaches the same invariants from arbitrary offsets,
			// including coordinates outside the field.
			action.Event = event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseMove, Button: event.MouseLeft}}
			action.X, action.Bounds.W = int(cursor%32)-8, int(anchor%16)
		}
		input.Apply(action)
		buffer := render.NewBuffer(int(anchor%16), 1)
		var ctx Context
		ctx.Input("fuzz", render.NewCanvas(buffer), buffer.Bounds(), &input, InputStyle{})
		if !utf8.ValidString(input.Value) || input.MaxBytes > 0 && len(input.Value) > input.MaxBytes {
			t.Fatalf("invalid bounded value: %#v", input)
		}
		for _, position := range []int{input.Cursor, input.Anchor, input.Offset} {
			if !textBoundary(input.Value, position) {
				t.Fatalf("offset %d is not a grapheme boundary in %q", position, input.Value)
			}
		}
	})
}

func BenchmarkTextInput(b *testing.B) {
	for _, size := range []int{32, 1024, 64 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			value := strings.Repeat("ab界🙂", max(size/10, 1))
			var input TextInput
			input.SetValue(value)
			action := inputAction(event.KeyPress(event.KeyLeft, event.ModCtrl))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				input.Cursor, input.Anchor = len(input.Value), len(input.Value)
				input.Apply(action)
			}
		})
	}
}

func textBoundary(value string, position int) bool {
	if position == 0 || position == len(value) {
		return true
	}
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		from, to := graphemes.Positions()
		if position == from || position == to {
			return true
		}
	}
	return false
}
