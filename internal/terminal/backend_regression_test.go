package terminal

import (
	"context"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// TestReadLoopReplaysLargePrefillWithoutDeadlock pins the probe-leftover
// replay fix. Keystrokes captured while Open negotiates the terminal used to
// be replayed into the event channel from Open itself, with the backend mutex
// held, so a large paste larger than the channel capacity wedged Open against
// NextEvent forever. The fix hands the leftover bytes to readLoop as a prefill
// that is fed before the first read, where every event send can block on the
// channel without anyone holding a lock. The deliberately undersized channel
// below reproduces the old blocking condition: with the fix, the replay only
// slows down until the test drains it.
func TestReadLoopReplaysLargePrefillWithoutDeadlock(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()
	// The writer end stays open so the loop never sees EOF: after the prefill
	// is drained, readLoop must park inside readInput, which a cancelled
	// context has to unwind without a read ever succeeding.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := &Backend{input: input}
	events := make(chan event.Event, 8)
	done := make(chan struct{})
	prefill := []byte(strings.Repeat("a", 200))
	go backend.readLoop(ctx, done, events, newParser(), prefill)

	want := event.RuneKey('a', 0)
	for i := 0; i < len(prefill); i++ {
		select {
		case next := <-events:
			if !reflect.DeepEqual(next, want) {
				t.Fatalf("prefill event %d = %#v, want %#v", i, next, want)
			}
		case <-time.After(hangTimeout):
			t.Fatalf("timed out waiting for prefill event %d of %d", i+1, len(prefill))
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(hangTimeout):
		t.Fatal("read loop did not exit after context cancellation")
	}
}

// TestPresentSyncVetoReachesOutput pins the synchronized-output veto on
// Present. The safe default leaves SynchronizedOutput off (it is opt-in via
// LUX_TUI_SYNC_OUTPUT and negotiation can only downgrade), so a diff must be
// written bare, with no mode 2026 wrapper at all; only the opted-in case may
// wrap the diff in \x1b[?2026h and \x1b[?2026l.
func TestPresentSyncVetoReachesOutput(t *testing.T) {
	tests := []struct {
		name string
		sync bool
	}{
		{"vetoed default writes no mode 2026", false},
		{"opted in wraps the diff in mode 2026", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			previous := render.NewBuffer(4, 1)
			render.NewCanvas(previous).Text(0, 0, "old", render.Style{})
			frame := previous.Clone()
			render.NewCanvas(frame).Text(0, 0, "new", render.Style{})

			file, err := os.CreateTemp(t.TempDir(), "terminal-output")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			backend := &Backend{
				opened:   true,
				output:   file,
				writer:   file,
				features: capability.Set{Color: capability.TrueColor, ScrollRegions: true, SynchronizedOutput: tc.sync},
				previous: previous,
			}
			if err := backend.Present(context.Background(), frame); err != nil {
				t.Fatal(err)
			}

			if _, err := file.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			if !strings.Contains(got, "new") {
				t.Fatalf("changed frame emitted no diff: %q", got)
			}
			if !tc.sync && strings.Contains(got, "\x1b[?2026") {
				t.Fatalf("vetoed synchronized output leaked mode 2026: %q", got)
			}
			if tc.sync && (!strings.Contains(got, "\x1b[?2026h") || !strings.Contains(got, "\x1b[?2026l")) {
				t.Fatalf("synchronized output missing mode 2026 wrapper: %q", got)
			}
		})
	}
}

// TestResizeDuringProbe pins the probe-window resize replay: a terminal
// resized between Open's pre-probe snapshot and its post-probe comparison
// must surface as a synthesized ResizeTo event, because the watchers only
// start after the probe returns and would otherwise miss it. The rows also
// pin what the replay must NOT do: no event when the pre-probe size was
// unknown (the probe ran against something without a size), none when the
// post-probe query failed, and none when nothing changed.
func TestResizeDuringProbe(t *testing.T) {
	postFailure := errors.New("size unavailable after probe")
	tests := []struct {
		name       string
		sizeKnown  bool
		preWidth   int
		preHeight  int
		postWidth  int
		postHeight int
		postErr    error
		wantEvent  bool
	}{
		{"pre-probe size unknown stays quiet", false, 0, 0, 100, 50, nil, false},
		{"failed post-probe query stays quiet", true, 80, 24, 0, 0, postFailure, false},
		{"unchanged size stays quiet", true, 100, 50, 100, 50, nil, false},
		{"width change replays the post size", true, 80, 24, 100, 24, nil, true},
		{"height change replays the post size", true, 80, 24, 80, 50, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resize, resized := resizeDuringProbe(tc.sizeKnown, tc.preWidth, tc.preHeight, tc.postWidth, tc.postHeight, tc.postErr)
			if resized != tc.wantEvent {
				t.Fatalf("resizeDuringProbe(...) reported event %v, want %v", resized, tc.wantEvent)
			}
			if !tc.wantEvent {
				return
			}
			if resize.Kind != event.Resize {
				t.Fatalf("synthesized event kind = %v, want %v", resize.Kind, event.Resize)
			}
			if resize.Width != tc.postWidth || resize.Height != tc.postHeight {
				t.Fatalf("synthesized resize = %dx%d, want %dx%d", resize.Width, resize.Height, tc.postWidth, tc.postHeight)
			}
		})
	}
}

// TestTerminalSizeAnswersThroughInputHandle pins the Windows screen-buffer
// fallback. The console splits its handles by direction: the size query
// needs an output handle and fails on the console input handle, which is
// exactly the handle the backend holds for reads. Before the fallback, every
// Windows size query through the input side failed silently — the resize
// poller never delivered an event, and the probe-window replay above was
// dead code on Windows. The test runs only where a console is attached; a
// piped process without one has nothing to fall back to and skips.
func TestTerminalSizeAnswersThroughInputHandle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("console handle split is a Windows property")
	}
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no console to query: %v", err)
	}
	defer func() { _ = input.Close() }()
	width, height, err := terminalSize(input)
	if err != nil {
		t.Fatalf("terminalSize through the input handle failed: %v", err)
	}
	if width <= 0 || height <= 0 {
		t.Fatalf("terminalSize through the input handle = %dx%d, want positive", width, height)
	}
}
