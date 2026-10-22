//go:build windows

package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
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

// peekNamedPipe wraps kernel32's PeekNamedPipe: it never consumes, it
// reports how many bytes the read end holds and whether the write end is
// still attached. x/sys does not export the entry point, so it comes
// through the lazy loader like any other raw call.
var (
	modkernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procPeekNamedPipe = modkernel32.NewProc("PeekNamedPipe")
)

func peekNamedPipe(handle windows.Handle, available *uint32) error {
	r1, _, errno := procPeekNamedPipe.Call(
		uintptr(handle), 0, 0, 0,
		uintptr(unsafe.Pointer(available)), 0,
	)
	if r1 == 0 {
		return errno
	}
	return nil
}

// waitSlice bounds every WaitForSingleObject and pipe-poll sleep so a
// context cancellation or a deadline is noticed within one slice no
// matter how long the caller's timeout is. Windows waits are not
// interruptible, so responsiveness is bought by slicing, not by the
// kernel.
const waitSlice = 100 * time.Millisecond

// readInput reads one chunk from file while honoring ctx and timeout.
// Console input handles are the one object type whose wait contract is
// documented: WaitForSingleObject signals exactly when input records are
// queued. Everything else the backend reads — the anonymous pipes of
// os.Pipe above all — carries no such contract: Go creates pipes with
// CreatePipe, and a synchronous pipe handle signals by kernel-flavor
// rules (never on client Windows, "signaled when idle" on server
// releases, the mismatch that parked the Windows CI read loop inside a
// Read no context could cancel). Pipes therefore poll their byte count
// through PeekNamedPipe, whose answer is contractual, and non-pipe
// non-console files fall back to a direct read.
// readWaker is the Unix poll wake-up; the console wait below is already
// sliced and costs nothing measurable while idle, so Windows needs none.
type readWaker struct{}

func newReadWaker(context.Context) *readWaker { return nil }

func (*readWaker) close() {}

func readInput(ctx context.Context, _ *readWaker, file *os.File, buffer []byte, timeout time.Duration) (int, error) {
	handle := windows.Handle(file.Fd())
	var consoleMode uint32
	console := windows.GetConsoleMode(handle, &consoleMode) == nil
	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if console {
			slice := waitSlice
			if timeout >= 0 && timeout < waitSlice {
				slice = max(time.Millisecond, timeout)
			}
			result, err := windows.WaitForSingleObject(handle, uint32(slice.Milliseconds()))
			if err != nil {
				return 0, err
			}
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if result == windows.WAIT_OBJECT_0 {
				return file.Read(buffer)
			}
		} else {
			var available uint32
			if err := peekNamedPipe(handle, &available); err != nil {
				if errors.Is(err, windows.ERROR_BROKEN_PIPE) {
					// The write end is gone: surface the same EOF a
					// blocking read would, so a read loop left on the
					// pipe exits on last-writer close instead of
					// polling forever.
					return 0, io.EOF
				}
				if errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
					// Not a pipe and not a console — a regular file.
					// Those reads complete on their own; there is no
					// wait contract to honor, so read directly.
					return file.Read(buffer)
				}
				return 0, err
			}
			if available > 0 {
				// Reading only when bytes are queued keeps the Read
				// short: the kernel returns at most what the pipe
				// holds, never parking the goroutine on an empty
				// handle.
				return file.Read(buffer)
			}
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return 0, errReadTimeout
		}
		if !console {
			sleep := waitSlice / 10
			if !deadline.IsZero() {
				if remaining := time.Until(deadline); remaining < sleep {
					sleep = remaining
				}
			}
			time.Sleep(sleep)
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
			// latch is also Close's receipt that this restore already ran:
			// the exit path's own leave write is skipped, so the unwind
			// never double-restores. The process exits through the error
			// path either way; this is honest best effort — the restore
			// wins the race more often than an app that starts unwinding
			// only after the event, but no handler can promise it.
			b.suspendTerminal()
			b.deliverLifecycle(event.Event{Kind: event.Error, Err: errConsoleClose})
			return
		}
	}
}
