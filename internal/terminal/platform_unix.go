//go:build !windows

package terminal

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// defaultColorDepth reports the color floor for Unix terminals. Unix hosts a
// much wider range of terminals than Windows, from modern emulators to the
// 16-color Linux console and dumb serial lines, so the floor stays low and
// the environment overrides in detectCapabilities promote it when earned.
func defaultColorDepth() capability.ColorDepth {
	return capability.ANSI16
}

func enableVT(*os.File) (func() error, error) { return nil, nil }

// makeRawTerm is the Unix raw-mode entry: the term package already produces
// exactly the mode the parser needs, and the restore story is a single
// term.Restore. Windows needs the extra VT-input bit and wraps this in
// platform_windows.go.
func makeRawTerm(fd int) (*term.State, error) { return term.MakeRaw(fd) }

func duplicateFile(file *os.File, name string) (*os.File, error) {
	if file == nil {
		return nil, nil
	}
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func terminalSize(file *os.File) (int, int, error) { return term.GetSize(int(file.Fd())) }

var errReadTimeout = errors.New("terminal: read timeout")

// errHangup is the terminal-loss error the lifecycle watcher reports when the
// controlling terminal goes away: the read loop's EOF uses the same error
// path, so a terminal that ends — hangup or clean close — always exits the
// application through an event, never a panic.
var errHangup = errors.New("terminal: hangup")

func readInput(ctx context.Context, file *os.File, buffer []byte, timeout time.Duration) (int, error) {
	fd := int(file.Fd())
	milliseconds := 100
	if timeout >= 0 {
		milliseconds = max(1, int(timeout/time.Millisecond))
	}
	for {
		ready, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, milliseconds)
		if err != nil && err != syscall.EINTR {
			return 0, err
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if ready > 0 {
			return file.Read(buffer)
		}
		if timeout >= 0 {
			return 0, errReadTimeout
		}
	}
}

func cancelRead(*os.File) {}

type resizeWatcher struct {
	signals chan os.Signal
}

func (w *resizeWatcher) start(ctx context.Context, file *os.File, resizes chan event.Event) {
	w.signals = make(chan os.Signal, 1)
	signal.Notify(w.signals, syscall.SIGWINCH)
	go func(signals <-chan os.Signal) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				width, height, err := terminalSize(file)
				if err != nil {
					continue
				}
				// The signal is already consumed, so a non-blocking send
				// could drop the only notification; the last-wins slot keeps
				// the newest size pending no matter how deep the input
				// queue is.
				deliverResize(resizes, event.ResizeTo(width, height))
			}
		}
	}(w.signals)
}

func (w *resizeWatcher) stop() {
	if w.signals != nil {
		signal.Stop(w.signals)
		w.signals = nil
	}
}

// SuspendSupported reports whether the platform implements job-control
// suspension. Unix does; the Windows console has no job-control analogue, so
// Suspend there always reports false and the suspend key reaches the model
// as an ordinary key event.
const SuspendSupported = true

// raiseStop stops the whole process group the way a shell's job control
// would, indirected as a var so the suspend handshake is testable in a
// process that must not really stop: unit tests swap the var and pin the
// byte sequence around it, while the PTY verification lane runs the real
// kernel stop. The group id 0 — not getpid — matters: the shell continues
// the group, and a stop aimed at one pid leaves group semantics behind.
var raiseStop = func() error { return unix.Kill(0, unix.SIGTSTP) }

// CanSuspend reports whether a job-control handshake can start right now.
// It is the pre-flight the run loop uses to decide whether a Ctrl+Z means
// suspend or an ordinary key: the Suspend event must not reach a model
// whose process is never going to stop, so the refusal has to be knowable
// before the handshake, not discovered inside it. A true answer is a
// promise — while the application runs, only the run loop moves the
// suspend state, so the Suspend call that follows either enters the
// handshake or fails with an error.
func (b *Backend) CanSuspend() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opened && !b.plain && !b.suspended
}

// watchLifecycle watches for terminal loss. Job-control suspension is NOT
// handled here: this process never notifies SIGTSTP, so the kernel's default
// disposition survives untouched and a stop that arrives anyway — an
// external kill -TSTP, or a Ctrl+Z forwarded by a terminal in cooked mode —
// stops the process the ordinary kernel way. The raw-mode runtime claims
// the Ctrl+Z byte itself and runs the suspend handshake on the run loop's
// goroutine through Backend.Suspend, which is the only place that can
// order the model's Suspend event before the leave sequences. What the
// watcher owns is SIGHUP: the controlling terminal went away, which is an
// error event and an exit, never a suspend. Reset in the deferred cleanup
// restores the default disposition a shell expects to find after the
// program exits — a terminal loss must not leave behind a process that
// survives the next HUP.
func (b *Backend) watchLifecycle(ctx context.Context) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	defer signal.Reset(syscall.SIGHUP)
	for {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			// The controlling terminal is gone: raw mode, the alternate
			// screen, and any restore all died with it — writes would fail
			// and the read loop already saw EOF — so the loss is reported
			// through the same error path and the application exits through
			// its deferred Close. The watcher stops too: a dead terminal
			// produces no further job control.
			b.mu.Lock()
			opened := b.opened
			b.mu.Unlock()
			if opened {
				b.deliverLifecycle(event.Event{Kind: event.Error, Err: errHangup})
			}
			return
		}
	}
}

// Suspend hands the terminal to the shell, stops the process group, and
// blocks until the shell continues it. It is the application-facing half of
// job control: the run loop calls it after the model has already seen the
// Suspend event, so the leave sequences that make the terminal ordinary
// land on a model that is already paused.
//
// The stop itself is never notified by this process — the Go runtime
// installs no handler for a signal nobody calls Notify on, so SIGTSTP's
// kernel default survives — and is raised on the whole group, exactly the
// handshake shells, tmux, and vim run. A second Ctrl+Z inside the stop
// window queues a second stop; SIGCONT always discards pending stop
// signals, so a queued stop cannot wedge the resume.
//
// The returned bool reports which of two outcomes happened: true means the
// terminal was handed over and the process group really stopped, and the
// caller follows up with the Resume event; false means the handshake never
// started — a plain or closed backend refuses before anything changes, so
// the caller should let the suspend key reach the model as an ordinary key
// event. An error means the window ended abnormally: a hangup lost the
// terminal, the context was canceled, or the re-entry failed. Every error
// path relies on the deferred Close for cleanup: the leave sequences are
// idempotent, and Close's own leave after a failed resume is the
// documented belt-and-braces.
//
// SIGCONT and SIGHUP are armed on a private channel before the stop is
// raised, so a continue that races ahead of the observed stop still wakes
// the wait. The channel is detached with Stop — not Reset — when the wait
// ends: Reset would also tear down the lifecycle watcher's SIGHUP
// registration, and a Suspend call must never silence the hangup watcher.
func (b *Backend) Suspend(ctx context.Context) (bool, error) {
	if !b.suspendTerminal() {
		return false, nil
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGCONT, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := raiseStop(); err != nil {
		// The group stop itself failed, which only happens on a malformed
		// call; the terminal is already handed over and latched, so the
		// error routes the exit and the deferred Close restores.
		return true, err
	}
	if err := b.waitContinue(ctx, signals); err != nil {
		return true, err
	}
	if err := b.resumeTerminal(); err != nil {
		return true, err
	}
	// The terminal may have been resized while the process was stopped; the
	// SIGWINCH the resize watcher buffers is delivered only after the
	// continue, so the fresh size rides the wakeup without waiting for it,
	// and the slot's last-wins semantics keep the duplicate harmless.
	if width, height, err := terminalSize(b.input); err == nil {
		deliverResize(b.resizes, event.ResizeTo(width, height))
	}
	return true, nil
}

// waitContinue blocks until SIGCONT, a hangup, or cancellation — the three
// ways the stop window can end. The continue channel is armed before the
// stop is raised, so a shell's fg that races ahead of the observed stop
// still wakes the wait. A hangup ends the window with errHangup; the error
// is returned rather than delivered as an event because the caller is the
// run loop itself — the same goroutine that would have consumed the event.
func (b *Backend) waitContinue(ctx context.Context, signals <-chan os.Signal) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig := <-signals:
			switch sig {
			case syscall.SIGCONT:
				return nil
			case syscall.SIGHUP:
				return errHangup
			}
		}
	}
}
