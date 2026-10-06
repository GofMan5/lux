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
		{"DA1 report with attributes", "\x1b[?62;1;2c", probeReply{answered: true}, ""},
		{"DA1 report with one attribute", "\x1b[?6c", probeReply{answered: true}, ""},
		{"scroll regions rejected", "\x1b[?6c\x1b[?6;0$y", probeReply{answered: true, scroll: probeRejected}, ""},
		{"scroll regions set", "\x1b[?6c\x1b[?6;1$y", probeReply{answered: true, scroll: probeSupported}, ""},
		{"scroll regions reset", "\x1b[?6c\x1b[?6;2$y", probeReply{answered: true, scroll: probeSupported}, ""},
		{"scroll regions permanently set", "\x1b[?6c\x1b[?6;3$y", probeReply{answered: true, scroll: probeSupported}, ""},
		{"scroll regions permanently reset", "\x1b[?6c\x1b[?6;4$y", probeReply{answered: true, scroll: probeSupported}, ""},
		{"synchronized output rejected", "\x1b[?6c\x1b[?2026;0$y", probeReply{answered: true, sync: probeRejected}, ""},
		{"synchronized output set", "\x1b[?6c\x1b[?2026;2$y", probeReply{answered: true, sync: probeSupported}, ""},
		{"synchronized output permanently set", "\x1b[?6c\x1b[?2026;3$y", probeReply{answered: true, sync: probeSupported}, ""},
		// Status 4 for mode 2026 is "permanently reset": the set-mode wrapper
		// would be a permanent no-op, so the probe treats it as rejected.
		{"synchronized output permanently reset", "\x1b[?6c\x1b[?2026;4$y", probeReply{answered: true, sync: probeRejected}, ""},
		// Windows Terminal answers DA1 with a 39-byte report; the reportEnd
		// window must accept it or every WT launch stalls for the full probe
		// timeout and replays the reply as a phantom keystroke.
		{"Windows Terminal DA1 answer", "\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c", probeReply{answered: true}, ""},
		{
			name:   "Windows Terminal full negotiation answer",
			stream: "\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c\x1b[?6;2$y\x1b[?2026;2$y",
			want:   probeReply{answered: true, scroll: probeSupported, sync: probeSupported},
		},
		{
			name:   "full negotiation answer",
			stream: "\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y",
			want:   probeReply{answered: true, scroll: probeSupported, sync: probeSupported},
		},
		{"leading zeros in a report", "\x1b[?6c\x1b[?06;01$y", probeReply{answered: true, scroll: probeSupported}, ""},
		{"report for an unprobed mode is consumed", "\x1b[?6c\x1b[?99;1$y", probeReply{answered: true}, ""},
		{"user keys around reports are replayed", "\x1b[?6cX\x1b[?6;0$yY", probeReply{answered: true, scroll: probeRejected}, "XY"},
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
		_, err := writer.Write([]byte("\x1b[?62;1;2c\x1b[?6;1$y\x1b[?2026;2$y"))
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
	want := probeReply{answered: true, scroll: probeSupported, sync: probeSupported}
	if reply != want {
		t.Errorf("reply = %+v, want %+v", reply, want)
	}
	if len(leftover) != 0 {
		t.Errorf("leftover = %q, want none", leftover)
	}
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
	if reply != (probeReply{answered: true}) {
		t.Errorf("reply = %+v, want only answered set", reply)
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

func TestApplyProbeFeaturesOnlyDowngrades(t *testing.T) {
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
	tests := []struct {
		name     string
		features capability.Set
		reply    probeReply
		want     capability.Set
	}{
		{"unknown reply keeps defaults", before, probeReply{}, before},
		{"supported scroll keeps the flag", before, probeReply{answered: true, scroll: probeSupported}, before},
		{"supported sync keeps the flag", before, probeReply{answered: true, sync: probeSupported}, before},
		{"rejected scroll clears scroll regions", before, probeReply{answered: true, scroll: probeRejected}, scrollOff},
		{"rejected sync clears synchronized output", before, probeReply{answered: true, sync: probeRejected}, syncOff},
		{"both rejected clear both", before, probeReply{answered: true, scroll: probeRejected, sync: probeRejected}, bothOff},
		{"supported never upgrades an empty set", capability.Set{}, probeReply{answered: true, scroll: probeSupported, sync: probeSupported}, capability.Set{}},
		{"already disabled stays disabled", bothOff, probeReply{answered: true, scroll: probeRejected, sync: probeRejected}, bothOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyProbeFeatures(tt.features, tt.reply); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyProbeFeatures(%+v, %+v) = %+v, want %+v", tt.features, tt.reply, got, tt.want)
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
