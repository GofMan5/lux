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
		switch operation % 6 {
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
