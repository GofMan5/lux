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
