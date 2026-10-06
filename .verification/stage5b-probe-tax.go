// Scratch harness for the stage-5b negotiation startup-tax numbers in
// verification-stage5b.txt. NOT part of the shipped package: copy this file
// into internal/terminal as zz_probe_tax_test.go to reproduce the
// measurements (go test ./internal/terminal -run 'TestProbeTax' -count=1 -v).
//
// What is measured, and why it is honest:
//   - The silent-terminal worst case is bounded by the probe deadline on
//     every platform. On Unix an anonymous pipe polls clean; on Windows the
//     readInput wait (WaitForSingleObject on the pipe handle) returns
//     WAIT_TIMEOUT rather than a hard error — verified empirically on this
//     machine: a 200 ms wait on an empty pipe returned 0x102 in ~210 ms, and
//     the same wait with 2 bytes already buffered ALSO returned 0x102 (pipe
//     data never signals WAIT_OBJECT_0). So on Windows a silent pipe pays
//     exactly one deadline-sized wait and the loop exits through the
//     remaining<=0 check, which is what the lower bound below pins.
//   - The answering-terminal fast path is measurable only on Unix: a
//     goroutine plays DA1 plus both DECRPM reports into the pipe, and
//     negotiate returns as soon as all three are parsed (the early exit
//     once answered && scroll != probeUnknown && sync != probeUnknown),
//     far below the deadline. On Windows the same test cannot deliver
//     pipe bytes to readInput at all, so it skips there; the fast path on
//     Windows real terminals follows from the same early exit and is argued
//     in verification-stage5b.txt, not measured.
//
// Exact report bytes (kept byte-identical to negotiate_test.go rows):
//   DA1:      \x1b[?62;1;2c      body "62;1;2" passes isAttributeBody
//   mode 6:   \x1b[?6;2$y        DECRPM set — scroll regions supported
//   mode 2026: \x1b[?2026;3$y    DECRPM permanently set — sync supported

package terminal

import (
	"context"
	"io"
	"os"
	"runtime"
	"testing"
	"time"
)

// probeTaxTimeout is the budget the tests hand to negotiate directly; 150 ms
// mirrors defaultProbeTimeout.
const probeTaxTimeout = 150 * time.Millisecond

// TestProbeTaxSilentTerminalBounded measures the worst case: a terminal that
// never answers. The writer end of the pipe stays open (no EOF), so the only
// exits from the negotiation loop are the deadline or a byte delivery that
// never comes. The lower bound proves the probe really waits for its budget
// (no instant hard-error give-up, no busy-spin), and the upper bound proves
// the wait is bounded (no unbounded hang class). The generous upper slack
// absorbs CI scheduler noise; these are scratch tests, not CI gates.
func TestProbeTaxSilentTerminalBounded(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()

	start := time.Now()
	reply, leftover := negotiate(context.Background(), input, io.Discard, probeTaxTimeout)
	wall := time.Since(start)

	if wall < 140*time.Millisecond {
		t.Fatalf("silent-terminal probe returned in %v, want at least ~%v (budget must be honored)", wall, probeTaxTimeout)
	}
	if wall > 2*time.Second {
		t.Fatalf("silent-terminal probe took %v, want at most 2s (deadline-bounded wait)", wall)
	}
	if reply.answered {
		t.Fatalf("silent pipe reported answered=true: %+v", reply)
	}
	if len(leftover) != 0 {
		t.Fatalf("silent pipe produced leftover bytes: %q", leftover)
	}
}

// TestProbeTaxAnsweringTerminalFast measures the answering case: DA1 plus
// both DECRPM reports arrive ~5ms in, and negotiate must return via the
// early exit, well before the deadline. Skipped on Windows because
// anonymous-pipe data never signals the console-input wait there (see the
// file header); the Windows answering path rests on the same early exit.
func TestProbeTaxAnsweringTerminalFast(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("anonymous pipe data never reaches readInput on Windows; fast path argued in verification-stage5b.txt")
	}
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()

	go func() {
		time.Sleep(5 * time.Millisecond)
		_, _ = writer.WriteString("\x1b[?62;1;2c\x1b[?6;2$y\x1b[?2026;3$y")
	}()

	start := time.Now()
	reply, leftover := negotiate(context.Background(), input, io.Discard, probeTaxTimeout)
	wall := time.Since(start)

	if !reply.answered || reply.scroll != probeSupported || reply.sync != probeSupported {
		t.Fatalf("answering terminal misparsed: %+v", reply)
	}
	if len(leftover) != 0 {
		t.Fatalf("answering terminal left %d unconsumed bytes: %q", len(leftover), leftover)
	}
	if wall >= 120*time.Millisecond {
		t.Fatalf("answering terminal took %v, want well under the %v deadline (early exit after DA1 + both reports)", wall, probeTaxTimeout)
	}
}
