package ui

import (
	"context"
	"errors"
	"testing"
)

type fakeClipboard struct {
	text              string
	readErr, writeErr error
}

func (f *fakeClipboard) Read(context.Context) (string, error) {
	return f.text, f.readErr
}

func (f *fakeClipboard) Write(_ context.Context, value string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.text = value
	return nil
}

func TestTextInputClipboardCopyCutPaste(t *testing.T) {
	ctx := context.Background()
	clip := new(fakeClipboard)
	input := TextInput{MaxBytes: 5}
	input.SetValue("a\u754cb")
	input.Cursor, input.Anchor = len("a"), len("a\u754c")
	if err := input.Copy(ctx, clip); err != nil || clip.text != "\u754c" {
		t.Fatalf("copy text=%q err=%v", clip.text, err)
	}
	if err := input.Cut(ctx, clip); err != nil || input.Value != "ab" {
		t.Fatalf("cut = %#v, %v", input, err)
	}
	if !input.Undo() || input.Value != "a\u754cb" {
		t.Fatalf("undo cut = %#v", input)
	}
	input.Cursor, input.Anchor = 0, len(input.Value)
	clip.text = "\u754cx\r\ny"
	if err := input.Paste(ctx, clip); err != nil || input.Value != "\u754cx " {
		t.Fatalf("bounded paste = %q, %v", input.Value, err)
	}
	if !input.Undo() || input.Value != "a\u754cb" {
		t.Fatalf("paste was not one undo step = %#v", input)
	}
}

func TestTextInputClipboardErrorsPreserveValue(t *testing.T) {
	ctx := context.Background()
	want := errors.New("clipboard failed")
	input := TextInput{}
	input.SetValue("keep")
	input.Cursor, input.Anchor = 0, len(input.Value)
	clip := &fakeClipboard{writeErr: want}
	if err := input.Cut(ctx, clip); !errors.Is(err, want) || input.Value != "keep" {
		t.Fatalf("failed cut value=%q err=%v", input.Value, err)
	}
	clip.writeErr, clip.readErr = nil, want
	if err := input.Paste(ctx, clip); !errors.Is(err, want) || input.Value != "keep" {
		t.Fatalf("failed paste value=%q err=%v", input.Value, err)
	}
	if err := input.Copy(ctx, nil); !errors.Is(err, ErrClipboardUnavailable) {
		t.Fatalf("nil copy err=%v", err)
	}
}

func TestTextAreaClipboardCopyCutPaste(t *testing.T) {
	ctx := context.Background()
	clip := new(fakeClipboard)
	area := &TextArea{}
	area.SetValue("ab\ncd")
	area.Line, area.Offset = 1, len("cd")
	area.AnchorLine, area.AnchorOffset = 0, 0
	if err := area.Copy(ctx, clip); err != nil || clip.text != "ab\ncd" {
		t.Fatalf("copy text=%q err=%v", clip.text, err)
	}
	if err := area.Cut(ctx, clip); err != nil || len(area.Lines) != 1 || area.Lines[0] != "" {
		t.Fatalf("cut lines=%q err=%v", area.Lines, err)
	}
	if area.Line != 0 || area.Offset != 0 {
		t.Fatalf("cut caret=%d:%d", area.Line, area.Offset)
	}
	if !area.Undo() || len(area.Lines) != 2 || area.Lines[0] != "ab" || area.Lines[1] != "cd" {
		t.Fatalf("undo cut lines=%q", area.Lines)
	}
	area.Line, area.Offset = 0, 0
	clip.text = "x\r\ny"
	if err := area.Paste(ctx, clip); err != nil || len(area.Lines) != 3 || area.Lines[0] != "x" ||
		area.Lines[1] != "yab" || area.Lines[2] != "cd" {
		t.Fatalf("paste lines=%q err=%v", area.Lines, err)
	}
	if area.Line != 1 || area.Offset != 1 {
		t.Fatalf("paste caret=%d:%d", area.Line, area.Offset)
	}
	if !area.Undo() || len(area.Lines) != 2 || area.Lines[0] != "ab" || area.Lines[1] != "cd" {
		t.Fatalf("paste was not one undo step lines=%q", area.Lines)
	}
}

func TestTextAreaClipboardErrorsPreserveValue(t *testing.T) {
	ctx := context.Background()
	want := errors.New("clipboard failed")
	area := &TextArea{}
	area.SetValue("ab\ncd")
	area.Line, area.Offset = 0, 0
	area.AnchorLine, area.AnchorOffset = 1, len("cd")
	clip := &fakeClipboard{writeErr: want}
	if err := area.Cut(ctx, clip); !errors.Is(err, want) || area.Lines[0] != "ab" || area.Lines[1] != "cd" {
		t.Fatalf("failed cut lines=%q err=%v", area.Lines, err)
	}
	clip.writeErr, clip.readErr = nil, want
	if err := area.Paste(ctx, clip); !errors.Is(err, want) || area.Lines[0] != "ab" {
		t.Fatalf("failed paste lines=%q err=%v", area.Lines, err)
	}
	if err := area.Copy(ctx, nil); !errors.Is(err, ErrClipboardUnavailable) {
		t.Fatalf("nil copy err=%v", err)
	}
}
