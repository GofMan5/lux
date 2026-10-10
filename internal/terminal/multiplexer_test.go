package terminal

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/GofMan5/lux/event"
)

// TestDetectMultiplexer pins the witness matrix: environment variables, the
// propagated $TERM, and the tmux fingerprints in the recorded device
// reports. Every row is hermetic — the three environment variables are
// cleared first, then the row installs exactly its own signals.
func TestDetectMultiplexer(t *testing.T) {
	tests := []struct {
		name string
		tmux string
		sty  string
		term string
		da1  string
		da2  string
		want muxKind
	}{
		{"no witnesses", "", "", "xterm-256color", "62;1;2;6;9;15;16;17;18;21;22;27;28;29", "1;0;0", muxNone},
		{"tmux environment", "/tmp/tmux-0/default,123,0", "", "xterm-256color", "", "", muxTmux},
		{"screen environment", "", "12345.pts-0.host", "xterm", "", "", muxScreen},
		{"tmux TERM", "", "", "tmux-256color", "", "", muxTmux},
		{"tmux direct TERM", "", "", "tmux-direct", "", "", muxTmux},
		{"screen TERM", "", "", "screen.xterm-256color", "", "", muxScreen},
		{"screen linux TERM", "", "", "screen.linux", "", "", muxScreen},
		{"st is not screen", "", "", "st-256color", "", "", muxNone},
		{"DA2 manufacturer code 84", "", "", "xterm-256color", "", "84;0;0", muxTmux},
		{"DA1 tmux shape without sixel", "", "", "xterm-256color", "1;2", "", muxTmux},
		{"DA1 tmux shape with sixel", "", "", "xterm-256color", "1;2;4", "", muxTmux},
		{"DA2 from Windows Terminal", "", "", "xterm-256color", "61;4;6;7;14;21;22;23;24;28;32;42;52", "0;10;1", muxNone},
		{"DA2 from xterm", "", "", "xterm-256color", "62;1;2", "41;0;0", muxNone},
		{"DA2 from VTE", "", "", "xterm-256color", "62;1;2", "65;0;0", muxNone},
		// The screen DA2 code is deliberately not a witness: pane-terminal
		// DA2 values have drifted between builds, so screen stays
		// environment-detected only.
		{"DA2 code 83 is not a witness", "", "", "xterm-256color", "62;1;2", "83;0;0", muxNone},
		{"junk DA2 first parameter is not a witness", "", "", "xterm-256color", "62;1;2", ";0;0", muxNone},
		{"empty DA2 first parameter is not a witness", "", "", "xterm-256color", "62;1;2", "", muxNone},
		// The ssh case: a local tmux pane with ssh into a remote box leaves
		// the remote environment without $TMUX, but the propagated $TERM and
		// the reports still land from the local tmux.
		{"TERM with no reports over ssh", "", "", "tmux-256color", "", "", muxTmux},
		{"reports with no TERM over ssh", "", "", "xterm-256color", "1;2", "84;0;0", muxTmux},
		// tmux nested inside screen: the inner tmux is the pane's answerer,
		// so its witness wins.
		{"tmux inside screen", "/tmp/tmux-0/default,123,0", "12345.pts-0.host", "screen", "", "", muxTmux},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMUX", tt.tmux)
			t.Setenv("STY", tt.sty)
			t.Setenv("TERM", tt.term)
			got := detectMultiplexer(probeReply{da1: tt.da1, da2: tt.da2})
			if got != tt.want {
				t.Errorf("detectMultiplexer() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGateMultiplexerInput pins the provenance rule itself: inside a
// multiplexer the input-family verdicts are downgraded to unknown while the
// output family keeps the multiplexer's own honest answers — mode 2027
// included, because the clustering contract rides the output path — and
// without a multiplexer every verdict passes through untouched.
func TestGateMultiplexerInput(t *testing.T) {
	hostile := probeReply{
		answered:    true,
		da1:         "1;2",
		da2:         "84;0;0",
		scroll:      probeSupported,
		sync:        probeSupported,
		keyboard:    probeSupported,
		pixel:       probeSupported,
		win32:       probeSupported,
		unicodeCore: probeSupported,
	}
	// Neutralize the environment so rows decide purely through the reports.
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	t.Setenv("TERM", "xterm-256color")

	t.Run("tmux reports gate the input family", func(t *testing.T) {
		want := probeReply{
			answered:    true,
			da1:         "1;2",
			da2:         "84;0;0",
			scroll:      probeSupported,
			sync:        probeSupported,
			keyboard:    probeUnknown,
			pixel:       probeUnknown,
			win32:       probeUnknown,
			unicodeCore: probeSupported,
		}
		if got := gateMultiplexerInput(hostile); got != want {
			t.Errorf("gated reply = %+v, want %+v", got, want)
		}
	})
	t.Run("screen environment gates the input family", func(t *testing.T) {
		t.Setenv("STY", "12345.pts-0.host")
		hostile.da2 = "" // screen is detected purely through the environment
		want := probeReply{
			answered:    true,
			da1:         "1;2",
			scroll:      probeSupported,
			sync:        probeSupported,
			keyboard:    probeUnknown,
			pixel:       probeUnknown,
			win32:       probeUnknown,
			unicodeCore: probeSupported,
		}
		if got := gateMultiplexerInput(hostile); got != want {
			t.Errorf("gated reply = %+v, want %+v", got, want)
		}
	})
	t.Run("host terminal passes through untouched", func(t *testing.T) {
		honest := probeReply{
			answered:    true,
			da1:         "62;1;2",
			da2:         "1;0;0",
			scroll:      probeSupported,
			sync:        probeSupported,
			keyboard:    probeSupported,
			pixel:       probeSupported,
			win32:       probeSupported,
			unicodeCore: probeSupported,
		}
		if got := gateMultiplexerInput(honest); got != honest {
			t.Errorf("gated reply = %+v, want the input unchanged", got)
		}
	})
	t.Run("rejections and silence stay untouched", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-0/default,123,0")
		rejected := probeReply{
			answered: true,
			da1:      "1;2",
			scroll:   probeSupported,
			sync:     probeRejected,
			keyboard: probeUnknown,
			pixel:    probeRejected,
			win32:    probeRejected,
		}
		// Downgrading unknown to unknown and rejected to unknown would be
		// harmless for the latches, but the gate must not rewrite verdicts
		// it has no reason to touch: the rejections are honest output-path
		// answers and the unknowns are the legacy default already.
		if got := gateMultiplexerInput(rejected); got != rejected {
			t.Errorf("gated reply = %+v, want the input unchanged", got)
		}
	})
}

// TestTmuxKeepsLegacyInput is the stage-6 negative test from the roadmap:
// a pane whose multiplexer answers — or even lies about — every input
// protocol must still end up with live legacy input. The hostile stream is
// tmux's exact report set plus a forged kitty flags answer, the way a
// mis-routed passthrough reply would look; the chain parse → gate →
// apply must land on legacy decoding, and the legacy bytes tmux actually
// re-emits must decode.
func TestTmuxKeepsLegacyInput(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,123,0")
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("COLORTERM", "")

	tmuxAnswers := "\x1b[?1;2c\x1b[>84;0;0c\x1b[?6;2$y\x1b[?2026;2$y\x1b[?1016;0$y\x1b[?9001;0$y"
	reply, leftover := parseProbeReply([]byte(tmuxAnswers + "\x1b[?1u"))
	if string(leftover) != "" {
		t.Fatalf("leftover = %q, want none", leftover)
	}
	// The raw reply is the lie: everything answered, kitty included.
	rawWant := probeReply{
		answered: true,
		da1:      "1;2",
		da2:      "84;0;0",
		scroll:   probeSupported,
		sync:     probeSupported,
		pixel:    probeRejected,
		win32:    probeRejected,
		keyboard: probeSupported,
	}
	if reply != rawWant {
		t.Fatalf("raw reply = %+v, want %+v", reply, rawWant)
	}

	gated := gateMultiplexerInput(reply)
	features := applyProbeFeatures(detectCapabilities(false), gated, syncOutputAuto)
	if features.KeyboardEnhanced {
		t.Error("kitty input enabled inside tmux without confirmed passthrough")
	}
	if features.PixelMouse {
		t.Error("pixel mouse enabled inside tmux")
	}
	if features.Win32Input {
		t.Error("win32-input-mode enabled inside tmux")
	}
	// The output family keeps tmux's own honest answers: it implements
	// DECSTBM and 2026 for its panes itself.
	if !features.ScrollRegions {
		t.Error("scroll regions vetoed though the pane path supports them")
	}
	if !features.SynchronizedOutput {
		t.Error("synchronized output off though the pane path supports it")
	}

	// Input alive: exactly the legacy shapes tmux re-emits decode through
	// the parser Open configures for this verdict set.
	got := newParser().feed([]byte("a\x1b[Z\x1b[15~\x01"), false)
	// The C0 byte spells the letter it controls in uppercase —
	// parseRune's rune(byte+'@') convention — and applications match
	// with ToLower.
	want := []event.Event{
		event.RuneKey('a', 0),
		event.KeyPress(event.KeyBackTab, 0),
		event.KeyPress(event.KeyF5, 0),
		event.RuneKey('A', event.ModCtrl),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy stream = %#v, want %#v", got, want)
	}
}

// TestTmuxKeepsUnicodeCoreClaim is the output-family counterpart of the
// stage-6 negative test: a mode 2027 report inside a tmux pane is not an
// input lie the gate must strip. The terminal-unicode-core contract rides
// the output path, so the multiplexer's own answer stays authoritative —
// today's tmux answers status 0 for unimplemented modes and the mode stays
// off; if a pane ever answers supported, the claim belongs to the pane path
// the same way DECSTBM and 2026 do. The chain parse → gate → apply must
// preserve the verdict, and a forged supported answer must land in the
// capability set the same way tmux's honest 2026 does.
func TestTmuxKeepsUnicodeCoreClaim(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,123,0")
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("COLORTERM", "")

	// The forged lie: tmux does not implement 2027 today, so a supported
	// status is exactly what a mis-routed passthrough reply would look
	// like. The gate must not rewrite it either way.
	tmuxAnswers := "\x1b[?1;2c\x1b[>84;0;0c\x1b[?2026;2$y\x1b[?2027;2$y"
	reply, leftover := parseProbeReply([]byte(tmuxAnswers))
	if string(leftover) != "" {
		t.Fatalf("leftover = %q, want none", leftover)
	}
	if reply.unicodeCore != probeSupported {
		t.Fatalf("raw 2027 verdict = %v, want probeSupported", reply.unicodeCore)
	}

	gated := gateMultiplexerInput(reply)
	if gated.unicodeCore != probeSupported {
		t.Fatalf("gated 2027 verdict = %v, want the mux answer untouched", gated.unicodeCore)
	}
	features := applyProbeFeatures(detectCapabilities(false), gated, syncOutputAuto)
	if !features.UnicodeCoreMode {
		t.Error("unicode core mode vetoed though the pane path answered for it")
	}
}

// TestNegotiateTmuxStream runs the same hostile answer set through the real
// negotiate loop on a pipe: the probe request itself now carries the DA2
// query, tmux's report order arrives behind it, and the keystrokes that
// follow the reports survive as leftover, exactly as Open would replay
// them into the read loop.
func TestNegotiateTmuxStream(t *testing.T) {
	reader, writer := negotiatePipe(t)
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("\x1b[?1;2c\x1b[>84;0;0c\x1b[?6;2$y\x1b[?2026;2$y\x1b[?1016;0$y\x1b[?9001;0$y\x1b[?1u" +
			"a\x1b[Z\x1b[15~"))
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		done <- err
	}()
	probe := &bytes.Buffer{}
	reply, leftover := negotiate(context.Background(), reader, probe, 400*time.Millisecond)
	if err := <-done; err != nil {
		t.Errorf("reply writer: %v", err)
	}
	if got := probe.String(); got != probeRequest {
		t.Errorf("probe request = %q, want %q", got, probeRequest)
	}
	want := probeReply{
		answered: true,
		da1:      "1;2",
		da2:      "84;0;0",
		scroll:   probeSupported,
		sync:     probeSupported,
		pixel:    probeRejected,
		win32:    probeRejected,
		keyboard: probeSupported,
	}
	if reply != want {
		t.Errorf("reply = %+v, want %+v", reply, want)
	}
	// The keystrokes behind the reports are the input the pane actually
	// receives, and they must arrive in the read loop untouched.
	if string(leftover) != "a\x1b[Z\x1b[15~" {
		t.Errorf("leftover = %q, want the legacy keystrokes", leftover)
	}
}
