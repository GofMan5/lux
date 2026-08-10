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
