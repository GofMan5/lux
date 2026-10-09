//go:build !windows

package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The handshake tests here run the real Suspend flow — leave sequences,
// stop, wait, re-enter — with the one untestable act, stopping the process
// group, swapped out: raiseStop is indirected precisely so these tests can
// pin the ordering around it while the PTY verification lane pins the real
// kernel stop. SIGCONT and SIGHUP are raised the real way, to this own
// process, through the kernel's signal machinery.

// swapRaiseStop replaces the group stop for one test.
func swapRaiseStop(t *testing.T, stop func() error) {
	t.Helper()
	previous := raiseStop
	raiseStop = stop
	t.Cleanup(func() { raiseStop = previous })
}

// suspendResult carries one Suspend outcome across a goroutine boundary.
type suspendResult struct {
	suspended bool
	err       error
}

// TestSuspendHandshakeStopsAndResumes pins the full handshake as one
// ordered story: the leave sequences are written before the group stop is
// raised, the wait ends on the continue signal, and the enter sequences
// plus the cleared latch land after it — the same shape the PTY lane
// verifies against a real shell.
func TestSuspendHandshakeStopsAndResumes(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)

	stopped := make(chan struct{})
	var once sync.Once
	swapRaiseStop(t, func() error {
		once.Do(func() { close(stopped) })
		return nil
	})

	done := make(chan suspendResult, 1)
	go func() {
		suspended, err := backend.Suspend(context.Background())
		done <- suspendResult{suspended: suspended, err: err}
	}()

	select {
	case <-stopped:
	case <-time.After(hangTimeout):
		t.Fatal("handshake never raised the group stop")
	}
	// The stop is observed; the shell continues the group.
	if err := syscall.Kill(os.Getpid(), syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || !result.suspended {
			t.Fatalf("Suspend = (%v, %v); want (true, nil)", result.suspended, result.err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("handshake never woke on the continue")
	}
	if backend.suspended {
		t.Fatal("handshake left the suspended latch set")
	}
	written := writtenBytes(t, output)
	if !bytes.HasSuffix(written, []byte(enterTerminal)) {
		t.Fatalf("handshake did not re-enter: %q", written)
	}
	if bytes.Count(written, []byte(leaveTerminal)) != 1 {
		t.Fatalf("handshake wrote leave %d times", bytes.Count(written, []byte(leaveTerminal)))
	}
	if bytes.Index(written, []byte(leaveTerminal)) > bytes.Index(written, []byte(enterTerminal)) {
		t.Fatal("leave sequences landed after the enter sequences")
	}
}

// TestSuspendHandshakeRefusesNonSuspendable pins the refusal half of the
// returned bool at the transport boundary the run loop calls: a plain
// stream, a closed backend, and an already-suspended one all refuse
// before writing a byte, because the caller must be able to deliver the
// suspend key as an ordinary event instead — the internal guards are the
// same refusal, pinned separately by the suspendTerminal tests.
func TestSuspendHandshakeRefusesNonSuspendable(t *testing.T) {
	stubMakeRaw(t)
	plain, plainOutput := newSuspendBackend(t)
	plain.plain = true
	suspended, err := plain.Suspend(context.Background())
	if suspended || err != nil || plain.suspended {
		t.Fatalf("plain Suspend = (%v, %v, latch %v); want refusal", suspended, err, plain.suspended)
	}
	if got := len(writtenBytes(t, plainOutput)); got != 0 {
		t.Fatalf("refused plain suspend wrote %d bytes", got)
	}

	closed, closedOutput := newSuspendBackend(t)
	closed.opened = false
	suspended, err = closed.Suspend(context.Background())
	if suspended || err != nil || closed.suspended {
		t.Fatalf("closed Suspend = (%v, %v, latch %v); want refusal", suspended, err, closed.suspended)
	}
	if got := len(writtenBytes(t, closedOutput)); got != 0 {
		t.Fatalf("refused closed suspend wrote %d bytes", got)
	}

	latched, latchedOutput := newSuspendBackend(t)
	latched.suspendTerminal()
	suspended, err = latched.Suspend(context.Background())
	if suspended || err != nil {
		t.Fatalf("double Suspend = (%v, %v); want refusal", suspended, err)
	}
	if bytes.Count(writtenBytes(t, latchedOutput), []byte(leaveTerminal)) != 1 {
		t.Fatal("refused double suspend wrote leave bytes")
	}
}

// TestSuspendHandshakeReportsHangup pins the stop window's bad ending: a
// hangup during the wait is the same terminal-loss error the lifecycle
// watcher reports, routed as the handshake's error so the run loop exits
// instead of inventing a resume no terminal exists for.
func TestSuspendHandshakeReportsHangup(t *testing.T) {
	stubMakeRaw(t)
	backend, _ := newSuspendBackend(t)

	stopped := make(chan struct{})
	swapRaiseStop(t, func() error {
		close(stopped)
		return nil
	})

	done := make(chan suspendResult, 1)
	go func() {
		suspended, err := backend.Suspend(context.Background())
		done <- suspendResult{suspended: suspended, err: err}
	}()
	<-stopped
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if !result.suspended {
			t.Error("hangup path must report the handshake had started")
		}
		if !errors.Is(result.err, errHangup) {
			t.Fatalf("hangup error = %v; want errHangup", result.err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("hangup never ended the stop window")
	}
}

// TestSuspendHandshakeHonorsContextCancel pins cancellation: a stop window
// outlived by its context ends with the context's error, the latch stays
// retryable, and the deferred Close is the cleanup of record.
func TestSuspendHandshakeHonorsContextCancel(t *testing.T) {
	stubMakeRaw(t)
	backend, _ := newSuspendBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{})
	swapRaiseStop(t, func() error {
		// Return like a real group stop would: the window that follows is
		// held by the wait for a continue that this test never sends.
		close(stopped)
		return nil
	})

	done := make(chan suspendResult, 1)
	go func() {
		suspended, err := backend.Suspend(ctx)
		done <- suspendResult{suspended: suspended, err: err}
	}()
	<-stopped
	cancel()
	select {
	case result := <-done:
		if !result.suspended {
			t.Error("cancel path must report the handshake had started")
		}
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("cancel error = %v; want context.Canceled", result.err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("cancellation never ended the stop window")
	}
	if !backend.suspended {
		t.Fatal("a canceled handshake must keep the latch retryable for Close")
	}
}

// TestCanSuspendKeepsThePromise pins the pre-flight: the promise holds
// exactly for an open interactive backend that is not already suspended,
// because the run loop turns this answer into the decision between the
// suspend handshake and an ordinary key event.
func TestCanSuspendKeepsThePromise(t *testing.T) {
	backend, _ := newSuspendBackend(t)
	if !backend.CanSuspend() {
		t.Fatal("open interactive backend must promise suspension")
	}
	backend.plain = true
	if backend.CanSuspend() {
		t.Fatal("plain backend must refuse suspension")
	}
	backend.plain = false
	backend.opened = false
	if backend.CanSuspend() {
		t.Fatal("closed backend must refuse suspension")
	}
	backend.opened = true
	backend.suspendTerminal()
	if backend.CanSuspend() {
		t.Fatal("suspended backend must refuse suspension")
	}
}
