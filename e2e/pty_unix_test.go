//go:build linux || darwin

package e2e

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// The wire bytes asserted here are standards, not internal constants: the
// alternate screen (DECSET 1049), the cursor visibility (DECSET 25), and
// the XTWINOPS title stack (CSI 22/23;0 t). Asserting them on a real pty
// proves what a terminal actually receives, which is the only claim the
// compatibility matrix is allowed to rest on.
const (
	enterAltScreen = "\x1b[?1049h"
	leaveAltScreen = "\x1b[?1049l"
	showCursor     = "\x1b[?25h"
	titleSet       = "\x1b]0;lux-e2e\x07"
	titlePop       = "\x1b[23;0t"
)

// Two lanes run the same child against the same kind of pty. The direct
// lane spawns the child as the session leader — a terminal to itself,
// which is the right shape for ordinary sessions: no shell, no job
// control, keystrokes and resizes only. The shell lane spawns a real sh
// as the session leader and runs the child as its foreground job, because
// the job-control claims cannot be observed any other way: a process
// group whose session has no other members is orphaned, and the kernel
// discards stop signals sent to an orphaned group — a direct-lane child
// can never actually stop, so the stop is proven here by the only witness
// that can see it, the shell, whose "Stopped" line is the kernel's word
// that the group really stopped.
const (
	directLane = "direct"
	shellLane  = "shell"
)

var childBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lux-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: temp dir: %v\n", err)
		os.Exit(1)
	}
	childBinary = filepath.Join(dir, "childapp")
	buildArgs := []string{"build"}
	if childRaceFlag != "" {
		buildArgs = append(buildArgs, childRaceFlag)
	}
	buildArgs = append(buildArgs, "-o", childBinary, "./childapp")
	build := exec.Command("go", buildArgs...)
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: childapp build failed: %v\n%s\n", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// session is one lane attached to a pty as its controlling terminal, with
// everything the pty writes drained into a buffer the test asserts on.
// The reader goroutine must outlive assertions: a stopped child stops
// writing, and a full pty buffer would block it for reasons that have
// nothing to do with the claims under test.
type session struct {
	t      *testing.T
	lane   string
	master *os.File
	cmd    *exec.Cmd
	pid    int

	mu  sync.Mutex
	out []byte
}

// startSession runs the child directly as the session leader.
func startSession(t *testing.T, extraEnv ...string) *session {
	t.Helper()
	cmd := exec.Command(childBinary)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Env = append(cmd.Env, extraEnv...)
	s := start(t, directLane, cmd)
	t.Cleanup(func() {
		if s.waitExit(2*time.Second, false) {
			s.master.Close()
			return
		}
		s.master.Write([]byte("q"))
		if !s.waitExit(2*time.Second, false) {
			syscall.Kill(s.pid, syscall.SIGKILL)
			s.waitExit(2*time.Second, false)
		}
		s.master.Close()
	})
	return s
}

// startShellSession runs a real interactive sh as the session leader in
// the pty and expects the test to drive the child through it as a
// foreground job. The shell's working directory is the child's directory,
// so "./childapp" is the command; the shell's job control — its own
// process groups, its fg/bg, its waitpid on the stop — is the machinery
// under test, not scaffolding. Cleanup is best effort in both directions:
// a running child gets q, a stopped one gets fg first (the shell refuses
// to exit over stopped jobs), and the shell gets exit; anything still
// wedged is killed by pid, child first so it cannot be reparented away.
func startShellSession(t *testing.T, extraEnv ...string) *session {
	t.Helper()
	cmd := exec.Command("sh", "-i")
	cmd.Dir = filepath.Dir(childBinary)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Env = append(cmd.Env, extraEnv...)
	s := start(t, shellLane, cmd)
	t.Cleanup(func() {
		if s.waitExit(2*time.Second, false) {
			s.master.Close()
			return
		}
		s.master.Write([]byte("q"))
		s.master.Write([]byte("fg\r"))
		s.master.Write([]byte("q"))
		s.master.Write([]byte("exit\r"))
		if !s.waitExit(3*time.Second, false) {
			if pid := findChildPid(s.pid); pid > 0 {
				syscall.Kill(pid, syscall.SIGKILL)
				s.waitExit(3*time.Second, false)
			}
			syscall.Kill(s.pid, syscall.SIGKILL)
			s.waitExit(3*time.Second, false)
		}
		s.master.Close()
	})
	return s
}

func start(t *testing.T, lane string, cmd *exec.Cmd) *session {
	t.Helper()
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("e2e: pty start: %v", err)
	}
	s := &session{t: t, lane: lane, master: master, cmd: cmd, pid: cmd.Process.Pid}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.out = append(s.out, buf[:n]...)
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

func (s *session) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.out)
}

func (s *session) send(keys string) {
	s.t.Helper()
	if _, err := s.master.Write([]byte(keys)); err != nil {
		s.t.Fatalf("e2e: write %q: %v", keys, err)
	}
}

func (s *session) waitFor(what string, pred func(out string) bool) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	out := s.snapshot()
	for time.Now().Before(deadline) {
		if pred(out) {
			return
		}
		time.Sleep(2 * time.Millisecond)
		out = s.snapshot()
	}
	s.t.Fatalf("e2e: timed out waiting for %s; output so far (%d bytes):\n%q",
		what, len(out), clip(out))
}

func (s *session) resize(cols, rows uint16) {
	s.t.Helper()
	if err := pty.Setsize(s.master, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		s.t.Fatalf("e2e: setsize %dx%d: %v", cols, rows, err)
	}
}

// waitExit polls for the lane's process to exit; blocking only when force
// is set. In the shell lane this is the shell itself, which outlives the
// child job, so a clean exit means the whole session drained. A pid the
// kernel no longer knows is also gone: a signal test reaps the direct
// lane's child itself, and the cleanup that runs after it must not burn
// its whole timeout waiting for a process that already left.
func (s *session) waitExit(timeout time.Duration, force bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		var ws syscall.WaitStatus
		if _, err := syscall.Wait4(s.pid, &ws, syscall.WNOHANG, nil); err == nil {
			if ws.Exited() {
				if force && ws.ExitStatus() != 0 {
					s.t.Fatalf("e2e: child exited %d, want 0; output:\n%q",
						ws.ExitStatus(), clip(s.snapshot()))
				}
				return true
			}
		} else if errors.Is(err, syscall.ECHILD) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		if force {
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// waitReap polls until the direct lane's child is reaped by this caller
// and returns how it ended. The termination contract needs the wait
// status itself, which waitExit throws away: a child killed by an
// untrapped signal and a child that unwound through Run's error return
// are indistinguishable from the parent side until Wait4 speaks, and
// killed-by-signal is exactly the failure the contract exists to prevent.
// The pid belongs to the direct lane only — in the shell lane it is the
// shell's, and the shell is not the process under signal test.
func (s *session) waitReap(timeout time.Duration) syscall.WaitStatus {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var ws syscall.WaitStatus
		if wpid, err := syscall.Wait4(s.pid, &ws, syscall.WNOHANG, nil); err == nil {
			if wpid == s.pid {
				return ws
			}
		} else {
			s.t.Fatalf("e2e: wait4(%d): %v", s.pid, err)
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("e2e: child never reaped; output:\n%q", clip(s.snapshot()))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// describeExit renders a wait status the failure messages can carry: the
// distinction the termination tests fail on is exit versus signal, and
// the raw status prints as a bare number on Linux.
func describeExit(ws syscall.WaitStatus) string {
	if ws.Exited() {
		return fmt.Sprintf("exited %d", ws.ExitStatus())
	}
	if ws.Signaled() {
		return fmt.Sprintf("killed by %v", ws.Signal())
	}
	return fmt.Sprintf("stopped or continued (raw status %d)", int(ws))
}

// findChildPid scans the process table for a child of the given pid —
// the child job under the shell, whose pid the test never learns because
// the shell owns the spawning. Linux reads /proc; macOS shells fall back
// to pgrep. Zero means "not found", which callers treat as "force-kill
// the shell and let the pty's close take the job down".
func findChildPid(parent int) int {
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil || pid == parent {
				continue
			}
			data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
			if err != nil {
				continue
			}
			// The comm field can contain spaces and parentheses, so the
			// fields after the final closing paren are the stable ones:
			// state, then ppid.
			stat := string(data)
			if idx := strings.LastIndexByte(stat, ')'); idx >= 0 && idx+2 < len(stat) {
				fields := strings.Fields(stat[idx+2:])
				if len(fields) >= 2 {
					if ppid, err := strconv.Atoi(fields[1]); err == nil && ppid == parent {
						return pid
					}
				}
			}
		}
		return 0
	}
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parent)).Output()
	if err != nil || len(out) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	if err != nil {
		return 0
	}
	return pid
}

func clip(s string) string {
	const tail = 600
	if len(s) <= tail {
		return s
	}
	return "…" + s[len(s)-tail:]
}

// TestPTYRendersReceivesKeysResizesAndRestores pins the ordinary session in
// the direct lane: enter sequences on the wire, a first frame, key
// delivery, a real SIGWINCH resize observed through the drawn size, a
// clean quit, and the leave sequences as the terminal's last bytes. The
// negotiation probe is disabled so the byte stream is exactly ours; the
// probe has its own race test.
func TestPTYRendersReceivesKeysResizesAndRestores(t *testing.T) {
	s := startSession(t, "LUX_TUI_PROBE_TIMEOUT=off")

	s.waitFor("alternate screen enter", func(out string) bool {
		return strings.Contains(out, enterAltScreen)
	})
	s.waitFor("title set", func(out string) bool {
		return strings.Contains(out, titleSet)
	})
	s.waitFor("first frame", func(out string) bool {
		return strings.Contains(out, "READY K:0 S:0 R:0 80x24")
	})

	s.send("a")
	s.waitFor("key delivered", func(out string) bool {
		return strings.Contains(out, "EVT:key=a")
	})

	s.resize(100, 30)
	s.waitFor("resize observed", func(out string) bool {
		return strings.Contains(out, "EVT:resize=100x30")
	})

	s.send("q")
	if !s.waitExit(10*time.Second, true) {
		t.Fatalf("e2e: child never exited after quit; output:\n%q", clip(s.snapshot()))
	}
	// The exit is a poor moment to sample the stream: the reader drains
	// asynchronously, so the trailing Close writes are awaited by their
	// bytes, not by the process state.
	s.waitFor("leave sequences after quit", func(out string) bool {
		return strings.Count(out, leaveAltScreen) == 1 && strings.Count(out, titlePop) == 1
	})
	out := s.snapshot()
	if strings.Count(out, leaveAltScreen) != 1 || strings.Count(out, titlePop) != 1 {
		t.Fatalf("e2e: want exactly one leave and one title pop, got %d and %d",
			strings.Count(out, leaveAltScreen), strings.Count(out, titlePop))
	}
	// The very last bytes are the title stack pop: the leave sequence
	// (modes off, cursor shown, alternate screen off, attributes reset)
	// runs first, then Close restores the title it pushed on open.
	if !strings.HasSuffix(out, titlePop) {
		t.Fatalf("e2e: output does not end with the title pop; tail:\n%q", clip(out))
	}
	if leaveAt, popAt := strings.LastIndex(out, leaveAltScreen), strings.LastIndex(out, titlePop); leaveAt < 0 || popAt < leaveAt {
		t.Fatalf("e2e: leave does not precede the final title pop")
	}
	if !strings.Contains(out, showCursor) {
		t.Fatalf("e2e: leave does not show the cursor")
	}
}

// TestPTYSuspendResumeJobControl is the Ctrl+Z / fg round trip in the shell
// lane, against a real kernel terminal with a real shell in the loop. The
// byte 0x1A arrives raw; the runtime claims it, the model pauses, the leave
// sequences make the terminal ordinary, and the process group stops for
// real — proven by the shell's own "Stopped" line, which only a waitpid on
// a genuinely stopped job produces. The shell reading "fg" as a line is
// the cooked-terminal proof: echo and line delivery mean the child's
// restore handed the shell a working terminal. fg hands the tty back and
// continues the job; the child's re-entry runs after the continue, so its
// raw mode wins, the title is re-pushed, and the dropped diff baseline
// forces a full repaint. The status row — whole only in full repaints —
// is the repaint oracle.
func TestPTYSuspendResumeJobControl(t *testing.T) {
	s := startShellSession(t, "LUX_TUI_PROBE_TIMEOUT=off")

	s.send("./childapp\r")
	s.waitFor("first frame", func(out string) bool {
		return strings.Contains(out, "READY K:0 S:0 R:0 80x24")
	})
	s.send("a")
	s.waitFor("key delivered", func(out string) bool {
		return strings.Contains(out, "EVT:key=a")
	})

	s.send("\x1a")
	s.waitFor("leave written on suspend", func(out string) bool {
		return strings.Count(out, leaveAltScreen) == 1 && strings.Count(out, titlePop) == 1
	})
	s.waitFor("shell reports the job stopped", func(out string) bool {
		return strings.LastIndex(out, "Stopped") > strings.LastIndex(out, "EVT:key=a")
	})

	s.send("fg\r")
	s.waitFor("resume counted", func(out string) bool {
		return strings.Contains(out, "S:1 R:1")
	})
	s.waitFor("full repaint after resume", func(out string) bool {
		return strings.Count(out, "READY") >= 2
	})

	s.send("q")
	s.send("exit\r")
	if !s.waitExit(10*time.Second, true) {
		t.Fatalf("e2e: shell never exited; output:\n%q", clip(s.snapshot()))
	}
	// Sampled by bytes, not by process state — the trailing writes of the
	// child's Close drain asynchronously.
	s.waitFor("suspend leave + close leave", func(out string) bool {
		return strings.Count(out, leaveAltScreen) == 2 && strings.Count(out, titlePop) == 2
	})
	out := s.snapshot()
	if strings.Count(out, titleSet) != 2 {
		t.Fatalf("e2e: want title set on open and again on resume (2), got %d",
			strings.Count(out, titleSet))
	}
}

// TestPTYKeystrokesRacingTheProbeAreReplayed runs with the default probe
// budget and types before the probe can finish: a pty answers nothing, so
// negotiation must time out (150 ms) and the keystroke typed into it must
// still arrive — the "replayed, never dropped" promise, exercised against
// real kernel input buffering rather than a fake backend.
func TestPTYKeystrokesRacingTheProbeAreReplayed(t *testing.T) {
	s := startSession(t)

	s.send("a")
	s.waitFor("racing keystroke replayed", func(out string) bool {
		return strings.Contains(out, "EVT:key=a")
	})

	s.send("q")
	if !s.waitExit(10*time.Second, true) {
		t.Fatalf("e2e: child never exited after quit; output:\n%q", clip(s.snapshot()))
	}
}

// TestPTYSuspendRefusesToDoubleWrite pins the double-stop safety in the
// shell lane: a second SIGTSTP raised straight at the job while it is
// already stopped. The kernel holds the second stop pending, the latch
// refuses to run the handshake a second time (no new leave bytes), and
// the fg's SIGCONT discards the pending stop — exactly one suspension is
// delivered, so the child resumes once, with one leave on the wire at the
// moment of the continue.
func TestPTYSuspendRefusesToDoubleWrite(t *testing.T) {
	s := startShellSession(t, "LUX_TUI_PROBE_TIMEOUT=off")

	s.send("./childapp\r")
	s.waitFor("first frame", func(out string) bool {
		return strings.Contains(out, "READY K:0 S:0 R:0 80x24")
	})

	s.send("\x1a")
	s.waitFor("leave written on suspend", func(out string) bool {
		return strings.Count(out, leaveAltScreen) == 1
	})
	s.waitFor("shell reports the job stopped", func(out string) bool {
		return strings.Contains(out, "Stopped")
	})
	if pid := findChildPid(s.pid); pid > 0 {
		syscall.Kill(pid, syscall.SIGTSTP)
	}
	time.Sleep(200 * time.Millisecond)

	s.send("fg\r")
	s.waitFor("one suspension only", func(out string) bool {
		return strings.Contains(out, "S:1 R:1")
	})
	if got := strings.Count(s.snapshot(), leaveAltScreen); got != 1 {
		t.Fatalf("e2e: second SIGTSTP wrote extra leaves: %d", got)
	}

	s.send("q")
	s.send("exit\r")
	if !s.waitExit(10*time.Second, true) {
		t.Fatalf("e2e: shell never exited; output:\n%q", clip(s.snapshot()))
	}
	s.waitFor("close leave after quit", func(out string) bool {
		return strings.Count(out, leaveAltScreen) == 2
	})
}

// TestPTYTerminationSignalsRestoreBeforeExit pins the external-kill half of
// the Unix termination contract in the direct lane: a SIGINT or SIGTERM
// from outside — a supervisor, a kill from another terminal — must be
// trapped, not left to the default disposition. The trap's two halves have
// two different oracles. The wait status is the one the bytes cannot fake:
// an untrapped termination leaves the child killed by the kernel, a trapped
// one exits on its own feet through Run's error return, and the child maps
// every Run error to exit 0, so a clean reap with code 0 means the run loop
// unwound through the error event. The wire bytes are the restore's oracle:
// exactly one leave set and one title pop, the title pop last — the same
// terminal state the ordinary quit path leaves behind, because Close runs
// on top of the watcher's latch and must not write a second set. A Ctrl+C
// typed into the terminal is deliberately not this test: raw mode claims
// the byte, the model sees a Key event, and that ownership is what keeps
// the trap from stealing interactive quit from the model.
func TestPTYTerminationSignalsRestoreBeforeExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startSession(t, "LUX_TUI_PROBE_TIMEOUT=off")

			s.waitFor("first frame", func(out string) bool {
				return strings.Contains(out, "READY K:0 S:0 R:0 80x24")
			})

			syscall.Kill(s.pid, tc.sig)

			ws := s.waitReap(10 * time.Second)
			if !ws.Exited() || ws.ExitStatus() != 0 {
				t.Fatalf("e2e: child %s after %s, want exit 0 — the trap must turn the signal into an ordinary Run return; output:\n%q",
					describeExit(ws), tc.name, clip(s.snapshot()))
			}
			// The exit is a poor moment to sample the stream: the reader
			// drains asynchronously, so the restore bytes are awaited by
			// their bytes, not by the process state.
			s.waitFor("leave sequences written by the trap", func(out string) bool {
				return strings.Count(out, leaveAltScreen) == 1 && strings.Count(out, titlePop) == 1
			})
			out := s.snapshot()
			if got := strings.Count(out, leaveAltScreen); got != 1 {
				t.Fatalf("e2e: want exactly one leave after %s, got %d — Close must not double-restore on top of the watcher's latch", tc.name, got)
			}
			if !strings.HasSuffix(out, titlePop) {
				t.Fatalf("e2e: output does not end with the title pop; tail:\n%q", clip(out))
			}
			if !strings.Contains(out, showCursor) {
				t.Fatalf("e2e: restore does not show the cursor")
			}
		})
	}
}
