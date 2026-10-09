package terminal

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"golang.org/x/term"
)

// The suspend/resume unit tests run on plain files: a test process's stdin
// and stdout are not terminals, and the byte stream on a file is the same
// stream a terminal would receive. The one thing files cannot provide is the
// raw-mode dance, so makeRaw is stubbed here and the real MakeRaw/Restore
// round trip is pinned by the PTY verification lane.

// stubMakeRaw swaps the raw-mode entry for the duration of one test. The
// suspend path never calls it; the resume path calls it on b.input, whose
// file descriptor these stubs ignore.
func stubMakeRaw(t *testing.T) {
	t.Helper()
	previous := makeRaw
	makeRaw = func(int) (*term.State, error) { return nil, nil }
	t.Cleanup(func() { makeRaw = previous })
}

// newSuspendBackend builds the minimal interactive backend these tests
// need: open, non-plain, a single temp file standing in for all three
// handles. The input handle matters only to resumeTerminal, whose MakeRaw
// call is stubbed — the fd itself is never touched.
func newSuspendBackend(t *testing.T) (*Backend, *os.File) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	backend := &Backend{opened: true, input: output, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
	return backend, output
}

func writtenBytes(t *testing.T, output *os.File) []byte {
	t.Helper()
	written, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func testFrame(texts ...string) *render.Buffer {
	frame := render.NewBuffer(len([]rune(texts[0])), len(texts))
	canvas := render.NewCanvas(frame)
	for row, text := range texts {
		canvas.Text(0, row, text, render.Style{})
	}
	return frame
}

// TestSuspendWritesLeaveOnceAndRefusesFrames pins the terminal half of the
// stop: the leave sequences Close would write, exactly once, and the frame
// gate that keeps a stopped process from painting onto the shell's screen.
func TestSuspendWritesLeaveOnceAndRefusesFrames(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused on an open interactive backend")
	}
	if !backend.suspended {
		t.Fatal("suspend did not latch the suspended state")
	}
	written := writtenBytes(t, output)
	if !bytes.HasSuffix(written, []byte(leaveTerminal)) {
		t.Fatalf("suspend did not write the leave sequences: %q", written)
	}
	if bytes.Count(written, []byte(leaveTerminal)) != 1 {
		t.Fatalf("suspend wrote the leave sequences more than once: %q", written)
	}
	if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
		t.Fatalf("a frame during suspension must be a clean skip, not an error: %v", err)
	}
	if got := len(writtenBytes(t, output)); got != len(leaveTerminal) {
		t.Fatalf("Present during suspension wrote %d extra bytes", got-len(leaveTerminal))
	}
	if backend.previous != nil {
		t.Fatal("a skipped frame must not commit a diff baseline")
	}
	// A second stop while the first still holds is refused by the latch:
	// the terminal is already cooked, and a second leave would pop the
	// kitty and title stacks twice.
	if backend.suspendTerminal() {
		t.Fatal("double suspend must be refused")
	}
	if bytes.Count(writtenBytes(t, output), []byte(leaveTerminal)) != 1 {
		t.Fatal("refused second suspend still wrote leave bytes")
	}
}

// TestResumeRewritesEnterAndClearsTheLatch pins the continue half: the same
// enter sequences Open would write, the latch cleared, and the baseline
// dropped so the next frame is a full repaint.
func TestResumeRewritesEnterAndClearsTheLatch(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if err := backend.resumeTerminal(); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if backend.suspended {
		t.Fatal("resume did not clear the suspended latch")
	}
	if backend.previous != nil {
		t.Fatal("resume must drop the diff baseline")
	}
	written := writtenBytes(t, output)
	if !bytes.HasSuffix(written, []byte(enterTerminal)) {
		t.Fatalf("resume did not write the enter sequences: %q", written)
	}
	if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
		t.Fatal(err)
	}
	if backend.previous == nil {
		t.Fatal("the first frame after resume must commit a fresh baseline")
	}
	if !bytes.HasSuffix(writtenBytes(t, output), []byte("ok")) {
		t.Fatal("the post-resume frame was not written")
	}
}

// TestResumeForcesFullRepaintOfUnchangedRows proves the repaint contract:
// a row that did not change between the pre-suspend frame and the
// post-resume frame is written again, because the baseline was dropped on
// resume — the shell had the terminal between the two frames, so the diff
// would be a lie.
func TestResumeForcesFullRepaintOfUnchangedRows(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	if err := backend.Present(context.Background(), testFrame("AA", "BB")); err != nil {
		t.Fatal(err)
	}
	if err := backend.Present(context.Background(), testFrame("AA", "CC")); err != nil {
		t.Fatal(err)
	}
	// The second frame is a plain diff: row 0 is unchanged and must not be
	// rewritten before the suspension.
	if got := bytes.Count(writtenBytes(t, output), []byte("AA")); got != 1 {
		t.Fatalf("row 0 was rewritten %d times before suspension, want 1", got)
	}
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if err := backend.resumeTerminal(); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if err := backend.Present(context.Background(), testFrame("AA", "DD")); err != nil {
		t.Fatal(err)
	}
	written := writtenBytes(t, output)
	if got := bytes.Count(written, []byte("AA")); got != 2 {
		t.Fatalf("the unchanged row appears %d times after resume, want 2 (full repaint): %q", got, written)
	}
	if !bytes.Contains(written, []byte("DD")) {
		t.Fatalf("the changed row was not written after resume: %q", written)
	}
}

// TestSuspendRefusesPlainAndClosedBackends pins the guards: a redirected
// output stream has no terminal state to hand away, and a closed backend
// must not stop a process that is exiting.
func TestSuspendRefusesPlainAndClosedBackends(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	plain := &Backend{opened: true, plain: true, output: output, writer: output}
	if plain.suspendTerminal() {
		t.Fatal("a plain backend must refuse suspension")
	}
	if err := plain.resumeTerminal(); err != nil {
		t.Fatalf("resume on a plain backend must be a no-op, not an error: %v", err)
	}
	closed := &Backend{output: output, writer: output}
	if closed.suspendTerminal() {
		t.Fatal("a closed backend must refuse suspension")
	}
	if err := closed.resumeTerminal(); err != nil {
		t.Fatalf("resume on a closed backend must be a no-op, not an error: %v", err)
	}
	if len(writtenBytes(t, output)) != 0 {
		t.Fatalf("refused suspends wrote bytes: %q", writtenBytes(t, output))
	}
}

// TestResumeWithoutSuspensionIsNoOp pins the SIGCONT branch contract: a
// continue that arrives without a stop through this watcher is not a resume.
func TestResumeWithoutSuspensionIsNoOp(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	if err := backend.resumeTerminal(); err != nil {
		t.Fatalf("resume without suspension must be a no-op, not an error: %v", err)
	}
	if backend.suspended {
		t.Fatal("a no-op resume latched suspension")
	}
	if len(writtenBytes(t, output)) != 0 {
		t.Fatalf("a no-op resume wrote bytes: %q", writtenBytes(t, output))
	}
}

// TestResumeWithoutInputKeepsSuspensionLatched pins the error contract when
// the terminal input handle is gone: the latch stays set so Present keeps
// refusing frames on a terminal whose input mode is unknown.
func TestResumeWithoutInputKeepsSuspensionLatched(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
	if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
		t.Fatal(err)
	}
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if err := backend.resumeTerminal(); err == nil {
		t.Fatal("resume without terminal input must fail")
	}
	if !backend.suspended {
		t.Fatal("a failed resume must keep the suspension latched")
	}
	if backend.previous != nil {
		t.Fatal("a failed resume must still drop the diff baseline")
	}
}

// TestFailedRawEntryKeepsSuspensionLatchedAndRecovers pins the MakeRaw
// failure path: the latch stays set and Present keeps refusing until a later
// resume succeeds and writes the enter sequences.
func TestFailedRawEntryKeepsSuspensionLatchedAndRecovers(t *testing.T) {
	previous := makeRaw
	makeRaw = func(int) (*term.State, error) { return nil, os.ErrInvalid }
	t.Cleanup(func() { makeRaw = previous })
	backend, output := newSuspendBackend(t)
	if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
		t.Fatal(err)
	}
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if err := backend.resumeTerminal(); err == nil {
		t.Fatal("a failing MakeRaw must fail the resume")
	}
	if !backend.suspended {
		t.Fatal("the latch must survive a MakeRaw failure")
	}
	if err := backend.Present(context.Background(), testFrame("no")); err != nil {
		t.Fatalf("Present while still suspended must be a clean skip: %v", err)
	}
	if bytes.Contains(writtenBytes(t, output), []byte("no")) {
		t.Fatal("Present wrote while the failed resume kept the latch set")
	}
	// A later resume — the terminal came back — recovers: latch cleared,
	// enter written, next frame fully repainted.
	makeRaw = func(int) (*term.State, error) { return nil, nil }
	if err := backend.resumeTerminal(); err != nil {
		t.Fatalf("the retrying resume failed: %v", err)
	}
	if backend.suspended {
		t.Fatal("the retrying resume did not clear the latch")
	}
	if !bytes.HasSuffix(writtenBytes(t, output), []byte(enterTerminal)) {
		t.Fatalf("the retrying resume did not write enter: %q", writtenBytes(t, output))
	}
	if err := backend.Present(context.Background(), testFrame("yes")); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(writtenBytes(t, output), []byte("yes")) {
		t.Fatal("the frame after the retrying resume was not written")
	}
}

// TestCloseDuringSuspensionClearsTheLatch pins the Close interplay: Close
// writes its own leave — idempotent on the already-cooked terminal, the
// accepted double-pop — and clears the latch so a re-opened backend starts
// from a clean state.
func TestCloseDuringSuspensionClearsTheLatch(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
		t.Fatal(err)
	}
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close during suspension failed: %v", err)
	}
	if backend.suspended {
		t.Fatal("Close must clear the suspension latch")
	}
	written := writtenBytes(t, output)
	if got := bytes.Count(written, []byte(leaveTerminal)); got != 2 {
		t.Fatalf("leave written %d times (suspend + close), want 2: %q", got, written)
	}
	if !bytes.HasSuffix(written, []byte(leaveTerminal)) {
		t.Fatalf("Close's leave must be the last bytes: %q", written)
	}
	if err := backend.Present(context.Background(), testFrame("late")); err == nil {
		t.Fatal("Present after Close must fail")
	}
}

// TestSuspendResumeWithKittyAndTitleRoundTripsTheStacks pins the latch
// composition: the leave and enter come from the same sequences() table
// Open and Close use, so the kitty and title stacks pop and push in the same
// order a fresh Open would produce.
func TestSuspendResumeWithKittyAndTitleRoundTripsTheStacks(t *testing.T) {
	stubMakeRaw(t)
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	backend := &Backend{opened: true, input: output, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}, kitty: true, title: "App"}
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	if !bytes.HasSuffix(writtenBytes(t, output), []byte(kittyLeave+leaveTerminal+titlePop)) {
		t.Fatalf("suspend did not pop the stacks in order: %q", writtenBytes(t, output))
	}
	if err := backend.resumeTerminal(); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	written := writtenBytes(t, output)
	if got := bytes.Count(written, []byte(leaveTerminal)); got != 1 {
		t.Fatalf("leave written %d times, want 1: %q", got, written)
	}
	if !bytes.HasSuffix(written, []byte(titlePush+setWindowTitle("App")+enterTerminal+kittyEnter)) {
		t.Fatalf("resume did not push the stacks in order: %q", written)
	}
}

// TestDeliverLifecycleNeverBlocks pins the watcher contract: the signal
// watcher must never wedge on a busy application — the notice is dropped,
// not waited on, because the terminal transitions have already happened and
// the resume repaint does not depend on the event being seen.
func TestDeliverLifecycleNeverBlocks(t *testing.T) {
	backend := &Backend{opened: true, events: make(chan event.Event, 1)}
	done := make(chan struct{})
	go func() {
		backend.deliverLifecycle(event.Event{Kind: event.Suspend})
		backend.deliverLifecycle(event.Event{Kind: event.Resume})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(hangTimeout):
		t.Fatal("deliverLifecycle blocked on a full event queue")
	}
	if got := (<-backend.events).Kind; got != event.Suspend {
		t.Fatalf("the queued notice is %v, want Suspend", got)
	}
}
