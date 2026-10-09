//go:build !windows

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	gssh "github.com/gliderlabs/ssh"
)

// fakeContext implements gssh.Context with no metadata; only Done matters
// to the pty bridge.
type fakeContext struct {
	context.Context
	mu sync.Mutex
}

func (c *fakeContext) Lock()                          { c.mu.Lock() }
func (c *fakeContext) Unlock()                        { c.mu.Unlock() }
func (c *fakeContext) User() string                   { return "test" }
func (c *fakeContext) SessionID() string              { return "test" }
func (c *fakeContext) ClientVersion() string          { return "test" }
func (c *fakeContext) ServerVersion() string          { return "test" }
func (c *fakeContext) RemoteAddr() net.Addr           { return nil }
func (c *fakeContext) LocalAddr() net.Addr            { return nil }
func (c *fakeContext) Permissions() *gssh.Permissions { return &gssh.Permissions{} }
func (c *fakeContext) SetValue(key, value any)        {}

// fakeSession reproduces the session surface serve uses, including the
// newline munging gliderlabs applies to pty sessions: a lone \n becomes
// \r\n, \r\n stays \r\n, a lone \r is untouched. Input comes from a pipe
// that Exit closes, mimicking a channel that closes with the session.
type fakeSession struct {
	reader  io.Reader
	typeIn  io.Writer
	closeIn func() error
	mu      sync.Mutex
	out     bytes.Buffer
	ctx     *fakeContext
	term    string
	window  gssh.Window
	winch   chan gssh.Window
	hasPty  bool
	exit    int
	exited  bool
}

func newFakeSession(t *testing.T, term string, width, height int) (*fakeSession, context.CancelFunc) {
	t.Helper()
	inner, cancel := context.WithCancel(context.Background())
	stdin, stdinWriter := io.Pipe()
	s := &fakeSession{
		reader:  stdin,
		typeIn:  stdinWriter,
		closeIn: stdin.Close,
		ctx:     &fakeContext{Context: inner},
		term:    term,
		window:  gssh.Window{Width: width, Height: height},
		winch:   make(chan gssh.Window, 1),
		hasPty:  true,
	}
	s.winch <- s.window
	return s, cancel
}

func (s *fakeSession) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *fakeSession) Write(p []byte) (int, error) {
	n := len(p)
	if s.hasPty {
		p = bytes.Replace(p, []byte{'\n'}, []byte{'\r', '\n'}, -1)
		p = bytes.Replace(p, []byte{'\r', '\r', '\n'}, []byte{'\r', '\n'}, -1)
	}
	s.mu.Lock()
	s.out.Write(p)
	s.mu.Unlock()
	// A terminal answers cursor-position queries; busybox ash's line editor
	// blocks waiting for the reply, so the fake must behave like a terminal
	// here too.
	if s.hasPty && bytes.Contains(p, []byte("\x1b[6n")) {
		go func() { _, _ = s.typeIn.Write([]byte("\x1b[1;1R")) }()
	}
	return n, nil
}

func (s *fakeSession) Pty() (gssh.Pty, <-chan gssh.Window, bool) {
	return gssh.Pty{Term: s.term, Window: s.window}, s.winch, s.hasPty
}

func (s *fakeSession) Context() gssh.Context { return s.ctx }

func (s *fakeSession) Exit(code int) error {
	s.mu.Lock()
	if !s.exited {
		s.exited = true
		s.exit = code
		_ = s.closeIn()
	}
	s.mu.Unlock()
	return nil
}

func (s *fakeSession) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *fakeSession) done() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *fakeSession) code() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exit
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, dump func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v; state:\n%s", timeout, dump())
}

func TestServeRefusesSessionWithoutPty(t *testing.T) {
	s, cancel := newFakeSession(t, "xterm-256color", 80, 24)
	s.hasPty = false
	defer cancel()

	serve(s, []string{"sh", "-c", "echo should-not-run"})

	if !s.done() || s.code() != 1 {
		t.Fatalf("no-pty session must exit 1, got done=%v code=%d", s.done(), s.code())
	}
	if !strings.Contains(s.text(), "needs a pty") {
		t.Fatalf("missing refusal message; output:\n%q", s.text())
	}
}

func TestServePtySizeTermAndExitStatus(t *testing.T) {
	s, cancel := newFakeSession(t, "xterm-256color", 80, 24)
	defer cancel()

	script := "stty size; printf 'TERM=%s\\n' \"$TERM\"; printf 'A\\nB\\n'; exit 7"
	go serve(s, []string{"sh", "-c", script})

	waitFor(t, 10*time.Second, s.done, func() string { return s.text() })

	// stty only succeeds on a real tty, so "24 80" proves the child runs on
	// a pty sized from the pty-req, and TERM proves client propagation.
	if !strings.Contains(s.text(), "24 80") {
		t.Fatalf("pty size not reported; output:\n%q", s.text())
	}
	if !strings.Contains(s.text(), "TERM=xterm-256color") {
		t.Fatalf("TERM not propagated; output:\n%q", s.text())
	}
	// The session writer munges lone newlines into \r\n; the fake must
	// reproduce that or the test lies about the real wire format.
	if !strings.Contains(s.text(), "A\r\nB\r\n") {
		t.Fatalf("newline munging not visible; output:\n%q", s.text())
	}
	if s.code() != 7 {
		t.Fatalf("child exit status must propagate, got %d", s.code())
	}
}

func TestServeResizeDrivesNewPtySize(t *testing.T) {
	s, cancel := newFakeSession(t, "xterm-256color", 80, 24)
	defer cancel()

	go serve(s, []string{"sh"})

	writeLine := func(line string) {
		if _, err := s.typeIn.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("typing %q: %v", line, err)
		}
	}
	writeLine("stty size")
	waitFor(t, 10*time.Second,
		func() bool { return strings.Contains(s.text(), "24 80") },
		func() string { return s.text() })

	s.winch <- gssh.Window{Width: 100, Height: 30}

	// stty reports the size the moment it runs, so re-ask until the resize
	// lands; this keeps the test robust without sleeping a fixed interval.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(s.text(), "30 100") {
		if time.Now().After(deadline) {
			t.Fatalf("resize never reached the pty; output:\n%q", s.text())
		}
		writeLine("stty size")
		time.Sleep(50 * time.Millisecond)
	}

	writeLine("exit 0")
	waitFor(t, 10*time.Second, s.done, func() string { return s.text() })
	if s.code() != 0 {
		t.Fatalf("shell exit status must propagate, got %d", s.code())
	}
}

func TestServeDisconnectKillsChild(t *testing.T) {
	s, cancel := newFakeSession(t, "xterm-256color", 80, 24)
	defer cancel()

	// sleep never reads the tty, so only the kill path can end it.
	go serve(s, []string{"sleep", "60"})

	time.Sleep(300 * time.Millisecond)
	if s.done() {
		t.Fatalf("session ended before the disconnect")
	}
	cancel()

	waitFor(t, 10*time.Second, s.done, func() string { return s.text() })
	if s.code() != 1 {
		t.Fatalf("a killed child must map to exit status 1, got %d", s.code())
	}
}

func TestExitCodeMapsSignalDeathToOne(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Fatalf("clean exit must map to 0, got %d", got)
	}
	cmd := exec.Command("sh", "-c", "kill -9 $$")
	_ = cmd.Run() // Run records the signal death in ExitError
	if got := exitCode(cmd.Wait()); got != 1 {
		t.Fatalf("signal death must map to 1, got %d", got)
	}
}
