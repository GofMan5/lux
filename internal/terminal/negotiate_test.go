package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
)

// Tests for the capability negotiation probe: the reply parser, the probe
// loop, feature downgrades, and the LUX_TUI_PROBE_TIMEOUT configuration.
//
// The probe reads replies through readInput, which on Windows waits on the
// input handle. Anonymous pipe handles never satisfy that wait (see the
// console-input skip in terminal_test.go for the same limitation), so tests
// that need bytes to arrive over a pipe run everywhere but Windows. The write
// error, silence, and EOF paths are platform independent and run everywhere.

// negotiatePipe hands the test a pipe whose ends are closed on cleanup.
func negotiatePipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	return reader, writer
}

// probeWriteFailer rejects every write so negotiate's write-error return can
// be exercised without touching the input side.
type probeWriteFailer struct{ err error }

func (w *probeWriteFailer) Write([]byte) (int, error) { return 0, w.err }

func TestParseProbeReply(t *testing.T) {
	tests := []struct {
		name     string
		stream   string
		want     probeReply
		leftover string
	}{
		{"empty stream", "", probeReply{}, ""},
		// The DA1 request form carries no '?', and the parser only consumes
		// report sequences, so the request form is replayed instead of
		// counting as an answer.
		{
			name:     "DA1 request form is not a report",
			stream:   "\x1b[c",
			want:     probeReply{},
			leftover: "\x1b[c",
		},
		// A DA1 body must be digits and semicolons; anything else is
		// treated as user input and replayed byte for byte.
		{"DA1 report with an empty body is replayed", "\x1b[?c", probeReply{}, "\x1b[?c"},
		{"DA1 report with a non-numeric body is replayed", "\x1b[?6 c", probeReply{}, "\x1b[?6 c"},
		{"DA1 report with attributes", "\x1b[?62;1;2c", probeReply{answered: true, da1: "62;1;2"}, ""},
		{"DA1 report with one attribute", "\x1b[?6c", probeReply{answered: true, da1: "6"}, ""},
		// DA2 answers carry the '>' introducer: they are recorded for
		// provenance but never mark the terminal answered — only DA1 proves
		// the classic report path the early-exit logic depends on.
		{"DA2 answer with the tmux code", "\x1b[>84;0;0c", probeReply{da2: "84;0;0"}, ""},
		{"DA2 answer from Windows Terminal", "\x1b[>0;10;1c", probeReply{da2: "0;10;1"}, ""},
		{"DA2 request form is not a report", "\x1b[>c", probeReply{}, "\x1b[>c"},
		{"DA2 with a space in the body is replayed", "\x1b[>84 0;0c", probeReply{}, "\x1b[>84 0;0c"},
		{"modifyOtherKeys report shape is replayed", "\x1b[>4;2m", probeReply{}, "\x1b[>4;2m"},
		{"DA1 then DA2 are both recorded", "\x1b[?62;1;2c\x1b[>1;0;0c", probeReply{answered: true, da1: "62;1;2", da2: "1;0;0"}, ""},
		{"first DA1 wins over a forged second", "\x1b[?62;1;2c\x1b[?1;2c", probeReply{answered: true, da1: "62;1;2"}, ""},
		{"first DA2 wins over a forged second", "\x1b[>84;0;0c\x1b[>0;10;1c", probeReply{da2: "84;0;0"}, ""},
		{"scroll regions rejected", "\x1b[?6c\x1b[?6;0$y", probeReply{answered: true, da1: "6", scroll: probeRejected}, ""},
		{"scroll regions set", "\x1b[?6c\x1b[?6;1$y", probeReply{answered: true, da1: "6", scroll: probeSupported}, ""},
		{"scroll regions reset", "\x1b[?6c\x1b[?6;2$y", probeReply{answered: true, da1: "6", scroll: probeSupported}, ""},
		{"scroll regions permanently set", "\x1b[?6c\x1b[?6;3$y", probeReply{answered: true, da1: "6", scroll: probeSupported}, ""},
		{"scroll regions permanently reset", "\x1b[?6c\x1b[?6;4$y", probeReply{answered: true, da1: "6", scroll: probeSupported}, ""},
		{"synchronized output rejected", "\x1b[?6c\x1b[?2026;0$y", probeReply{answered: true, da1: "6", sync: probeRejected}, ""},
		{"synchronized output set", "\x1b[?6c\x1b[?2026;2$y", probeReply{answered: true, da1: "6", sync: probeSupported}, ""},
		{"synchronized output permanently set", "\x1b[?6c\x1b[?2026;3$y", probeReply{answered: true, da1: "6", sync: probeSupported}, ""},
		// Status 4 for mode 2026 is "permanently reset": the set-mode wrapper
		// would be a permanent no-op, so the probe treats it as rejected.
		{"synchronized output permanently reset", "\x1b[?6c\x1b[?2026;4$y", probeReply{answered: true, da1: "6", sync: probeRejected}, ""},
		{"pixel mouse rejected", "\x1b[?6c\x1b[?1016;0$y", probeReply{answered: true, da1: "6", pixel: probeRejected}, ""},
		{"pixel mouse set", "\x1b[?6c\x1b[?1016;2$y", probeReply{answered: true, da1: "6", pixel: probeSupported}, ""},
		{"pixel mouse permanently set", "\x1b[?6c\x1b[?1016;3$y", probeReply{answered: true, da1: "6", pixel: probeSupported}, ""},
		// Status 4 for mode 1016 gets the same treatment as 2026: the mode
		// cannot be turned on, so pixel coordinates would never arrive and
		// enabling the decode would only misread cell reports.
		{"pixel mouse permanently reset", "\x1b[?6c\x1b[?1016;4$y", probeReply{answered: true, da1: "6", pixel: probeRejected}, ""},
		{"win32 input rejected", "\x1b[?6c\x1b[?9001;0$y", probeReply{answered: true, da1: "6", win32: probeRejected}, ""},
		{"win32 input set", "\x1b[?6c\x1b[?9001;1$y", probeReply{answered: true, da1: "6", win32: probeSupported}, ""},
		{"win32 input reset but settable", "\x1b[?6c\x1b[?9001;2$y", probeReply{answered: true, da1: "6", win32: probeSupported}, ""},
		{"win32 input permanently set", "\x1b[?6c\x1b[?9001;3$y", probeReply{answered: true, da1: "6", win32: probeSupported}, ""},
		// Status 4 for mode 9001 matches the other enable-only modes: a
		// permanently reset mode can never carry the CSI underscore
		// records, so gating the decode on it would only mislabel the
		// legacy stream it actually sends.
		{"win32 input permanently reset", "\x1b[?6c\x1b[?9001;4$y", probeReply{answered: true, da1: "6", win32: probeRejected}, ""},
		{"unicode core rejected", "\x1b[?6c\x1b[?2027;0$y", probeReply{answered: true, da1: "6", unicodeCore: probeRejected}, ""},
		{"unicode core set", "\x1b[?6c\x1b[?2027;1$y", probeReply{answered: true, da1: "6", unicodeCore: probeSupported}, ""},
		{"unicode core reset but settable", "\x1b[?6c\x1b[?2027;2$y", probeReply{answered: true, da1: "6", unicodeCore: probeSupported}, ""},
		{"unicode core permanently set", "\x1b[?6c\x1b[?2027;3$y", probeReply{answered: true, da1: "6", unicodeCore: probeSupported}, ""},
		// Status 4 for mode 2027 matches the other enable-only modes: a
		// permanently reset mode ignores the set-mode escape, so the
		// terminal can never be held to the clustering contract while the
		// capability bit would claim it is.
		{"unicode core permanently reset", "\x1b[?6c\x1b[?2027;4$y", probeReply{answered: true, da1: "6", unicodeCore: probeRejected}, ""},
		// Windows Terminal answers DA1 with a 39-byte report; the reportEnd
		// window must accept it or every WT launch stalls for the full probe
		// timeout and replays the reply as a phantom keystroke.
		{"Windows Terminal DA1 answer", "\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c", probeReply{answered: true, da1: "61;4;6;7;14;21;22;23;24;28;32;42;52"}, ""},
		{
			name:   "Windows Terminal full negotiation answer",
			stream: "\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c\x1b[?6;2$y\x1b[?2026;2$y",
			want:   probeReply{answered: true, da1: "61;4;6;7;14;21;22;23;24;28;32;42;52", scroll: probeSupported, sync: probeSupported},
		},
		{
			name:   "tmux full negotiation answer",
			stream: "\x1b[?1;2c\x1b[>84;0;0c\x1b[?6;2$y\x1b[?2026;2$y\x1b[?1016;0$y\x1b[?9001;0$y",
			want:   probeReply{answered: true, da1: "1;2", da2: "84;0;0", scroll: probeSupported, sync: probeSupported, pixel: probeRejected, win32: probeRejected},
		},
		{
			name:   "full negotiation answer",
			stream: "\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y\x1b[?1016;2$y",
			want:   probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, pixel: probeSupported},
		},
		{"leading zeros in a report", "\x1b[?6c\x1b[?06;01$y", probeReply{answered: true, da1: "6", scroll: probeSupported}, ""},
		{"report for an unprobed mode is consumed", "\x1b[?6c\x1b[?99;1$y", probeReply{answered: true, da1: "6"}, ""},
		{"user keys around reports are replayed", "\x1b[?6cX\x1b[?6;0$yY", probeReply{answered: true, da1: "6", scroll: probeRejected}, "XY"},
		{"mode-set escape after a query is replayed", "\x1b[?25h", probeReply{}, "\x1b[?25h"},
		{"mode report without DA1", "\x1b[?6;1$y", probeReply{scroll: probeSupported}, ""},
		{"arrow key escape is replayed", "\x1b[A", probeReply{}, "\x1b[A"},
		{"plain text is replayed", "hello", probeReply{}, "hello"},
		{"truncated escape is replayed", "\x1b", probeReply{}, "\x1b"},
		{"truncated report is replayed", "\x1b[?6;", probeReply{}, "\x1b[?6;"},
		{"report without a status is replayed", "\x1b[?6$y", probeReply{}, "\x1b[?6$y"},
		{"report without a mode is replayed", "\x1b[?;1$y", probeReply{}, "\x1b[?;1$y"},
		{"report with a junk status is replayed", "\x1b[?6;x$y", probeReply{}, "\x1b[?6;x$y"},
		{"report missing its final byte is replayed", "\x1b[?6;0$", probeReply{}, "\x1b[?6;0$"},
		{"overflowing status is replayed", "\x1b[?6;99999999999999999999$y", probeReply{}, "\x1b[?6;99999999999999999999$y"},
		// The kitty flags reply is digits behind the private-mode question
		// mark; its value is never parsed, only its shape matters, so even
		// overflowing digits count as an answer.
		{"kitty flags reply", "\x1b[?6c\x1b[?1u", probeReply{answered: true, da1: "6", keyboard: probeSupported}, ""},
		{"kitty flags reply without DA1", "\x1b[?1u", probeReply{keyboard: probeSupported}, ""},
		{"kitty flags of zero still count", "\x1b[?6c\x1b[?0u", probeReply{answered: true, da1: "6", keyboard: probeSupported}, ""},
		{"kitty flags with leading zeros", "\x1b[?6c\x1b[?013u", probeReply{answered: true, da1: "6", keyboard: probeSupported}, ""},
		{"overflowing kitty flags still count", "\x1b[?6c\x1b[?99999999999999999999u", probeReply{answered: true, da1: "6", keyboard: probeSupported}, ""},
		{"user keys around a kitty reply are replayed", "\x1b[?6cX\x1b[?1uY", probeReply{answered: true, da1: "6", keyboard: probeSupported}, "XY"},
		{
			name:   "full negotiation answer with kitty flags",
			stream: "\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y\x1b[?1016;2$y\x1b[?1u",
			want:   probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, pixel: probeSupported, keyboard: probeSupported},
		},
		{
			name:   "full negotiation answer with win32 input",
			stream: "\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y\x1b[?1016;2$y\x1b[?9001;2$y\x1b[?1u",
			want:   probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, pixel: probeSupported, win32: probeSupported, keyboard: probeSupported},
		},
		{
			name:   "full negotiation answer with unicode core",
			stream: "\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y\x1b[?1016;2$y\x1b[?9001;2$y\x1b[?2027;2$y\x1b[?1u",
			want:   probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, pixel: probeSupported, win32: probeSupported, unicodeCore: probeSupported, keyboard: probeSupported},
		},
		{"kitty query shape without digits is replayed", "\x1b[?u", probeReply{}, "\x1b[?u"},
		{"kitty reply with junk flags is replayed", "\x1b[?6c\x1b[?,u", probeReply{answered: true, da1: "6"}, "\x1b[?,u"},
		{"kitty reply with parameters is replayed", "\x1b[?6c\x1b[?1;1u", probeReply{answered: true, da1: "6"}, "\x1b[?1;1u"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply, leftover := parseProbeReply([]byte(tt.stream))
			if reply != tt.want {
				t.Errorf("parseProbeReply(%q) reply = %+v, want %+v", tt.stream, reply, tt.want)
			}
			if string(leftover) != tt.leftover {
				t.Errorf("parseProbeReply(%q) leftover = %q, want %q", tt.stream, leftover, tt.leftover)
			}
		})
	}
}

func TestReportEnd(t *testing.T) {
	tests := []struct {
		name      string
		rest      string
		wantIndex int
		wantOK    bool
	}{
		{"DA1 report", "\x1b[?c", 3, true},
		{"mode report", "\x1b[?6;0$y", 7, true},
		{"attributes before the final byte", "\x1b[?62;1;2c", 9, true},
		{"truncated report has no final byte", "\x1b[?6;", 0, false},
		{"introducer only", "\x1b[?", 0, false},
		{"empty", "", 0, false},
		{"final byte at the edge of the window", "\x1b[?" + strings.Repeat("1", 124) + "y", 127, true},
		{"final byte past the window", "\x1b[?" + strings.Repeat("1", 125) + "y", 0, false},
		{"Windows Terminal DA1 report", "\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c", 38, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index, ok := reportEnd([]byte(tt.rest))
			if index != tt.wantIndex || ok != tt.wantOK {
				t.Errorf("reportEnd(%q) = (%d, %t), want (%d, %t)", tt.rest, index, ok, tt.wantIndex, tt.wantOK)
			}
		})
	}
}

func TestParseStatusReport(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantMode   int
		wantStatus int
		wantOK     bool
	}{
		{"mode 6 reset", "6;0$", 6, 0, true},
		{"mode 2026 set", "2026;2$", 2026, 2, true},
		{"mode 2027 set", "2027;1$", 2027, 1, true},
		{"mode 6 permanently set", "6;3$", 6, 3, true},
		{"mode 6 permanently reset", "6;4$", 6, 4, true},
		{"leading zeros are tolerated", "06;01$", 6, 1, true},
		{"two status parameters", "6;1;2$", 0, 0, false},
		{"missing status", "6$", 0, 0, false},
		{"missing mode", ";1$", 0, 0, false},
		{"empty status", "6;;0$", 0, 0, false},
		{"junk status", "6;x$", 0, 0, false},
		{"junk mode", "x;1$", 0, 0, false},
		{"missing dollar final", "6;0", 0, 0, false},
		{"empty body", "", 0, 0, false},
		{"missing mode with status digits", ";12$", 0, 0, false},
		{"mode overflow", "99999999999999999999;1$", 0, 0, false},
		{"status overflow", "6;99999999999999999999$", 0, 0, false},
		{"stray dollar in the body", "6;0$$", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, status, ok := parseStatusReport([]byte(tt.body))
			if mode != tt.wantMode || status != tt.wantStatus || ok != tt.wantOK {
				t.Errorf("parseStatusReport(%q) = (%d, %d, %t), want (%d, %d, %t)", tt.body, mode, status, ok, tt.wantMode, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestNegotiateWriteError(t *testing.T) {
	reader, _ := negotiatePipe(t) // the input side is never touched
	boom := errors.New("probe write failed")
	start := time.Now()
	reply, leftover := negotiate(context.Background(), reader, &probeWriteFailer{err: boom}, defaultProbeTimeout)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("negotiate spent %v on a failing probe write, want an immediate return", elapsed)
	}
	if reply != (probeReply{}) {
		t.Errorf("reply after a failed probe write = %+v, want the zero value", reply)
	}
	if len(leftover) != 0 {
		t.Errorf("leftover after a failed probe write = %q, want none", leftover)
	}
}

func TestNegotiateSilentTerminalBounded(t *testing.T) {
	t.Run("open writer burns the budget", func(t *testing.T) {
		reader, _ := negotiatePipe(t) // write end stays open: silence without EOF
		probe := &bytes.Buffer{}
		start := time.Now()
		reply, leftover := negotiate(context.Background(), reader, probe, 50*time.Millisecond)
		elapsed := time.Since(start)
		if elapsed < 30*time.Millisecond {
			t.Errorf("negotiate returned after %v on a silent terminal, want the budget waited out", elapsed)
		}
		if elapsed > 2*time.Second {
			t.Errorf("negotiate stalled %v on a silent terminal", elapsed)
		}
		if reply != (probeReply{}) {
			t.Errorf("silent terminal reply = %+v, want the zero value", reply)
		}
		if len(leftover) != 0 {
			t.Errorf("silent terminal leftover = %q, want none", leftover)
		}
		if got := probe.String(); got != probeRequest {
			t.Errorf("probe request = %q, want %q", got, probeRequest)
		}
	})
	t.Run("closed writer ends the probe", func(t *testing.T) {
		reader, writer := negotiatePipe(t)
		if err := writer.Close(); err != nil {
			t.Fatalf("close write end: %v", err)
		}
		start := time.Now()
		reply, leftover := negotiate(context.Background(), reader, &bytes.Buffer{}, 50*time.Millisecond)
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("negotiate stalled %v on a closed pipe", elapsed)
		}
		if reply != (probeReply{}) {
			t.Errorf("closed pipe reply = %+v, want the zero value", reply)
		}
		if len(leftover) != 0 {
			t.Errorf("closed pipe leftover = %q, want none", leftover)
		}
	})
}

func TestNegotiateDetectsSupports(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("readInput's WaitForSingleObject never signals for anonymous pipe handles on Windows, so this pipe cannot deliver the reply")
	}
	reader, writer := negotiatePipe(t)
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y\x1b[?1016;2$y"))
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		done <- err
	}()
	probe := &bytes.Buffer{}
	start := time.Now()
	reply, leftover := negotiate(context.Background(), reader, probe, 200*time.Millisecond)
	if err := <-done; err != nil {
		t.Errorf("reply writer: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("negotiate took %v on a cooperative terminal", elapsed)
	}
	if got := probe.String(); got != probeRequest {
		t.Errorf("probe request = %q, want %q", got, probeRequest)
	}
	want := probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, pixel: probeSupported}
	if reply != want {
		t.Errorf("reply = %+v, want %+v", reply, want)
	}
	if len(leftover) != 0 {
		t.Errorf("leftover = %q, want none", leftover)
	}
}

// TestNegotiateKittyGrace pins the shape of the grace window: a kitty reply
// that lands fragmented behind the classic trio still completes the probe
// early, while a terminal with no kitty answer costs only the grace, never
// the full deadline.
func TestNegotiateKittyGrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("readInput's WaitForSingleObject never signals for anonymous pipe handles on Windows, so this pipe cannot deliver the reply")
	}
	t.Run("fragmented late replies land within the grace", func(t *testing.T) {
		reader, writer := negotiatePipe(t)
		done := make(chan error, 1)
		go func() {
			_, err := writer.Write([]byte("\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y"))
			// The late replies arrive as their own read, the way a protocol
			// terminal sends them behind the classic trio: kitty flags and
			// the pixel-mode report answer two of the probe-enable
			// questions. The early exit also wants the win32 and
			// Unicode-core reports, which never come here, so the stream
			// settles through the grace window instead — still far under
			// the deadline.
			time.Sleep(5 * time.Millisecond)
			if err == nil {
				_, err = writer.Write([]byte("\x1b[?1u\x1b[?1016;2$y"))
			}
			done <- err
		}()
		start := time.Now()
		reply, leftover := negotiate(context.Background(), reader, &bytes.Buffer{}, 400*time.Millisecond)
		if err := <-done; err != nil {
			t.Errorf("reply writer: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 400*time.Millisecond {
			t.Errorf("negotiate burned the full budget on a fragmented reply stream (elapsed %v)", elapsed)
		}
		want := probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported, keyboard: probeSupported, pixel: probeSupported}
		if reply != want {
			t.Errorf("reply = %+v, want %+v", reply, want)
		}
		if len(leftover) != 0 {
			t.Errorf("leftover = %q, want none", leftover)
		}
	})
	t.Run("silence after the classic trio pays only the grace", func(t *testing.T) {
		reader, writer := negotiatePipe(t)
		done := make(chan error, 1)
		go func() {
			// The write end stays open: silence without EOF is what a
			// terminal without the late protocols looks like — the kitty
			// flags and the pixel-mode report never answer, so the probe
			// returns after the grace rather than the full deadline.
			_, err := writer.Write([]byte("\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y"))
			done <- err
		}()
		start := time.Now()
		reply, leftover := negotiate(context.Background(), reader, &bytes.Buffer{}, 400*time.Millisecond)
		if err := <-done; err != nil {
			t.Errorf("reply writer: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed < kittyGrace {
			t.Errorf("negotiate returned after %v, want the %v grace window to be waited out", elapsed, kittyGrace)
		}
		if elapsed > 300*time.Millisecond {
			t.Errorf("negotiate spent %v waiting for late replies that never come, want the %v grace plus overhead", elapsed, kittyGrace)
		}
		want := probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported}
		if reply != want {
			t.Errorf("reply = %+v, want %+v (keyboard and pixel stay unknown on silence)", reply, want)
		}
		if len(leftover) != 0 {
			t.Errorf("leftover = %q, want none", leftover)
		}
	})
}

func TestNegotiatePreservesUserBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("readInput's WaitForSingleObject never signals for anonymous pipe handles on Windows, so this pipe cannot deliver the reply")
	}
	reader, writer := negotiatePipe(t)
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("\x1b[?6cabc"))
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		done <- err
	}()
	start := time.Now()
	reply, leftover := negotiate(context.Background(), reader, &bytes.Buffer{}, 100*time.Millisecond)
	if err := <-done; err != nil {
		t.Errorf("reply writer: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("negotiate took %v on a terminated reply stream", elapsed)
	}
	if reply != (probeReply{answered: true, da1: "6"}) {
		t.Errorf("reply = %+v, want only answered set with the DA1 body recorded", reply)
	}
	if string(leftover) != "abc" {
		t.Errorf("leftover = %q, want the user bytes %q", leftover, "abc")
	}
}

func TestNegotiateCapsRunawayStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the cap needs bytes flowing through readInput, and WaitForSingleObject never signals for anonymous pipe handles on Windows")
	}
	reader, writer := negotiatePipe(t)
	payload := strings.Repeat("x", maxProbeStream+1)
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte(payload))
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		done <- err
	}()
	start := time.Now()
	reply, leftover := negotiate(context.Background(), reader, &bytes.Buffer{}, 200*time.Millisecond)
	if err := <-done; err != nil {
		t.Errorf("reply writer: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("negotiate took %v on a runaway stream", elapsed)
	}
	if reply != (probeReply{}) {
		t.Errorf("reply on a runaway stream = %+v, want the zero value", reply)
	}
	// The cap stops the inspection, not the accounting: every byte already
	// read is replayed, so the leftover is the whole head of the runaway
	// stream that crossed the cap (it can overshoot by up to one read),
	// never a truncated slice of it.
	if len(leftover) < maxProbeStream {
		t.Fatalf("leftover length = %d, want at least the %d-byte cap", len(leftover), maxProbeStream)
	}
	if !strings.HasPrefix(payload, string(leftover)) {
		t.Error("leftover bytes are not the head of the runaway stream")
	}
	// Whatever negotiate stopped reading must still sit in the pipe for the
	// read loop that runs after Open, so no byte is ever dropped.
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the unread tail: %v", err)
	}
	if string(leftover)+string(rest) != payload {
		t.Errorf("leftover %d + unread %d bytes do not rebuild the %d-byte payload", len(leftover), len(rest), len(payload))
	}
}

// TestApplyProbeFeatures pins the direction of every probe verdict. Scroll
// regions stay veto-only: a positive reply never upgrades a set that does
// not already claim them. Synchronized output is probe-driven both ways
// under the auto default — a mode 2026 answer that recognizes the mode
// turns the wrapper on wherever it lands, a rejection turns it off — and
// the LUX_TUI_SYNC_OUTPUT override replaces the verdict entirely. The
// kitty keyboard, pixel mouse, and win32-input verdicts are enable-only:
// enhanced input and win32-input change how keys are encoded, and pixel
// mouse changes how coordinates are encoded, so each is enabled by a
// positive reply and by nothing else. Unicode core mode joins the
// enable-only family: a positive reply moves the clustering contract to the
// terminal, and a rejection or silence keeps our own grapheme measurement as
// the single width authority — nothing is ever downgraded by a report.
func TestApplyProbeFeatures(t *testing.T) {
	before := capability.Set{
		Terminal:           "vt500",
		Interactive:        true,
		Unicode:            true,
		Color:              capability.TrueColor,
		AlternateScreen:    true,
		CursorAddressing:   true,
		Mouse:              true,
		Focus:              true,
		BracketedPaste:     true,
		KeyboardEnhanced:   false,
		ScrollRegions:      true,
		SynchronizedOutput: true,
		Hyperlinks:         true,
		Graphics:           capability.GraphicsSixel,
	}
	scrollOff := before
	scrollOff.ScrollRegions = false
	syncOff := before
	syncOff.SynchronizedOutput = false
	bothOff := before
	bothOff.ScrollRegions = false
	bothOff.SynchronizedOutput = false
	keyboardOn := before
	keyboardOn.KeyboardEnhanced = true
	pixelOn := before
	pixelOn.PixelMouse = true
	win32On := before
	win32On.Win32Input = true
	unicodeCoreOn := before
	unicodeCoreOn.UnicodeCoreMode = true
	tests := []struct {
		name     string
		features capability.Set
		reply    probeReply
		sync     syncOutputMode
		want     capability.Set
	}{
		// The auto default: only the 2026 answer moves the flag.
		{"unknown reply keeps defaults", before, probeReply{}, syncOutputAuto, before},
		{"supported scroll keeps the flag", before, probeReply{answered: true, da1: "6", scroll: probeSupported}, syncOutputAuto, before},
		{"supported sync keeps the flag", before, probeReply{answered: true, da1: "6", sync: probeSupported}, syncOutputAuto, before},
		{"rejected scroll clears scroll regions", before, probeReply{answered: true, scroll: probeRejected}, syncOutputAuto, scrollOff},
		{"rejected sync clears synchronized output", before, probeReply{answered: true, da1: "6", sync: probeRejected}, syncOutputAuto, syncOff},
		{"both rejected clear both", before, probeReply{answered: true, scroll: probeRejected, sync: probeRejected}, syncOutputAuto, bothOff},
		{"supported sync upgrades a set that does not claim it", syncOff, probeReply{answered: true, da1: "6", sync: probeSupported}, syncOutputAuto, before},
		{"supported sync upgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", sync: probeSupported}, syncOutputAuto, capability.Set{SynchronizedOutput: true}},
		{"rejected sync downgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", sync: probeRejected}, syncOutputAuto, capability.Set{}},
		{"already disabled stays disabled", bothOff, probeReply{answered: true, scroll: probeRejected, sync: probeRejected}, syncOutputAuto, bothOff},
		// The override replaces the verdict in both directions: it exists
		// for terminals whose report lies.
		{"forced on beats a rejection", syncOff, probeReply{answered: true, da1: "6", sync: probeRejected}, syncOutputOn, before},
		{"forced on upgrades an empty set", capability.Set{}, probeReply{}, syncOutputOn, capability.Set{SynchronizedOutput: true}},
		{"forced off beats support", before, probeReply{answered: true, da1: "6", sync: probeSupported}, syncOutputOff, syncOff},
		{"forced off stays off on silence", syncOff, probeReply{}, syncOutputOff, syncOff},
		// Other verdicts are untouched by the sync mode.
		{"supported scroll and sync never upgrade scroll in an empty set", capability.Set{}, probeReply{answered: true, da1: "62;1;2", scroll: probeSupported, sync: probeSupported}, syncOutputAuto, capability.Set{SynchronizedOutput: true}},
		{"supported kitty turns the keyboard on", before, probeReply{answered: true, da1: "6", keyboard: probeSupported}, syncOutputAuto, keyboardOn},
		{"kitty support upgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", keyboard: probeSupported}, syncOutputAuto, capability.Set{KeyboardEnhanced: true}},
		{"rejected kitty changes nothing", before, probeReply{answered: true, keyboard: probeRejected}, syncOutputAuto, before},
		{"unknown kitty keeps the current state", keyboardOn, probeReply{answered: true}, syncOutputAuto, keyboardOn},
		{"supported pixel turns the mode on", before, probeReply{answered: true, da1: "6", pixel: probeSupported}, syncOutputAuto, pixelOn},
		{"pixel support upgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", pixel: probeSupported}, syncOutputAuto, capability.Set{PixelMouse: true}},
		{"rejected pixel changes nothing", before, probeReply{answered: true, da1: "6", pixel: probeRejected}, syncOutputAuto, before},
		{"unknown pixel keeps the current state", pixelOn, probeReply{answered: true}, syncOutputAuto, pixelOn},
		{"supported win32 turns the mode on", before, probeReply{answered: true, da1: "6", win32: probeSupported}, syncOutputAuto, win32On},
		{"win32 support upgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", win32: probeSupported}, syncOutputAuto, capability.Set{Win32Input: true}},
		{"rejected win32 changes nothing", before, probeReply{answered: true, da1: "6", win32: probeRejected}, syncOutputAuto, before},
		{"unknown win32 keeps the current state", win32On, probeReply{answered: true}, syncOutputAuto, win32On},
		{"supported unicode core turns the mode on", before, probeReply{answered: true, da1: "6", unicodeCore: probeSupported}, syncOutputAuto, unicodeCoreOn},
		{"unicode core support upgrades even an empty set", capability.Set{}, probeReply{answered: true, da1: "6", unicodeCore: probeSupported}, syncOutputAuto, capability.Set{UnicodeCoreMode: true}},
		{"rejected unicode core changes nothing", before, probeReply{answered: true, da1: "6", unicodeCore: probeRejected}, syncOutputAuto, before},
		{"unknown unicode core keeps the current state", unicodeCoreOn, probeReply{answered: true}, syncOutputAuto, unicodeCoreOn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyProbeFeatures(tt.features, tt.reply, tt.sync); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyProbeFeatures(%+v, %+v, %d) = %+v, want %+v", tt.features, tt.reply, tt.sync, got, tt.want)
			}
		})
	}
}

// TestSyncOutputConfigured pins the override vocabulary: on-words force the
// wrapper on, off-words force it off, and everything else leaves the
// decision to the probe. The garbage case must not panic the Open path —
// it degrades to auto after a one-line stderr notice, which the caller
// cannot assert here, so the return value alone is pinned.
func TestSyncOutputConfigured(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  syncOutputMode
	}{
		{"empty leaves the probe to decide", "", syncOutputAuto},
		{"auto keyword", "auto", syncOutputAuto},
		{"default keyword", "default", syncOutputAuto},
		{"keyword is case insensitive", "AUTO", syncOutputAuto},
		{"one forces on", "1", syncOutputOn},
		{"true forces on", "true", syncOutputOn},
		{"yes forces on", "yes", syncOutputOn},
		{"on forces on", "on", syncOutputOn},
		{"zero forces off", "0", syncOutputOff},
		{"false forces off", "false", syncOutputOff},
		{"no forces off", "no", syncOutputOff},
		{"off forces off", "off", syncOutputOff},
		{"surrounding spaces are trimmed", " on ", syncOutputOn},
		{"garbage falls back to auto", "banana", syncOutputAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LUX_TUI_SYNC_OUTPUT", tt.value)
			if got := syncOutputConfigured(); got != tt.want {
				t.Errorf("syncOutputConfigured() with %q = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestProbeTimeoutConfigured(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		want       time.Duration
		wantEnable bool
	}{
		{"empty means default", "", defaultProbeTimeout, true},
		{"default keyword", "default", defaultProbeTimeout, true},
		{"keyword is case insensitive", "DEFAULT", defaultProbeTimeout, true},
		{"milliseconds budget", "250", 250 * time.Millisecond, true},
		{"surrounding spaces are trimmed", " 250 ", 250 * time.Millisecond, true},
		{"off disables", "off", 0, false},
		{"false disables", "false", 0, false},
		{"no disables", "no", 0, false},
		{"disable keywords are case insensitive", "OFF", 0, false},
		{"zero disables", "0", 0, false},
		{"negative disables", "-5", 0, false},
		{"garbage falls back to default", "abc", defaultProbeTimeout, true},
		// "true" is not a disable keyword and not a number, so it falls
		// through to the default branch: negotiation stays enabled.
		{"true is not a disable keyword", "true", defaultProbeTimeout, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LUX_TUI_PROBE_TIMEOUT", tt.value)
			got, enabled := probeTimeoutConfigured()
			if got != tt.want || enabled != tt.wantEnable {
				t.Errorf("probeTimeoutConfigured() with %q = (%v, %t), want (%v, %t)", tt.value, got, enabled, tt.want, tt.wantEnable)
			}
		})
	}
}
