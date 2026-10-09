package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/lux-tui/lux/capability"
)

// TestProgressAnnexBytes pins the wire form of OSC 9;4: the none state
// carries no percent field, every other state does — Windows Terminal
// ignores the value it does not need while ConEmu expects the field — the
// percent is clamped into 0..100, and the BEL terminator is the one the
// Windows Terminal and ConEmu lineages document. A state outside the
// table encodes to no bytes: a malformed sequence would be worse than
// silence.
func TestProgressAnnexBytes(t *testing.T) {
	cases := []struct {
		name    string
		state   int
		percent int
		want    string
	}{
		{name: "none carries no percent", state: 0, percent: 50, want: "\x1b]9;4;0\a"},
		{name: "indeterminate", state: 1, percent: 40, want: "\x1b]9;4;1;40\a"},
		{name: "normal", state: 2, percent: 63, want: "\x1b]9;4;2;63\a"},
		{name: "error keeps the bar", state: 3, percent: 63, want: "\x1b]9;4;3;63\a"},
		{name: "paused", state: 4, percent: 63, want: "\x1b]9;4;4;63\a"},
		{name: "percent clamps low", state: 2, percent: -5, want: "\x1b]9;4;2;0\a"},
		{name: "percent clamps high", state: 2, percent: 400, want: "\x1b]9;4;2;100\a"},
		{name: "percent zero is honest", state: 2, percent: 0, want: "\x1b]9;4;2;0\a"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := string(ProgressAnnex(test.state, test.percent)); got != test.want {
				t.Fatalf("ProgressAnnex(%d, %d) = %q, want %q", test.state, test.percent, got, test.want)
			}
		})
	}
	t.Run("state outside the table returns no bytes", func(t *testing.T) {
		for _, state := range []int{-1, 5, 42} {
			if got := ProgressAnnex(state, 50); got != nil {
				t.Fatalf("ProgressAnnex(%d) = %q, want nil", state, got)
			}
		}
	})
}

// TestNotifyAnnexSanitization pins what a toast may carry: control runes
// are stripped from both fields (a BEL or ESC inside an OSC string ends
// it early and leaks the rest as commands), semicolons are stripped from
// the title (OSC 9;777's field separator — one in the title would slide
// the body into the title slot) but pass through the body, and a toast
// empty in both fields encodes to no bytes. The terminator is ST, the
// form the rxvt-lineage terminals that define 9;777 accept.
func TestNotifyAnnexSanitization(t *testing.T) {
	cases := []struct {
		name        string
		title, body string
		want        string
	}{
		{name: "plain toast", title: "Build", body: "it passed", want: "\x1b]777;notify;Build;it passed\x1b\\"},
		{name: "control injection stripped from both fields", title: "a\x07b\x1bc", body: "d\x7fe\u0085f", want: "\x1b]777;notify;abc;def\x1b\\"},
		{name: "semicolons leave the title and stay in the body", title: "one;two", body: "three;four", want: "\x1b]777;notify;onetwo;three;four\x1b\\"},
		{name: "unicode survives", title: "Ж猫", body: "— done 🦀", want: "\x1b]777;notify;Ж猫;— done 🦀\x1b\\"},
		{name: "empty title keeps the field", title: "", body: "body only", want: "\x1b]777;notify;;body only\x1b\\"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := string(NotifyAnnex(test.title, test.body)); got != test.want {
				t.Fatalf("NotifyAnnex(%q, %q) = %q, want %q", test.title, test.body, got, test.want)
			}
		})
	}
	t.Run("both fields empty returns no bytes", func(t *testing.T) {
		if got := NotifyAnnex("", ""); got != nil {
			t.Fatalf("NotifyAnnex(empty) = %q, want nil", got)
		}
	})
	t.Run("controls alone are not a toast", func(t *testing.T) {
		if got := NotifyAnnex("\x07\x1b", "\x7f\u009c"); got != nil {
			t.Fatalf("NotifyAnnex(controls only) = %q, want nil", got)
		}
	})
}

// TestClipboardAnnexBytes pins the wire form of OSC 52: standard base64
// (the alphabet cannot carry BEL, ESC, or semicolon, so no sanitization
// is needed), the c system clipboard selection, and the ST terminator.
// The read direction — a ? payload that makes the terminal type the
// user's clipboard into the program's stdin — is never built: the
// package only encodes writes. Payloads past 32 KiB return no bytes
// rather than a truncated clipboard.
func TestClipboardAnnexBytes(t *testing.T) {
	t.Run("text encodes to base64", func(t *testing.T) {
		want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello clipboard")) + "\x1b\\"
		if got := string(ClipboardAnnex("hello clipboard")); got != want {
			t.Fatalf("ClipboardAnnex = %q, want %q", got, want)
		}
	})
	t.Run("empty text is a clear, not a read", func(t *testing.T) {
		if got := string(ClipboardAnnex("")); got != "\x1b]52;c;\x1b\\" {
			t.Fatalf("ClipboardAnnex(empty) = %q, want a bare clear", got)
		}
	})
	t.Run("the read direction never exists", func(t *testing.T) {
		for _, text := range []string{"?", "???", "?payload"} {
			got := string(ClipboardAnnex(text))
			if got == "\x1b]52;c;?\x1b\\" {
				t.Fatalf("ClipboardAnnex(%q) built a read request", text)
			}
		}
	})
	t.Run("oversized text returns no bytes", func(t *testing.T) {
		if got := ClipboardAnnex(strings.Repeat("x", maxClipboardAnnex+1)); got != nil {
			t.Fatalf("oversized write returned %d bytes, want nil", len(got))
		}
		if got := ClipboardAnnex(strings.Repeat("x", maxClipboardAnnex)); got == nil {
			t.Fatal("the cap itself must still write")
		}
	})
}

// TestWriteAnnexDiscipline pins the guards on the annex write: it writes
// on an open interactive backend, and it returns without writing — and
// without an error — on plain, suspended, and closed backends, the same
// refusal surface Present has. Empty data is a no-op short-circuit so the
// runtime's gated frames never pay for a slot round trip.
func TestWriteAnnexDiscipline(t *testing.T) {
	annex := ProgressAnnex(2, 50)
	output, err := osCreateTemp(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	t.Run("open backend writes the annex", func(t *testing.T) {
		backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
		if err := backend.WriteAnnex(context.Background(), annex); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(writtenBytes(t, output), annex) {
			t.Fatalf("the annex was not written: %q", writtenBytes(t, output))
		}
	})
	t.Run("plain backend refuses without an error", func(t *testing.T) {
		plain := &Backend{opened: true, plain: true, output: output, writer: output}
		if err := plain.WriteAnnex(context.Background(), annex); err != nil {
			t.Fatalf("plain WriteAnnex must be a silent no-op: %v", err)
		}
		if bytes.Count(writtenBytes(t, output), annex) != 1 {
			t.Fatalf("the plain backend wrote an annex: %q", writtenBytes(t, output))
		}
	})
	t.Run("suspended backend drops the annex", func(t *testing.T) {
		suspended := &Backend{opened: true, suspended: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
		if err := suspended.WriteAnnex(context.Background(), annex); err != nil {
			t.Fatalf("suspended WriteAnnex must be a silent no-op: %v", err)
		}
		if bytes.Count(writtenBytes(t, output), annex) != 1 {
			t.Fatalf("the suspended backend wrote an annex: %q", writtenBytes(t, output))
		}
	})
	t.Run("closed backend drops the annex", func(t *testing.T) {
		closed := &Backend{output: output, writer: output}
		if err := closed.WriteAnnex(context.Background(), annex); err != nil {
			t.Fatalf("closed WriteAnnex must be a silent no-op: %v", err)
		}
		if bytes.Count(writtenBytes(t, output), annex) != 1 {
			t.Fatalf("the closed backend wrote an annex: %q", writtenBytes(t, output))
		}
	})
	t.Run("empty data never takes the slot", func(t *testing.T) {
		backend := &Backend{opened: true, output: output, writer: output}
		if err := backend.WriteAnnex(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if bytes.Count(writtenBytes(t, output), annex) != 1 {
			t.Fatalf("an empty annex wrote bytes: %q", writtenBytes(t, output))
		}
	})
}

// TestWriteAnnexHonorsContext pins that a write the caller cancels while
// the slot is held surfaces the cancellation instead of blocking on a
// busy terminal — the same contract Present carries.
func TestWriteAnnexHonorsContext(t *testing.T) {
	output, err := osCreateTemp(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	backend := &Backend{opened: true, output: output, writer: output}
	slot := backend.writeSlotChan()
	slot <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backend.WriteAnnex(ctx, ProgressAnnex(1, 0)); err == nil {
		t.Fatal("a canceled annex write must surface the cancellation")
	}
	<-slot
}

// TestCloseResetsProgress pins the leave-side half of the Progress option:
// a backend that opted in writes the OSC 9;4 reset in the same write as
// the legacy restore — the taskbar bar does not outlive the program —
// and a backend without the option never emits progress bytes on any
// path, so default builds stay byte-identical.
func TestCloseResetsProgress(t *testing.T) {
	t.Run("progress backend resets on close", func(t *testing.T) {
		output, err := osCreateTemp(t)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = output.Close() }()
		backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}, kitty: true, progress: true}
		if err := backend.Present(context.Background(), testFrame("ok")); err != nil {
			t.Fatal(err)
		}
		if err := backend.Close(); err != nil {
			t.Fatal(err)
		}
		written := writtenBytes(t, output)
		if !bytes.HasSuffix(written, []byte(kittyLeave+leaveTerminal+progressClear)) {
			t.Fatalf("Close did not reset progress last: %q", written)
		}
	})
	t.Run("progress write itself does not write a reset", func(t *testing.T) {
		output, err := osCreateTemp(t)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = output.Close() }()
		backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
		if err := backend.WriteAnnex(context.Background(), ProgressAnnex(2, 50)); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(writtenBytes(t, output), []byte(progressClear)) {
			t.Fatalf("a progress write produced a reset: %q", writtenBytes(t, output))
		}
	})
}

// TestSuspendResetsProgress pins that the suspend leave carries the reset
// too: the taskbar bar belongs to the running program, and while the
// program is stopped the shell owns the terminal. The next frame after
// resume re-reports whatever the model still considers true.
func TestSuspendResetsProgress(t *testing.T) {
	stubMakeRaw(t)
	backend, output := newSuspendBackend(t)
	backend.progress = true
	if !backend.suspendTerminal() {
		t.Fatal("suspend refused")
	}
	written := writtenBytes(t, output)
	if !bytes.HasSuffix(written, []byte(leaveTerminal+progressClear)) {
		t.Fatalf("suspend did not reset progress: %q", written)
	}
}

// TestProgressOptionLatches pins that the Option arms the latch exactly
// once and that it survives Close like the title — a second Open restores
// the same choice.
func TestProgressOptionLatches(t *testing.T) {
	backend := New(nil, nil, Progress(true))
	if !backend.progress {
		t.Fatal("Progress(true) did not latch")
	}
	backend = New(nil, nil, Progress(false))
	if backend.progress {
		t.Fatal("Progress(false) latched progress")
	}
	backend = New(nil, nil)
	if backend.progress {
		t.Fatal("progress must default to off")
	}
}

// osCreateTemp is os.CreateTemp spelled once so the annex tests stay
// focused on the discipline under test.
func osCreateTemp(t *testing.T) (*os.File, error) {
	t.Helper()
	return os.CreateTemp(t.TempDir(), "terminal-output")
}
