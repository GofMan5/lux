package lux

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/terminal"
	"github.com/GofMan5/lux/render"
)

type metricsSample struct {
	step    int
	metrics Metrics
}

type metricsModel struct {
	step    int
	samples chan metricsSample
}

func (m *metricsModel) Update(e event.Event) Command {
	if e.Kind == event.Key {
		m.step++
	}
	return nil
}

func (m *metricsModel) View(f *Frame) {
	step := m.step // callbacks capture values, never the borrowed Frame or model
	f.Canvas.Text(0, 0, "metrics", render.Style{})
	samples := m.samples
	f.OnPresentedWithMetrics(func(metrics Metrics) {
		samples <- metricsSample{step: step, metrics: metrics}
	})
}

type metricsBackend struct{ *fakeBackend }

func (b *metricsBackend) PresentWithTiming(ctx context.Context, frame *render.Buffer) (terminal.PresentTiming, error) {
	err := b.Present(ctx, frame)
	// Sentinel phase durations, not real elapsed time. Flush must not enter
	// the preparation sum, and zero encode is valid for the fused path.
	return terminal.PresentTiming{DiffEncode: 2 * time.Millisecond, Flush: time.Second}, err
}

func TestPresentedMetricsBelongToExactFrameAndExcludeFlush(t *testing.T) {
	for _, timed := range []bool{false, true} {
		name := "legacy-backend"
		if timed {
			name = "timed-backend"
		}
		t.Run(name, func(t *testing.T) {
			fake := newFakeBackend(20, 5)
			var backend Backend = fake
			if timed {
				backend = &metricsBackend{fake}
			}
			model := &metricsModel{samples: make(chan metricsSample, 4)}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- Run(ctx, model, WithBackend(backend), WithMaxFPS(240)) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Error(err)
					}
				case <-time.After(hangTimeout):
					t.Error("runtime did not stop")
				}
			})
			for step := 0; step < 3; step++ {
				if step > 0 {
					fake.events <- event.KeyPress(event.KeyEnter, 0)
				}
				select {
				case sample := <-model.samples:
					if sample.step != step {
						t.Fatalf("presented step = %d, want %d", sample.step, step)
					}
					got := sample.metrics
					if got.BackendTiming != timed {
						t.Fatalf("backend timing availability = %v, want %v", got.BackendTiming, timed)
					}
					if got.PreparationTime != got.UpdateTime+got.ViewTime+got.DiffEncodeTime+got.EncodeTime {
						t.Fatalf("preparation includes unmeasured or flush time: %+v", got)
					}
					if timed && (got.DiffEncodeTime != 2*time.Millisecond || got.FlushTime != time.Second || got.EncodeTime != 0) {
						t.Fatalf("wrong phase timings: %+v", got)
					}
					if !timed && (got.DiffEncodeTime != 0 || got.EncodeTime != 0 || got.FlushTime != 0) {
						t.Fatalf("invented phase timings for legacy backend: %+v", got)
					}
				case <-time.After(hangTimeout):
					t.Fatal("missing causal metrics callback")
				}
			}
		})
	}
}

type droppedModel struct {
	mu    sync.Mutex
	seen  []uint64
	marks map[int]chan struct{} // closed when that many views were drawn
}

func (m *droppedModel) Update(event.Event) Command { return nil }

func (m *droppedModel) View(f *Frame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = append(m.seen, f.Metrics.DroppedFrames)
	if mark, ok := m.marks[len(m.seen)]; ok {
		close(mark)
	}
	if len(m.seen) < 16 {
		f.RequestFrame()
	}
}

// gatedBackend parks every Present until it receives a token from allow,
// or until allow closes, so the loop keeps drawing and displacing queued
// frames behind one in-flight frame.
type gatedBackend struct {
	*fakeBackend
	allow chan struct{}
}

func (b *gatedBackend) Present(ctx context.Context, frame *render.Buffer) error {
	select {
	case <-b.allow:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.fakeBackend.Present(ctx, frame)
}

// Acknowledging an older in-flight frame must not roll the live drop
// counter back to the value captured when that frame was drawn.
func TestFrameMetricsDroppedFramesNeverDecrease(t *testing.T) {
	fake := newFakeBackend(20, 5)
	backend := &gatedBackend{fakeBackend: fake, allow: make(chan struct{})}
	model := &droppedModel{marks: map[int]chan struct{}{8: make(chan struct{}), 12: make(chan struct{}), 16: make(chan struct{})}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, model, WithBackend(backend), WithMaxFPS(240)) }()
	go func() {
		for range fake.presented {
		}
	}()
	wait := func(views int) {
		t.Helper()
		select {
		case <-model.marks[views]:
		case <-time.After(hangTimeout):
			t.Fatalf("stalled before %d views", views)
		}
	}
	wait(8)
	backend.allow <- struct{}{} // acknowledge only the first, oldest frame
	wait(12)
	close(backend.allow)
	wait(16)
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.seen[7] == 0 {
		t.Fatalf("no frames were displaced behind the gate: %v", model.seen)
	}
	for i := 1; i < len(model.seen); i++ {
		if model.seen[i] < model.seen[i-1] {
			t.Fatalf("DroppedFrames went backwards at view %d: %v", i, model.seen)
		}
	}
}
