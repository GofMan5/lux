package terminal

import (
	"context"
	"os"
	"reflect"
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
