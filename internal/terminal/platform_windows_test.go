//go:build windows

package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// TestMakeRawTermEnablesVTInput pins the Windows raw-mode input contract: a
// raw console must carry ENABLE_VIRTUAL_TERMINAL_INPUT, or conhost and
// conpty drop every key that has no character code — the F-keys and arrows
// a raw-mode TUI lives on. The gap was found by the Windows Terminal
// verification lane: rune keys and Ctrl+Q arrived while F1 vanished without
// a byte. MakeRaw's saved state must still restore the original mode,
// because the suspend/resume dance relies on that single Restore.
func TestMakeRawTermEnablesVTInput(t *testing.T) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no console to query: %v", err)
	}
	defer func() { _ = input.Close() }()
	fd := int(input.Fd())

	var before uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &before); err != nil {
		t.Skipf("no console mode: %v", err)
	}

	state, err := makeRawTerm(fd)
	if err != nil {
		t.Fatalf("makeRawTerm failed: %v", err)
	}
	var raw uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &raw); err != nil {
		t.Fatalf("GetConsoleMode after makeRawTerm: %v", err)
	}
	if raw&enableVirtualTerminalInput == 0 {
		t.Fatalf("raw input mode %#x lacks ENABLE_VIRTUAL_TERMINAL_INPUT", raw)
	}
	if raw&windows.ENABLE_LINE_INPUT != 0 {
		t.Fatalf("raw input mode %#x keeps line input", raw)
	}
	if raw&windows.ENABLE_ECHO_INPUT != 0 {
		t.Fatalf("raw input mode %#x keeps echo", raw)
	}

	if err := term.Restore(fd, state); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	var after uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &after); err != nil {
		t.Fatalf("GetConsoleMode after Restore: %v", err)
	}
	if after != before {
		t.Fatalf("Restore left mode %#x, want the original %#x", after, before)
	}
}

// TestMakeRawTermRestoresAfterRoundTrip guards the cooked window the suspend
// handshake opens: suspend restores the mode MakeRaw saved, and a resume
// re-enters raw through makeRawTerm again. The second entry must find the
// console in cooked mode and still end with the VT-input bit set, or a
// suspend/resume cycle would silently degrade function keys.
func TestMakeRawTermRestoresAfterRoundTrip(t *testing.T) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no console to query: %v", err)
	}
	defer func() { _ = input.Close() }()
	fd := int(input.Fd())

	first, err := makeRawTerm(fd)
	if err != nil {
		t.Fatalf("first makeRawTerm failed: %v", err)
	}
	if err := term.Restore(fd, first); err != nil {
		t.Fatalf("first Restore failed: %v", err)
	}
	second, err := makeRawTerm(fd)
	if err != nil {
		t.Fatalf("second makeRawTerm failed: %v", err)
	}
	defer func() { _ = term.Restore(fd, second) }()
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		t.Fatalf("GetConsoleMode after the second entry: %v", err)
	}
	if mode&enableVirtualTerminalInput == 0 {
		t.Fatalf("mode %#x after re-entry lacks ENABLE_VIRTUAL_TERMINAL_INPUT", mode)
	}
}

// readInput must serve two handle kinds whose readiness semantics differ:
// console input handles (the documented wait object) and the anonymous
// pipes of os.Pipe (whose WaitForSingleObject behavior is kernel-flavor
// dependent — never signaled on client Windows, always signaled when idle
// on server releases). The pipe contracts below hold on every flavor:
// bytes are delivered, deadlines are honored, cancellation returns, and a
// closed write end surfaces as EOF instead of an endless poll.

// The Win32 surrogate contract below was measured, not assumed: a probe
// injected U+1D11E as the two KEY_EVENT halves (D834, DD1E) through
// WriteConsoleInputW and read the console back in both raw modes. Both
// delivered exactly f0 9d 84 9e — the console driver pairs the surrogate
// halves and encodes the rune as UTF-8, so the byte stream the parser
// decodes never carries raw UTF-16. A dangling high surrogate delivered
// nothing at all: the console holds it pending until a low surrogate
// completes the pair, which jams later input behind it. That jam belongs
// to the console, not to the framework; the fixture only pins what the
// framework actually observes.

var (
	procWriteConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("WriteConsoleInputW")
	procFlushInput        = windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer")
)

// winInputRecord mirrors INPUT_RECORD; only the KEY_EVENT arm is used.
type winInputRecord struct {
	eventType uint16
	_         uint16
	event     [16]byte
}

// winKeyEventRecord mirrors KEY_EVENT_RECORD, the union payload above.
type winKeyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

func winKeyRecord(ch uint16, down bool) winInputRecord {
	var rec winInputRecord
	rec.eventType = 1 // KEY_EVENT_RECORD
	ke := (*winKeyEventRecord)(unsafe.Pointer(&rec.event))
	ke.keyDown = 0
	if down {
		ke.keyDown = 1
	}
	ke.repeatCount = 1
	ke.unicodeChar = ch
	return rec
}

func injectWinKeys(t *testing.T, handle windows.Handle, chars []uint16) {
	t.Helper()
	recs := make([]winInputRecord, 0, len(chars)*2)
	for _, ch := range chars {
		recs = append(recs, winKeyRecord(ch, true), winKeyRecord(ch, false))
	}
	var written uint32
	r, _, errno := procWriteConsoleInput.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&recs[0])),
		uintptr(len(recs)),
		uintptr(unsafe.Pointer(&written)),
	)
	if r == 0 {
		t.Fatalf("WriteConsoleInputW failed: %v", errno)
	}
}

// readConsoleWithin reads with a hard deadline so a jammed console queue
// fails the test instead of hanging the lane.
func readConsoleWithin(t *testing.T, file *os.File, wait time.Duration) []byte {
	t.Helper()
	type res struct {
		n   int
		err error
	}
	buf := make([]byte, 64)
	ch := make(chan res, 1)
	go func() { n, err := file.Read(buf); ch <- res{n, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("console read: %v", r.err)
		}
		return buf[:r.n]
	case <-time.After(wait):
		t.Fatalf("console read blocked for %s: input queue jammed", wait)
		return nil
	}
}

// TestConsoleSurrogatePairArrivesAsCombinedUTF8 pins the pairing guarantee
// the parser's non-BMP fixtures rely on: inject U+1D11E's surrogate halves
// as separate KEY_EVENT records and the console must hand ReadFile the
// combined UTF-8 bytes, in the legacy raw mode and under
// ENABLE_VIRTUAL_TERMINAL_INPUT alike.
func TestConsoleSurrogatePairArrivesAsCombinedUTF8(t *testing.T) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no console to drive: %v", err)
	}
	defer func() { _ = input.Close() }()
	handle := windows.Handle(input.Fd())

	var saved uint32
	if err := windows.GetConsoleMode(handle, &saved); err != nil {
		t.Skipf("no console mode: %v", err)
	}
	defer func() { _ = windows.SetConsoleMode(handle, saved) }()

	for _, tc := range []struct {
		name string
		vt   bool
	}{
		{"legacy raw", false},
		{"vt input", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := saved &^ (windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT)
			if tc.vt {
				mode |= enableVirtualTerminalInput
			} else {
				mode &^= enableVirtualTerminalInput
			}
			if err := windows.SetConsoleMode(handle, mode); err != nil {
				t.Skipf("console rejected mode: %v", err)
			}
			if r, _, errno := procFlushInput.Call(uintptr(handle)); r == 0 {
				t.Fatalf("FlushConsoleInputBuffer failed: %v", errno)
			}
			injectWinKeys(t, handle, []uint16{0xD834, 0xDD1E})
			got := readConsoleWithin(t, input, 2*time.Second)
			want := []byte("\U0001D11E")
			if !bytes.Equal(got, want) {
				t.Fatalf("console delivered % x, want the combined UTF-8 rune % x", got, want)
			}
		})
	}
}

func TestReadInputReadsBytesFromPipeInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	if _, err := writer.Write([]byte("AB")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	count, err := readInput(context.Background(), reader, buffer, time.Second)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if count != 2 || string(buffer[:count]) != "AB" {
		t.Fatalf("readInput read %d bytes %q, want 2 bytes \"AB\"", count, buffer[:count])
	}
}

func TestReadInputWakesForBytesThatArriveLate(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	go func() {
		time.Sleep(100 * time.Millisecond)
		if _, err := writer.Write([]byte("xy")); err != nil {
			t.Errorf("write: %v", err)
		}
	}()
	buffer := make([]byte, 8)
	count, err := readInput(context.Background(), reader, buffer, 2*time.Second)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if count != 2 || string(buffer[:count]) != "xy" {
		t.Fatalf("readInput read %d bytes %q, want 2 bytes \"xy\"", count, buffer[:count])
	}
}

func TestReadInputReturnsEOFWhenPipeWriterCloses(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	count, err := readInput(context.Background(), reader, buffer, time.Second)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("readInput error = %v, want io.EOF", err)
	}
	if count != 0 {
		t.Fatalf("readInput count = %d, want 0", count)
	}
}

func TestReadInputHonorsContextCancellationOnIdlePipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	buffer := make([]byte, 8)
	start := time.Now()
	count, err := readInput(ctx, reader, buffer, 10*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readInput error = %v, want context.Canceled", err)
	}
	if count != 0 {
		t.Fatalf("readInput count = %d, want 0", count)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("readInput took %v to notice cancellation", elapsed)
	}
}

func TestReadInputWaitsUntilDeadlineOnIdlePipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	buffer := make([]byte, 8)
	start := time.Now()
	count, err := readInput(context.Background(), reader, buffer, 300*time.Millisecond)
	if !errors.Is(err, errReadTimeout) {
		t.Fatalf("readInput error = %v, want errReadTimeout", err)
	}
	if count != 0 {
		t.Fatalf("readInput count = %d, want 0", count)
	}
	if elapsed := time.Since(start); elapsed < 280*time.Millisecond {
		t.Fatalf("readInput gave up after %v, before the 300ms deadline", elapsed)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("readInput took %v to reach its deadline", elapsed)
	}
}
