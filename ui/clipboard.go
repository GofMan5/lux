package ui

import (
	"context"
	"errors"

	"github.com/lux-tui/lux/event"
)

// Clipboard is an application-supplied text clipboard port.
type Clipboard interface {
	Read(context.Context) (string, error)
	Write(context.Context, string) error
}

var ErrClipboardUnavailable = errors.New("ui: clipboard is nil")

// Copy writes the current selection to clipboard.
func (t *TextInput) Copy(ctx context.Context, clipboard Clipboard) error {
	if clipboard == nil {
		return ErrClipboardUnavailable
	}
	if t == nil {
		return clipboard.Write(ctx, "")
	}
	return clipboard.Write(ctx, t.SelectedText())
}

// Cut writes the selection before deleting it as one undoable edit.
func (t *TextInput) Cut(ctx context.Context, clipboard Clipboard) error {
	if err := t.Copy(ctx, clipboard); err != nil {
		return err
	}
	t.ReplaceSelection("")
	return nil
}

// Paste reads clipboard text and routes it through normal paste policy.
func (t *TextInput) Paste(ctx context.Context, clipboard Clipboard) error {
	if clipboard == nil {
		return ErrClipboardUnavailable
	}
	if t == nil {
		return nil
	}
	value, err := clipboard.Read(ctx)
	if err != nil {
		return err
	}
	t.Apply(Action{Kind: Input, Event: event.Event{Kind: event.Paste, Text: value}})
	return nil
}

// Copy writes the TextArea selection, line breaks included, to clipboard.
func (a *TextArea) Copy(ctx context.Context, clipboard Clipboard) error {
	if clipboard == nil {
		return ErrClipboardUnavailable
	}
	if a == nil {
		return clipboard.Write(ctx, "")
	}
	return clipboard.Write(ctx, a.SelectedText())
}

// Cut writes the selection before deleting it as one undoable edit; a
// multi-line cut joins the surrounding lines, which one undo step reverts.
func (a *TextArea) Cut(ctx context.Context, clipboard Clipboard) error {
	if err := a.Copy(ctx, clipboard); err != nil {
		return err
	}
	a.ReplaceSelection("")
	return nil
}

// Paste reads clipboard text and routes it through the area's multiline paste
// policy, so \r\n becomes \n and MaxBytes still bounds the result.
func (a *TextArea) Paste(ctx context.Context, clipboard Clipboard) error {
	if clipboard == nil {
		return ErrClipboardUnavailable
	}
	if a == nil {
		return nil
	}
	value, err := clipboard.Read(ctx)
	if err != nil {
		return err
	}
	a.Apply(Action{Kind: Input, Event: event.Event{Kind: event.Paste, Text: value}})
	return nil
}
