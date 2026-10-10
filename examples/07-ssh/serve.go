//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	gssh "github.com/gliderlabs/ssh"
)

// ptySession is the slice of gssh.Session the pty bridge touches. It exists
// so tests can drive serve with a fake session instead of a live connection;
// real sessions satisfy it structurally.
type ptySession interface {
	io.Reader
	io.Writer
	Pty() (gssh.Pty, <-chan gssh.Window, bool)
	Context() gssh.Context
	Exit(code int) error
}

// serve bridges one SSH session onto a fresh pty and runs argv there as the
// session leader. Everything else follows from pty semantics: the child is
// the foreground process group, so the kernel hands it SIGWINCH when the
// client resizes; raw mode and capability probes travel through the tunnel
// to whatever terminal sits at the client end; and the child's exit status
// propagates back as an SSH exit-status request.
func serve(s ptySession, argv []string) {
	ptyReq, winch, ok := s.Pty()
	if !ok {
		fmt.Fprintln(s, "tui-ssh-server needs a pty: connect with ssh -t")
		_ = s.Exit(1)
		return
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM="+ptyReq.Term)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(ptyReq.Window.Height), Cols: uint16(ptyReq.Window.Width)})
	if err != nil {
		fmt.Fprintln(s, "tui-ssh-server: start:", err)
		_ = s.Exit(1)
		return
	}

	// Window changes the client asks for become TIOCSWINSZ on the pty. A
	// zero size (piped `ssh -t`) is forwarded as-is; the engine gates
	// frames at 0x0 by design. See README.md, "Zero-size ptys".
	//
	// The loop is quiesced before the master fd is closed: pty.Setsize
	// issues an ioctl on that fd, and an ioctl racing os.File.Close is a
	// data race the race detector (rightly) flags.
	ioStop := make(chan struct{})
	resizingDone := make(chan struct{})
	var quiesce sync.Once
	stopResizing := func() {
		quiesce.Do(func() { close(ioStop) })
		<-resizingDone
	}
	go func() {
		defer close(resizingDone)
		for {
			select {
			case <-ioStop:
				return
			case win, ok := <-winch:
				if !ok {
					return
				}
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(win.Height), Cols: uint16(win.Width)})
			}
		}
	}()

	// A disconnect must not leak the child. The connection context is
	// canceled when the ssh channel dies; killing the child's process
	// group (it is a session leader after setsid) also reaps anything it
	// spawned.
	bridge := make(chan struct{})
	defer close(bridge)
	go func() {
		select {
		case <-s.Context().Done():
			killChild(cmd)
			stopResizing()
			_ = ptmx.Close()
		case <-bridge:
		}
	}()

	go func() { _, _ = io.Copy(ptmx, s) }() // keystrokes: client -> child
	_, _ = io.Copy(s, ptmx)                 // frames: child -> client

	stopResizing()
	_ = ptmx.Close()
	_ = s.Exit(exitCode(cmd.Wait()))
}

// exitCode maps a Wait error to an SSH exit status. A signal death is
// reported as 1: the SSH exit-status message has no "killed" encoding.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
	}
	return 1
}

// killChild terminates the child and any processes it spawned. creack/pty
// made the child a session leader with setsid, so its pid doubles as the
// process-group id.
func killChild(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
