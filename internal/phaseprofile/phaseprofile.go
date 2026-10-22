// Package phaseprofile measures per-frame preparation phases of a real model
// driven through the real runtime, without terminal flush. Reference examples
// use it to report preparation p50/p95/p99 against the 8 ms budget.
//
// Phases are timed with time.Now, so samples are quantized to the host clock
// step, reported as clock-step-us: tens of nanoseconds on Linux, but ~0.5-1 ms
// on some Windows hosts, where sub-step phases read 0 and p95 is an upper bound.
package phaseprofile

import (
	"context"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/internal/terminal"
)

// Result holds one sample per presented frame, in presentation order.
type Result struct {
	Frames []lux.Metrics
	Bytes  int
}

// Features is the interactive feature set profiles encode against.
var Features = capability.Set{Interactive: true, Color: capability.TrueColor, SynchronizedOutput: true, ScrollRegions: true}

// Run drives model at width×height through script, one event per presented
// frame, and returns every presented frame's metrics.
func Run(ctx context.Context, model lux.Model, width, height int, script []event.Event) (Result, error) {
	backend := terminal.NewProfileBackend(width, height, Features, script)
	recorder := &recorder{Model: model}
	err := lux.Run(ctx, recorder, lux.WithBackend(backend), lux.WithMaxFPS(240))
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return Result{Frames: recorder.frames, Bytes: backend.Bytes}, err
}

type recorder struct {
	lux.Model
	mu     sync.Mutex
	frames []lux.Metrics
}

func (r *recorder) View(frame *lux.Frame) {
	r.Model.View(frame)
	frame.OnPresentedWithMetrics(func(metrics lux.Metrics) {
		r.mu.Lock()
		r.frames = append(r.frames, metrics)
		r.mu.Unlock()
	})
}

// Percentile returns the nearest-rank p-th percentile (0 < p <= 100) of one
// phase over all frames, or 0 when there are none.
func (r Result) Percentile(p float64, phase func(lux.Metrics) time.Duration) time.Duration {
	if len(r.Frames) == 0 {
		return 0
	}
	values := make([]time.Duration, len(r.Frames))
	for i, frame := range r.Frames {
		values[i] = phase(frame)
	}
	slices.Sort(values)
	rank := int(math.Ceil(p/100*float64(len(values)))) - 1
	return values[min(max(rank, 0), len(values)-1)]
}

// Preparation selects Metrics.PreparationTime for Percentile.
func Preparation(m lux.Metrics) time.Duration { return m.PreparationTime }

// Benchmark runs script against a fresh model b.N times at width×height and
// reports preparation p50/p95/p99 (us) and encoded bytes per frame, pooled
// over every presented frame. ns/op is the whole paced run, not a frame cost.
func Benchmark(b *testing.B, width, height int, newModel func() lux.Model, script []event.Event) {
	b.Helper()
	var pooled Result
	for i := 0; i < b.N; i++ {
		result, err := Run(context.Background(), newModel(), width, height, script)
		if err != nil {
			b.Fatal(err)
		}
		pooled.Frames = append(pooled.Frames, result.Frames...)
		pooled.Bytes += result.Bytes
	}
	us := func(d time.Duration) float64 { return float64(d) / float64(time.Microsecond) }
	b.ReportMetric(us(pooled.Percentile(50, Preparation)), "prep-p50-us")
	b.ReportMetric(us(pooled.Percentile(95, Preparation)), "prep-p95-us")
	b.ReportMetric(us(pooled.Percentile(99, Preparation)), "prep-p99-us")
	b.ReportMetric(us(pooled.Percentile(95, func(m lux.Metrics) time.Duration { return m.UpdateTime })), "update-p95-us")
	b.ReportMetric(us(pooled.Percentile(95, func(m lux.Metrics) time.Duration { return m.ViewTime })), "view-p95-us")
	b.ReportMetric(us(pooled.Percentile(95, func(m lux.Metrics) time.Duration { return m.DiffEncodeTime })), "encode-p95-us")
	b.ReportMetric(us(clockStep()), "clock-step-us")
	b.ReportMetric(float64(len(pooled.Frames))/float64(b.N), "frames/op")
	if len(pooled.Frames) > 0 {
		b.ReportMetric(float64(pooled.Bytes)/float64(len(pooled.Frames)), "bytes/frame")
	}
}

// clockStep returns the largest of a few observed edge-to-edge time.Now steps.
func clockStep() time.Duration {
	var step time.Duration
	for range 3 {
		edge := nextTick(time.Now())
		step = max(step, nextTick(edge).Sub(edge))
	}
	return step
}

func nextTick(from time.Time) time.Time {
	for {
		if now := time.Now(); now.After(from) {
			return now
		}
	}
}
