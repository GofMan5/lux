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
	"os"
	"runtime"
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

// Sample is the live heap and goroutine count after a forced GC.
type Sample struct {
	Heap       uint64
	Goroutines int
}

// Soak drives model through cycles repetitions of cycle, one event per
// presented frame, and samples after every completed cycle: all its events
// were applied and their frames presented. A cycle should leave application
// state where it found it, so growth across samples is framework-owned
// queues, caches and history.
//
// The run opens at the largest size the cycle resizes to and then resizes to
// width x height. Every runtime surface keeps the capacity of the largest
// frame it drew, and which surface draws a frame is up to the scheduler, so
// one meeting the largest size only after warmup would read as growth.
func Soak(ctx context.Context, model lux.Model, width, height int, cycle []event.Event, cycles int) ([]Sample, error) {
	openWidth, openHeight := width, height
	for _, next := range cycle {
		if next.Kind == event.Resize {
			openWidth, openHeight = max(openWidth, next.Width), max(openHeight, next.Height)
		}
	}
	script := make([]event.Event, 0, 1+len(cycle)*cycles)
	opening := 0
	// A resize to the current size presents no frame, so it would stall the
	// backend; add one only when the size changes.
	if openWidth != width || openHeight != height {
		script, opening = append(script, event.ResizeTo(width, height)), 1
	}
	for range cycles {
		script = append(script, cycle...)
	}
	backend := &soakBackend{ProfileBackend: terminal.NewProfileBackend(openWidth, openHeight, Features, script), cycle: len(cycle), opening: opening, last: len(script) - opening}
	err := lux.Run(ctx, model, lux.WithBackend(backend), lux.WithMaxFPS(240))
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.samples, err
}

// soakBackend samples between cycles. The embedded backend's NextEvent
// returns only after the previous event's frame was presented, so call
// k*cycle sees cycle k finished.
type soakBackend struct {
	*terminal.ProfileBackend
	cycle, opening, last, calls int
	mu                          sync.Mutex
	samples                     []Sample
}

func (b *soakBackend) NextEvent(ctx context.Context) (event.Event, error) {
	next, err := b.ProfileBackend.NextEvent(ctx)
	if err != nil {
		return next, err
	}
	b.calls++
	// An opening resize precedes the cycles and is not counted.
	if index := b.calls - 1 - b.opening; b.cycle > 0 && index > 0 && index <= b.last && index%b.cycle == 0 {
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		b.mu.Lock()
		b.samples = append(b.samples, Sample{Heap: stats.HeapAlloc, Goroutines: runtime.NumGoroutine()})
		b.mu.Unlock()
	}
	return next, nil
}

// CheckBounded soaks a fresh model and fails tb when the live heap after the
// first warmup cycles grows by more than slack bytes, or goroutines grow at
// all, by the end of the run. Warmup absorbs caches and capped histories
// filling to their bounds.
//
// Soaks run thousands of frames paced at 240 FPS, so they are opt-in:
// set LUX_SOAK=1 (CI's soak job does, without race instrumentation).
func CheckBounded(tb testing.TB, width, height int, model lux.Model, cycle []event.Event, cycles, warmup int, slack uint64) {
	tb.Helper()
	if os.Getenv("LUX_SOAK") == "" {
		tb.Skip("long-session soak; set LUX_SOAK=1 to run")
	}
	samples, err := Soak(context.Background(), model, width, height, cycle, cycles)
	if err != nil {
		tb.Fatal(err)
	}
	if len(samples) != cycles || warmup < 0 || warmup >= cycles {
		tb.Fatalf("soak sampled %d of %d cycles (warmup %d)", len(samples), cycles, warmup)
	}
	base, end := samples[warmup], samples[len(samples)-1]
	peak := base.Heap
	for _, sample := range samples[warmup:] {
		peak = max(peak, sample.Heap)
	}
	tb.Logf("live heap after cycle %d: %d B, after cycle %d: %d B (peak %d B); goroutines %d -> %d",
		warmup+1, base.Heap, cycles, end.Heap, peak, base.Goroutines, end.Goroutines)
	if end.Heap > base.Heap+slack {
		tb.Fatalf("live heap grew %d B over %d cycles, slack %d B", end.Heap-base.Heap, cycles-warmup-1, slack)
	}
	if end.Goroutines > base.Goroutines {
		tb.Fatalf("goroutines grew %d -> %d", base.Goroutines, end.Goroutines)
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
