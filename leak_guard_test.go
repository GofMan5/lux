package lux

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
)

// The framework owns goroutines for the lifetime of every Run: the writer
// that turns Present calls into terminal writes and annex callbacks, and,
// on real backends, the reader and signal watchers behind them. Every one
// of those must be gone when Run returns. A leaked writer parks a surface
// slot forever, which is exactly the shape the convergence flake hunt is
// chasing in CI; a leaked reader holds a file descriptor after Close.
//
// The fake backend is fully synchronous — it starts no goroutines of its
// own — so any goroutine that survives a completed Run belongs to the
// framework, and this test names it with a full stack dump.
func TestRunReturnsToTheGoroutineBaseline(t *testing.T) {
	// Warm-up: a framework that lazily initializes process-wide state (a
	// global timer, a tracing goroutine) would allocate it on first use
	// and keep it forever. One sacrificial run absorbs that; the measured
	// lifecycles below then prove the steady state, not first-touch noise.
	// The warm-up model never quits, so the run is torn down through its
	// context — which also makes cancellation the first lifecycle the
	// guard exercises.
	warmBackend := newFakeBackend(10, 3)
	warmCtx, warmCancel := context.WithCancel(context.Background())
	warmDone := make(chan error, 1)
	go func() { warmDone <- Run(warmCtx, &testModel{}, WithBackend(warmBackend)) }()
	select {
	case <-warmBackend.presented:
	case <-time.After(hangTimeout):
		warmCancel()
		t.Fatal("warm-up frame timeout")
	}
	warmCancel()
	select {
	case err := <-warmDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("warm-up run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("warm-up run did not return after cancellation")
	}

	baseline := runtime.NumGoroutine()

	// Lifecycle 1: interactive happy path — frames presented, then quit.
	backend := newFakeBackend(20, 5)
	model := &testModel{animate: 8, quitOn: event.Key}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, WithBackend(backend)) }()
	select {
	case <-backend.presented:
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("interactive run did not return after quit")
	}

	// Lifecycle 2: Present fails; Run must unwind the writer on the error
	// path, not only on the happy one.
	failing := newFakeBackend(10, 3)
	failing.presentErr = errors.New("write failed")
	if err := Run(context.Background(), &testModel{}, WithBackend(failing)); !errors.Is(err, failing.presentErr) {
		t.Fatalf("present error = %v", err)
	}

	// Lifecycle 3: cancellation while Present is parked in a blocked
	// write — the machinery must abandon the writer and return.
	blocked := newBlockingBackend()
	ctx, cancel := context.WithCancel(context.Background())
	blockDone := make(chan error, 1)
	go func() { blockDone <- Run(ctx, &testModel{}, WithBackend(blocked)) }()
	select {
	case <-blocked.started:
	case <-time.After(hangTimeout):
		t.Fatal("present did not start")
	}
	cancel()
	select {
	case err := <-blockDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled run error = %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("Run remained blocked in Present")
	}

	// Lifecycle 4: model panic — the panic-recovery path must still join
	// its goroutines before returning.
	_ = Run(context.Background(), panicModel{}, WithBackend(newFakeBackend(10, 3)))

	// Lifecycle 5: the plain non-interactive path renders one snapshot
	// and returns without an interactive loop at all.
	plainDone := make(chan error, 1)
	go func() {
		features := capability.Portable()
		settings := defaultConfig()
		settings.backend = newFakeBackend(12, 4)
		ctx, cancel := context.WithCancelCause(context.Background())
		plainDone <- run(ctx, cancel, &testModel{animate: 10}, settings, features, settings.backend.Close)
	}()
	select {
	case err := <-plainDone:
		if err != nil {
			t.Fatalf("plain run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("plain renderer did not return after its snapshot")
	}

	// Poll until the count returns to the baseline: transient background
	// work (GC workers, timer sweeps) may exceed it briefly, so a single
	// sample would report false leaks.
	deadline := time.Now().Add(hangTimeout)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			dump := make([]byte, 1<<16)
			dump = dump[:runtime.Stack(dump, true)]
			t.Fatalf("goroutines after completed runs = %d, baseline = %d; a framework goroutine outlived Run\n%s",
				runtime.NumGoroutine(), baseline, dump)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
