//go:build windows

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
	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// defaultColorDepth reports the color floor for a Windows console. It is
// platform detection, not probing: Windows Terminal advertises itself in the
// environment and is known to render 24-bit color, while legacy conhost
// safely renders the 256-color palette under VT processing. Environment
// overrides in detectCapabilities still win over this floor.
func defaultColorDepth() capability.ColorDepth {
	if os.Getenv("WT_SESSION") != "" {
		return capability.TrueColor
	}
	return capability.ANSI256
}

const enableVirtualTerminalProcessing = 0x0004

// enableVirtualTerminalInput is the console input flag that makes conhost
// and conpty translate function, arrow, and modifier keys into the escape
// sequences the parser decodes. Without it a raw Read on the console only
// ever receives keys that carry a character code — F1 and the arrows reach
// nothing — which is exactly the gap the Windows Terminal verification
// lane caught: the rune keys worked, F1 never arrived.
const enableVirtualTerminalInput = 0x0200

// makeRawTerm drives the console input into the mode the parser expects.
// term.MakeRaw clears the line/echo/processed bits, and the extra
// ENABLE_VIRTUAL_TERMINAL_INPUT bit turns key events into VT input. MakeRaw
// snapshots the mode before any change, so a single term.Restore rolls both
// changes back — the suspend/resume dance keeps working unchanged. Legacy
// consoles that reject the bit degrade to MakeRaw's mode instead of failing
// Open: the raw character path still works there.
func makeRawTerm(fd int) (*term.State, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	handle := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return state, nil
	}
	if mode&enableVirtualTerminalInput == 0 {
		if err := windows.SetConsoleMode(handle, mode|enableVirtualTerminalInput); err != nil {
			return state, nil
		}
	}
	return state, nil
}

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
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(process, windows.Handle(file.Fd()), process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(duplicate), name), nil
}

// terminalSize reports the active console's size. The Windows console splits
// its handles by direction: the screen-buffer query behind the size needs an
// output handle and fails with an invalid-handle error on the input handle,
// so a caller holding the input side — the resize poller, the probe-time
// size snapshot — resolves the real answer by opening the console's own
// output. Measured under ambient load, the fallback cycle costs tens of
// microseconds (open, query, close), about 1.4 ms per second at the poller's
// 100 ms cadence, and it also keeps Size() honest when stdout is redirected
// while a console is still attached.
func terminalSize(file *os.File) (int, int, error) {
	if width, height, err := term.GetSize(int(file.Fd())); err == nil {
		return width, height, nil
	}
	console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = console.Close() }()
	return term.GetSize(int(console.Fd()))
}

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

func (w *resizeWatcher) start(parent context.Context, file *os.File, resizes chan event.Event) {
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
				// Advancing the baseline before the delivery is safe:
				// deliverResize's last-wins slot always accepts the newest
				// size, so a deep input queue can delay it but never drop it.
				width, height = nextWidth, nextHeight
				deliverResize(resizes, event.ResizeTo(width, height))
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

// SuspendSupported reports whether the platform implements job-control
// suspension. The Windows console has no analogue: processes are not grouped
// under a shell's job control, there is no stop-and-continue handshake, and
// the console's Ctrl handling is a different mechanism entirely. Suspend on
// Windows therefore always reports false and the run loop lets the suspend
// key reach the model as an ordinary key event.
const SuspendSupported = false

// CanSuspend is always false on the Windows console: the pre-flight lets
// the run loop drop Ctrl+Z onto the model without ever synthesizing a
// Suspend event for a process that cannot stop.
func (b *Backend) CanSuspend() bool {
	return false
}

// Suspend is the Windows half of the suspend handshake contract: it never
// starts one. The console has no job control to hand a terminal back to,
// so the honest answer is the refusal, not an emulation.
func (b *Backend) Suspend(context.Context) (bool, error) {
	return false, nil
}

// errConsoleClose is the error the lifecycle watcher reports for the console
// events Windows delivers as SIGTERM: CTRL_CLOSE_EVENT (the window's close
// button), CTRL_LOGOFF_EVENT, and CTRL_SHUTDOWN_EVENT.
var errConsoleClose = errors.New("terminal: console close event")

// watchLifecycle waits for the SIGTERM that Windows console events arrive
// as. os/signal translates CTRL_CLOSE_EVENT, CTRL_LOGOFF_EVENT, and
// CTRL_SHUTDOWN_EVENT into syscall.SIGTERM when a handler is registered, and
// the translation is explicitly non-blocking: the process is terminated
// unless it exits, and Notify only buys the cleanup window. The terminal
// restore therefore happens right here on the watcher — within microseconds
// of the event — instead of waiting for the application loop to unwind, and
// the error event that follows still routes the exit through that loop and
// its deferred Close whenever the OS grants the time. This is honest best
// effort: the restore wins the race more often than an app that starts
// unwinding only after the event, but no handler can promise it.
func (b *Backend) watchLifecycle(ctx context.Context) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	for {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			// suspendTerminal is reused for its terminal half — the leave
			// sequences and the cooked restore — while the suspended latch
			// it sets doubles as the present gate: the dying application
			// stops painting frames onto a console that is closing. The
			// process exits through the error path either way, and Close's
			// own restore is idempotent on top of this one.
			b.suspendTerminal()
			b.deliverLifecycle(event.Event{Kind: event.Error, Err: errConsoleClose})
			return
		}
	}
}
