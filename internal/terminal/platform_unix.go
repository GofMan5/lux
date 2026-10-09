//go:build !windows

package terminal

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
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

// watchLifecycle translates Unix job-control signals into the terminal
// suspend and resume dance. SIGTSTP is caught so the stop happens only after
// the leave sequences and cooked restore have run — Ctrl+Z would otherwise
// stop the process mid-frame with raw mode, the alternate screen, and every
// negotiated mode still latched, leaving the shell unusable. The stop itself
// is then handed back to the kernel: Notify consumed the signal, so the
// default disposition has to be restored and the signal re-raised, which is
// the canonical job-control handshake (the same one the shells, tmux, and
// vim run). SIGCONT re-enters the terminal state and reports the resume; a
// SIGHUP means the controlling terminal is gone, and exits through the error
// event the read loop's EOF uses.
func (b *Backend) watchLifecycle(ctx context.Context) {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGTSTP, syscall.SIGCONT, syscall.SIGHUP)
	defer func() {
		// Reset restores the dispositions a shell expects to find after the
		// program exits: Stop only detaches this channel and leaves the
		// runtime consuming the signals. SIGHUP's default terminate matters
		// most — a terminal loss must not leave a deaf process behind.
		signal.Reset(syscall.SIGTSTP, syscall.SIGCONT, syscall.SIGHUP)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-signals:
			switch sig {
			case syscall.SIGTSTP:
				if !b.suspendTerminal() {
					// Plain, closed, or already suspended: the signal is
					// consumed and the state is either absent or already
					// correct, so the stop is not re-raised — a closed
					// backend must not stop a process that is exiting.
					continue
				}
				b.deliverLifecycle(event.Event{Kind: event.Suspend})
				// Reset restores SIG_DFL before the re-raise; the SIGCONT
				// and SIGHUP arms stay registered, so the continue wakes
				// this watcher and a terminal loss still reports. The
				// process stops inside the kill and returns here only
				// after the shell's SIGCONT.
				signal.Reset(syscall.SIGTSTP)
				_ = unix.Kill(unix.Getpid(), unix.SIGTSTP)
				// Re-register before taking the terminal back, so a second
				// Ctrl+Z landing in this window is queued and handled
				// instead of stopping the process behind raw mode's back.
				signal.Notify(signals, syscall.SIGTSTP)
				if err := b.resumeTerminal(); err != nil {
					b.deliverLifecycle(event.Event{Kind: event.Error, Err: err})
					continue
				}
				// The terminal may have been resized while the process was
				// stopped; the SIGWINCH the resize watcher buffers is
				// delivered only after the continue, so the fresh size
				// rides the wakeup without waiting for it, and the slot's
				// last-wins semantics keep the duplicate harmless.
				if width, height, err := terminalSize(b.input); err == nil {
					deliverResize(b.resizes, event.ResizeTo(width, height))
				}
				b.deliverLifecycle(event.Event{Kind: event.Resume})
			case syscall.SIGCONT:
				// A continue without a matching stop through this watcher —
				// kill -CONT on a background job, most commonly — finds the
				// terminal state already current, so nothing is re-entered
				// and no event is synthesized: a resume that was never
				// suspended is a no-op, not a repaint trigger.
			case syscall.SIGHUP:
				// The controlling terminal is gone: raw mode, the alternate
				// screen, and any restore all died with it — writes would
				// fail and the read loop already saw EOF — so the loss is
				// reported through the same error path and the application
				// exits through its deferred Close. The watcher stops too:
				// a dead terminal produces no further job control.
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
}
