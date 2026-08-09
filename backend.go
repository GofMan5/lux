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
