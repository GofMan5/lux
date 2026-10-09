package terminal

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

// TestTitleSanitization pins what a title may carry: graphic runes from any
// script pass through untouched, while C0 controls, DEL, and C1 controls are
// stripped — a BEL or ESC left inside would end the OSC 0 string early and
// leak the rest of the title as commands.
func TestTitleSanitization(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{name: "plain ascii", in: "App", want: "App"},
		{name: "unicode scripts survive", in: "lux — Ж猫 🦀", want: "lux — Ж猫 🦀"},
		{name: "bell and escape are control injection", in: "\x07bad\x1bell", want: "badell"},
		{name: "other C0 stripped", in: "tab\tnew\nline\r", want: "tabnewline"},
		{name: "DEL stripped", in: "del\x7f", want: "del"},
		{name: "C1 runes stripped", in: "nel\u0085end", want: "nelend"},
		{name: "all controls", in: "\x01\x1f\x7f\u009c", want: ""},
		{name: "empty", in: "", want: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitizeTitle(test.in); got != test.want {
				t.Fatalf("sanitizeTitle(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestTitleOptionSanitizesAtConstruction pins that the Option sanitizes the
// moment it is applied, so the field the lifecycle reads can never hold a
// control sequence, whatever route set it.
func TestTitleOptionSanitizesAtConstruction(t *testing.T) {
	backend := New(nil, nil, Title("\x07injected\x1b]52"))
	if backend.title != "injected]52" {
		t.Fatalf("title = %q, want the controls stripped", backend.title)
	}
	backend = New(nil, nil)
	if backend.title != "" {
		t.Fatalf("default title = %q, want empty", backend.title)
	}
}

// TestSequencesTitle pins the assembly the title rides: the push and set
// lead the enter before every mode, the pop closes the leave after every
// restore, and a backend with no title writes neither. The combined rows
// pin the same ordering discipline the kitty rows of the table already
// follow.
func TestSequencesTitle(t *testing.T) {
	cases := []struct {
		name                             string
		pixel, kitty, win32, unicodeCore bool
		title                            string
		progress                         bool
		wantEnter, wantLeave             string
	}{
		{
			name:      "no title writes nothing",
			wantEnter: enterTerminal,
			wantLeave: leaveTerminal,
		},
		{
			name:      "title leads the enter and closes the leave",
			title:     "App",
			wantEnter: titlePush + setWindowTitle("App") + enterTerminal,
			wantLeave: leaveTerminal + titlePop,
		},
		{
			name:      "title pairs with kitty",
			title:     "App",
			kitty:     true,
			wantEnter: titlePush + setWindowTitle("App") + enterTerminal + kittyEnter,
			wantLeave: kittyLeave + leaveTerminal + titlePop,
		},
		{
			name:      "title pairs with the pixel mouse",
			title:     "App",
			pixel:     true,
			wantEnter: titlePush + setWindowTitle("App") + enterPixel,
			wantLeave: leavePixel + titlePop,
		},
		{
			name:        "title closes behind every restore",
			title:       "App",
			kitty:       true,
			win32:       true,
			unicodeCore: true,
			wantEnter:   titlePush + setWindowTitle("App") + enterTerminal + kittyEnter + win32Enter + unicodeCoreEnter,
			wantLeave:   unicodeCoreLeave + win32Leave + kittyLeave + leaveTerminal + titlePop,
		},
		{
			name:      "progress reset closes behind the title pop",
			title:     "App",
			progress:  true,
			wantEnter: titlePush + setWindowTitle("App") + enterTerminal,
			wantLeave: leaveTerminal + titlePop + progressClear,
		},
		{
			name:      "progress without a title writes the reset alone",
			progress:  true,
			wantEnter: enterTerminal,
			wantLeave: leaveTerminal + progressClear,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			enter, leave := sequences(test.pixel, test.kitty, test.win32, test.unicodeCore, test.title, test.progress)
			if enter != test.wantEnter {
				t.Fatalf("enter = %q, want %q", enter, test.wantEnter)
			}
			if leave != test.wantLeave {
				t.Fatalf("leave = %q, want %q", leave, test.wantLeave)
			}
		})
	}
}

// TestCloseRestoresWindowTitle pins the pop half of the title deal, the same
// contract the kitty stack follows: a backend that pushed a title pops it in
// the same Write as the legacy restore, after every mode reset, and a
// backend with no title never emits a stray pop.
func TestCloseRestoresWindowTitle(t *testing.T) {
	t.Run("latched backend pops after every restore", func(t *testing.T) {
		output, err := os.CreateTemp(t.TempDir(), "terminal-output")
		if err != nil {
			t.Fatal(err)
		}
		backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}, kitty: true, title: "App"}
		frame := render.NewBuffer(2, 1)
		render.NewCanvas(frame).Text(0, 0, "ok", render.Style{})
		if err := backend.Present(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
		if err := backend.Close(); err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasSuffix(written, []byte(kittyLeave+leaveTerminal+titlePop)) {
			t.Fatalf("Close did not pop the title last: %q", written)
		}
		if bytes.Count(written, []byte(titlePop)) != 1 {
			t.Fatalf("the title pop appears more than once: %q", written)
		}
	})
	t.Run("backend without a title never writes title bytes", func(t *testing.T) {
		output, err := os.CreateTemp(t.TempDir(), "terminal-output")
		if err != nil {
			t.Fatal(err)
		}
		backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
		frame := render.NewBuffer(2, 1)
		render.NewCanvas(frame).Text(0, 0, "ok", render.Style{})
		if err := backend.Present(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
		if err := backend.Close(); err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, sequence := range []string{titlePop, titlePush, "\x1b]0;"} {
			if bytes.Contains(written, []byte(sequence)) {
				t.Fatalf("Close emitted title bytes %q for a backend without a title: %q", sequence, written)
			}
		}
	})
}

// TestPlainBackendWithTitleNeverWrites pins the plain path: a redirected
// output stream never carries title bytes — not at Open, not in a frame,
// not at Close. The title program, like every other mode, is exclusive to
// an interactive terminal.
func TestPlainBackendWithTitleNeverWrites(t *testing.T) {
	t.Setenv("TERM", "dumb")
	output, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	backend := New(nil, output, Title("App"))
	if _, err := backend.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame := render.NewBuffer(2, 1)
	render.NewCanvas(frame).Text(0, 0, "ok", render.Style{})
	if err := backend.Present(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "ok\n" {
		t.Fatalf("plain output = %q, want exactly the frame", written)
	}
	if strings.Contains(string(written), "\x1b") {
		t.Fatalf("plain output carries escape sequences: %q", written)
	}
}
