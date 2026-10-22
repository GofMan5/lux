package terminal

import (
	"context"
	"time"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// ProfileBackend is an in-memory interactive backend for phase profiling. It
// diff-encodes every frame exactly like an interactive Backend with the given
// features and discards the bytes, so the runtime's PreparationTime carries
// the real encoder cost while flush stays zero. It replays a script one event
// per presented frame — the next event waits until the previous one's frame
// reached Present — then ends the run with event.Quit.
type ProfileBackend struct {
	width, height int
	features      capability.Set
	script        []event.Event
	presented     chan struct{}

	// Touched only by the presentation goroutine.
	previous *render.Buffer
	scratch  []byte
	Bytes    int // encoded bytes over the run; read after Run returns
}

// NewProfileBackend returns a profiling backend of the given size. The script
// is not copied and must not change during the run.
func NewProfileBackend(width, height int, features capability.Set, script []event.Event) *ProfileBackend {
	return &ProfileBackend{width: width, height: height, features: features, script: script, presented: make(chan struct{}, 1)}
}

func (b *ProfileBackend) Open(context.Context) (capability.Set, error) { return b.features, nil }

func (b *ProfileBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case <-b.presented:
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
	if len(b.script) == 0 {
		return event.Event{Kind: event.Quit}, nil
	}
	next := b.script[0]
	b.script = b.script[1:]
	return next, nil
}

func (b *ProfileBackend) Present(ctx context.Context, frame *render.Buffer) error {
	_, err := b.PresentWithTiming(ctx, frame)
	return err
}

// PresentWithTiming reports the diff/encode duration of one frame; flush is
// always zero because nothing is written.
func (b *ProfileBackend) PresentWithTiming(ctx context.Context, frame *render.Buffer) (PresentTiming, error) {
	var timing PresentTiming
	if err := ctx.Err(); err != nil {
		return timing, err
	}
	started := time.Now()
	b.scratch = appendFrame(b.scratch[:0], frame, b.previous, b.features.Color, b.features.ScrollRegions, b.features.SynchronizedOutput, false)
	timing.DiffEncode = time.Since(started)
	b.Bytes += len(b.scratch)
	width, height := frame.Size()
	if previousWidth, previousHeight := b.previous.Size(); b.previous == nil || previousWidth != width || previousHeight != height {
		b.previous = frame.Clone()
	} else {
		b.previous.CopyFrom(frame)
	}
	select {
	case b.presented <- struct{}{}:
	default:
	}
	return timing, nil
}

func (b *ProfileBackend) Size() (int, int, error) { return b.width, b.height, nil }

func (b *ProfileBackend) Close() error { return nil }
