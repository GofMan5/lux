package lux

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GofMan5/lux/event"
)

// The intercept tests pin the run loop's claim on Ctrl+Z at the only
// boundary that matters, the events arm, using a backend that can suspend
// and blocks inside the handshake so the exact interleaving is observable:
// the model must pause before the terminal hands off, resume after the
// group continues, and never see the Ctrl+Z key itself — while backends
// that cannot suspend, and applications that opt out, keep the key as an
// ordinary event, unchanged.

// orderLog records the sequence of observable steps across the model and
// the backend handshake. The handshake runs on the run loop's goroutine
// and the model's Update is called from it, so the order here is exactly
// the order the run loop produced.
type orderLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *orderLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

func (l *orderLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

func (l *orderLog) indexOf(entry string) int {
	for i, candidate := range l.snapshot() {
		if candidate == entry {
			return i
		}
	}
	return -1
}

// interceptModel logs every update kind and can quit on a chosen kind.
type interceptModel struct {
	log    *orderLog
	quitOn event.Kind
}

func (m *interceptModel) Update(next event.Event) Command {
	m.log.add(next.Kind.String())
	if next.Kind == m.quitOn {
		return Quit()
	}
	return nil
}

func (m *interceptModel) View(*Frame) {}

// suspendBackend embeds the fake backend and adds the suspend transport:
// a controllable CanSuspend, a handshake that blocks until the test
// releases it, and a refusal mode for the raced-promise path.
type suspendBackend struct {
	*fakeBackend
	log     *orderLog
	can     bool
	refuse  bool
	fail    error
	release chan struct{}
}

func newSuspendBackend(width, height int, log *orderLog) *suspendBackend {
	return &suspendBackend{
		fakeBackend: newFakeBackend(width, height),
		log:         log,
		can:         true,
		release:     make(chan struct{}),
	}
}

func (b *suspendBackend) CanSuspend() bool { return b.can }

func (b *suspendBackend) Suspend(ctx context.Context) (bool, error) {
	b.log.add("backend-suspend")
	select {
	case <-b.release:
	case <-ctx.Done():
		return true, ctx.Err()
	}
	switch {
	case b.fail != nil:
		return true, b.fail
	case b.refuse:
		return false, nil
	default:
		return true, nil
	}
}

// startRun launches Run in the background; awaitRun bounds it so a wedged
// handshake fails the test instead of hanging it.
func startRun(model Model, backend Backend, opts ...Option) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, append([]Option{WithBackend(backend)}, opts...)...) }()
	return done
}

func awaitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(hangTimeout):
		t.Fatal("lux: Run never returned; suspend handshake wedged")
		return nil
	}
}

func waitForLog(t *testing.T, log *orderLog, entry string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if log.indexOf(entry) >= 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("lux: log never recorded %q; entries: %v", entry, log.snapshot())
}

// TestSuspendInterceptClaimsCtrlZ covers both spellings of the press —
// kitty and the Windows console deliver lowercase 'z', a legacy terminal
// delivers the raw 0x1A byte the parser decodes as uppercase 'Z' — and
// pins the ordering contract for each: Suspend reaches the model before
// the handshake runs, Resume after it completes, and the key event itself
// is consumed by the runtime, never delivered.
func TestSuspendInterceptClaimsCtrlZ(t *testing.T) {
	for _, press := range []struct {
		name string
		key  event.Event
	}{
		{"kitty lowercase", event.RuneKey('z', event.ModCtrl)},
		{"legacy uppercase", event.RuneKey('Z', event.ModCtrl)},
	} {
		t.Run(press.name, func(t *testing.T) {
			log := &orderLog{}
			backend := newSuspendBackend(80, 24, log)
			model := &interceptModel{log: log, quitOn: event.Resume}

			done := startRun(model, backend)
			backend.events <- press.key

			waitForLog(t, log, "backend-suspend")
			if keyAt, suspendAt := log.indexOf("key"), log.indexOf("suspend"); keyAt >= 0 && keyAt < suspendAt {
				t.Fatalf("lux: model saw the key before pausing; entries: %v", log.snapshot())
			}
			close(backend.release)

			if err := awaitRun(t, done); err != nil {
				t.Fatalf("lux: Run: %v", err)
			}
			entries := log.snapshot()
			if got := log.indexOf("key"); got >= 0 {
				t.Fatalf("lux: Ctrl+Z was delivered to the model; entries: %v", entries)
			}
			suspendAt, handshakeAt, resumeAt := log.indexOf("suspend"), log.indexOf("backend-suspend"), log.indexOf("resume")
			if suspendAt < 0 || handshakeAt < 0 || resumeAt < 0 || suspendAt > handshakeAt || handshakeAt > resumeAt {
				t.Fatalf("lux: want suspend -> backend-suspend -> resume; entries: %v", entries)
			}
		})
	}
}

// TestSuspendInterceptOptOutRestoresTheKey pins WithSuspend(false): the
// key passes to the model untouched and the handshake never runs, which is
// the opt-out applications that bind Ctrl+Z themselves need.
func TestSuspendInterceptOptOutRestoresTheKey(t *testing.T) {
	log := &orderLog{}
	backend := newSuspendBackend(80, 24, log)
	model := &interceptModel{log: log, quitOn: event.Key}

	done := startRun(model, backend, WithSuspend(false))
	backend.events <- event.RuneKey('z', event.ModCtrl)

	if err := awaitRun(t, done); err != nil {
		t.Fatalf("lux: Run: %v", err)
	}
	if log.indexOf("backend-suspend") >= 0 {
		t.Fatalf("lux: handshake ran despite WithSuspend(false); entries: %v", log.snapshot())
	}
	if log.indexOf("key") < 0 {
		t.Fatalf("lux: model never saw the key; entries: %v", log.snapshot())
	}
}

// TestSuspendInterceptCannotSuspendPassesKey pins the pre-flight promise:
// a backend whose CanSuspend is false — a plain stream, a pipe, a test
// fake, a Windows console — keeps Ctrl+Z an ordinary key, with no phantom
// Suspend or Resume events bookending it.
func TestSuspendInterceptCannotSuspendPassesKey(t *testing.T) {
	log := &orderLog{}
	backend := newSuspendBackend(80, 24, log)
	backend.can = false
	model := &interceptModel{log: log, quitOn: event.Key}

	done := startRun(model, backend)
	backend.events <- event.RuneKey('z', event.ModCtrl)

	if err := awaitRun(t, done); err != nil {
		t.Fatalf("lux: Run: %v", err)
	}
	for _, phantom := range []string{"backend-suspend", "suspend", "resume"} {
		if log.indexOf(phantom) >= 0 {
			t.Fatalf("lux: %s observed on a backend that cannot suspend; entries: %v", phantom, log.snapshot())
		}
	}
	if log.indexOf("key") < 0 {
		t.Fatalf("lux: model never saw the key; entries: %v", log.snapshot())
	}
}

// TestSuspendInterceptRacedRefusalFallsThrough pins the raced-promise
// path: CanSuspend said yes, then the handshake could not start — a
// backend closing underneath the press. The model has already paused, so
// it sees Suspend, then the key falls through as input; no Resume event
// is invented for a handshake that never happened.
func TestSuspendInterceptRacedRefusalFallsThrough(t *testing.T) {
	log := &orderLog{}
	backend := newSuspendBackend(80, 24, log)
	backend.refuse = true
	model := &interceptModel{log: log, quitOn: event.Key}

	done := startRun(model, backend)
	backend.events <- event.RuneKey('z', event.ModCtrl)

	// The refusal is raced: the handshake starts, cannot hold the
	// terminal, and reports that it never did.
	waitForLog(t, log, "backend-suspend")
	close(backend.release)

	if err := awaitRun(t, done); err != nil {
		t.Fatalf("lux: Run: %v", err)
	}
	suspendAt, keyAt := log.indexOf("suspend"), log.indexOf("key")
	if suspendAt < 0 || keyAt < 0 || suspendAt > keyAt {
		t.Fatalf("lux: want Suspend then the key on raced refusal; entries: %v", log.snapshot())
	}
	if log.indexOf("resume") >= 0 {
		t.Fatalf("lux: Resume invented for a handshake that never started; entries: %v", log.snapshot())
	}
}

// TestSuspendInterceptHandshakeErrorEndsRun pins the failure path: an
// error from the handshake — a hangup, a failed re-entry — ends the run
// with that error after the model paused, because the terminal state only
// the deferred Close can make safe is exactly the state the error left.
func TestSuspendInterceptHandshakeErrorEndsRun(t *testing.T) {
	log := &orderLog{}
	backend := newSuspendBackend(80, 24, log)
	backend.fail = errors.New("handshake exploded")
	model := &interceptModel{log: log, quitOn: event.Resume}

	done := startRun(model, backend)
	backend.events <- event.RuneKey('z', event.ModCtrl)

	waitForLog(t, log, "backend-suspend")
	close(backend.release)

	if err := awaitRun(t, done); err == nil || err.Error() != "handshake exploded" {
		t.Fatalf("lux: Run error = %v, want the handshake error", err)
	}
	if log.indexOf("suspend") < 0 || log.indexOf("resume") >= 0 {
		t.Fatalf("lux: want Suspend delivered and no Resume on failure; entries: %v", log.snapshot())
	}
}
