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

func (w *resizeWatcher) start(ctx context.Context, file *os.File, events chan<- event.Event) {
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
				select {
				case events <- event.ResizeTo(width, height):
				default:
				}
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
