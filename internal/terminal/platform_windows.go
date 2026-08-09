//go:build windows

package terminal

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/lux-tui/lux/event"
	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

const enableVirtualTerminalProcessing = 0x0004

func enableVT(file *os.File) (func() error, error) {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing); err != nil {
		return nil, err
	}
	return func() error { return windows.SetConsoleMode(handle, mode) }, nil
}

func duplicateFile(file *os.File, name string) (*os.File, error) {
	if file == nil {
		return nil, nil
	}
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(process, windows.Handle(file.Fd()), process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(duplicate), name), nil
}

func terminalSize(file *os.File) (int, int, error) { return term.GetSize(int(file.Fd())) }

var errReadTimeout = errors.New("terminal: read timeout")

func readInput(ctx context.Context, file *os.File, buffer []byte, timeout time.Duration) (int, error) {
	handle := windows.Handle(file.Fd())
	milliseconds := uint32(100)
	if timeout >= 0 {
		milliseconds = uint32(max(1, int(timeout/time.Millisecond)))
	}
	for {
		result, err := windows.WaitForSingleObject(handle, milliseconds)
		if err != nil {
			return 0, err
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if result == windows.WAIT_OBJECT_0 {
			return file.Read(buffer)
		}
		if timeout >= 0 {
			return 0, errReadTimeout
		}
	}
}

func cancelRead(file *os.File) {
	if file != nil {
		_ = windows.CancelIoEx(windows.Handle(file.Fd()), nil)
	}
}

type resizeWatcher struct {
	cancel context.CancelFunc
}

func (w *resizeWatcher) start(parent context.Context, file *os.File, events chan<- event.Event) {
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	go func() {
		width, height, _ := terminalSize(file)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				nextWidth, nextHeight, err := terminalSize(file)
				if err != nil || nextWidth == width && nextHeight == height {
					continue
				}
				width, height = nextWidth, nextHeight
				select {
				case events <- event.ResizeTo(width, height):
				default:
				}
			}
		}
	}()
}

func (w *resizeWatcher) stop() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}
