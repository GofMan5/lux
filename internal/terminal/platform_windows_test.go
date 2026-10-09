//go:build windows

package terminal

import (
	"os"
	"testing"

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
