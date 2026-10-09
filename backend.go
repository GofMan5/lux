package lux

import (
	"context"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// Backend is the hexagonal port between the runtime and a terminal transport.
// Implementations own terminal state but never application state. NextEvent and
// Present must honor context cancellation promptly; Close may run concurrently.
type Backend interface {
	Open(context.Context) (capability.Set, error)
	NextEvent(context.Context) (event.Event, error)
	Present(context.Context, *render.Buffer) error
	Size() (width, height int, err error)
	Close() error
}

// AnnexWriter is an optional Backend capability for control sidebands that
// are not screen content: taskbar progress, desktop notifications, and
// clipboard writes. The runtime queues annex bytes during Model.View and
// hands them over through this interface immediately after the same
// frame's successful Present, on the presentation goroutine — so the
// bytes serialize with frame content instead of racing it, and a dropped
// frame drops its annex with it. A backend that does not implement
// AnnexWriter silently ignores annex requests, exactly as a terminal
// without the corresponding protocol silently ignores the sequences.
type AnnexWriter interface {
	WriteAnnex(ctx context.Context, data []byte) error
}
