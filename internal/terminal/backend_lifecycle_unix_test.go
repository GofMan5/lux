//go:build !windows

package terminal

import (
	"bytes"
	"os"
	"syscall"
	"testing"
)

// TestTerminationSignalRestoresAndReports pins the trap's two halves in
// one place: the terminal is restored (leave sequences exactly once, the
// suspend latch set as Close's receipt that this restore already ran),
// and the error event carries the signal's sentinel so the application
// can map it to a conventional exit status. The Close call afterwards is
// the deferred closeBackend Run unwinds through, and the re-count after
// it is the exactly-once guarantee the e2e lanes assert on a real pty.
func TestTerminationSignalRestoresAndReports(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		want error
	}{
		{"SIGINT", syscall.SIGINT, ErrInterrupt},
		{"SIGTERM", syscall.SIGTERM, ErrTerminated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubMakeRaw(t)
			backend, output := newSuspendBackend(t)
			backend.events = makeLifecycleChan()
			signals := make(chan os.Signal, 1)
			returned, cancel := dispatchLifecycleFor(backend, signals)
			defer cancel()

			signals <- tc.sig
			awaitReturn(t, returned)

			if !backend.suspended {
				t.Fatalf("%s did not restore the terminal: suspend latch not set", tc.name)
			}
			written := writtenBytes(t, output)
			if !bytes.HasSuffix(written, []byte(leaveTerminal)) {
				t.Fatalf("%s did not write the leave sequences: %q", tc.name, written)
			}
			if bytes.Count(written, []byte(leaveTerminal)) != 1 {
				t.Fatalf("%s wrote the leave sequences more than once: %q", tc.name, written)
			}
			assertLifecycleEvent(t, backend, tc.want)

			// The deferred closeBackend on Run's error unwind runs
			// exactly this call; it must not double the leave.
			if err := backend.Close(); err != nil {
				t.Fatalf("close after %s restore: %v", tc.name, err)
			}
			written = writtenBytes(t, output)
			if bytes.Count(written, []byte(leaveTerminal)) != 1 {
				t.Fatalf("Close wrote a second leave on top of the %s restore: %q", tc.name, written)
			}
		})
	}
}

// TestHangupReportsLossWithoutRestore pins the asymmetry that is the
// whole point of keeping SIGHUP separate from the termination pair: the
// terminal is gone, so restore is impossible — no bytes, no latch — and
// only the loss is reported.
func TestHangupReportsLossWithoutRestore(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	backend.events = makeLifecycleChan()
	signals := make(chan os.Signal, 1)
	returned, cancel := dispatchLifecycleFor(backend, signals)
	defer cancel()

	signals <- syscall.SIGHUP
	awaitReturn(t, returned)

	if backend.suspended {
		t.Fatal("hangup must not latch a suspension: there is no terminal to restore")
	}
	if got := writtenBytes(t, output); len(got) != 0 {
		t.Fatalf("hangup wrote %d bytes, want none: the terminal is gone", len(got))
	}
	assertLifecycleEvent(t, backend, errHangup)
}

// TestSignalOnClosedBackendReportsNothing pins the guard the race with
// Close depends on: once the backend is closed, any signal that still
// arrives is swallowed without an event and without a restore attempt,
// because the exit path that owns the cleanup is already running.
func TestSignalOnClosedBackendReportsNothing(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	backend.events = makeLifecycleChan()
	backend.opened = false
	signals := make(chan os.Signal, 1)
	returned, cancel := dispatchLifecycleFor(backend, signals)
	defer cancel()

	signals <- syscall.SIGTERM
	awaitReturn(t, returned)

	if backend.suspended {
		t.Fatal("signal on a closed backend must not run the restore")
	}
	if got := writtenBytes(t, output); len(got) != 0 {
		t.Fatalf("signal on a closed backend wrote %d bytes, want none", len(got))
	}
	select {
	case next := <-backend.events:
		t.Fatalf("signal on a closed backend delivered an event: %+v", next)
	default:
	}
}

// TestDispatcherExitsOnContextCancel pins the quiet shutdown path: Close
// cancels the read context, and the dispatcher must leave without an
// event so an ordinary quit is never mistaken for a terminal loss.
func TestDispatcherExitsOnContextCancel(t *testing.T) {
	stubMakeRaw(t)
	backend, _ := newSuspendBackend(t)
	backend.events = makeLifecycleChan()
	signals := make(chan os.Signal, 1)
	returned, cancel := dispatchLifecycleFor(backend, signals)
	defer cancel()

	cancel()
	awaitReturn(t, returned)

	select {
	case next := <-backend.events:
		t.Fatalf("context cancel delivered an event: %+v", next)
	default:
	}
}
