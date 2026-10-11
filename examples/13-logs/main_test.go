package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// hangTimeout bounds a test that would otherwise wait forever; a loaded
// machine should never turn a stall into a false failure. A starved shared
// CI runner can freeze the process for tens of seconds while sibling
// package binaries run, so the guard absorbs runner starvation and still
// fails a real hang long before go test's package timeout.
const hangTimeout = 45 * time.Second

// chanSource hands the test full control over when lines arrive: nothing
// is generated until a batch lands on the channel, so user input can be
// interleaved between arrivals deterministically.
type chanSource struct {
	batches chan []logEntry
}

func newChanSource() *chanSource {
	return &chanSource{batches: make(chan []logEntry, 8)}
}

func (s *chanSource) Poll(ctx context.Context) []logEntry {
	select {
	case lines := <-s.batches:
		return lines
	case <-ctx.Done():
		return nil
	}
}

// Done is always false: a live tail has no end. The run loop stops it by
// cancelling the context when the app quits.
func (s *chanSource) Done() bool { return false }

// entry builds one test line with a fixed clock, so paints are
// deterministic down to the second.
func entry(level, msg string, second int) logEntry {
	return logEntry{at: time.Date(2025, 1, 1, 10, 30, second, 0, time.UTC), level: level, msg: msg}
}

// paintOnce renders one 64x16 frame and returns its buffer; View sets
// viewRows as a side effect, which state-level tests rely on.
func paintOnce(t *testing.T, app *logsApp) *render.Buffer {
	t.Helper()
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(64, 16)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
	app.View(frame)
	return frame.Canvas.Buffer()
}

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

// keyScroll builds the routed shape ui.Context.Handle produces for a
// scroll key on the focused log region.
func keyScroll(code event.KeyCode) ui.Action {
	return ui.Action{
		Kind:   ui.Scroll,
		Bounds: render.Rect{W: 60, H: 10},
		Event:  event.KeyPress(code, 0),
	}
}

// newUnitApp returns an app with a small cap and no live source:
// state-level tests drive appendBatch and applyList directly, bypassing
// the command seam (chanSource covers that in the E2E test).
func newUnitApp(cap int) *logsApp {
	return newLogsDemo(cap, nil, time.Hour)
}

// TestLogsFollowTailPausesOnScrollAndBoundsHistory is the slice's core
// promise: the history stays bounded (drops are counted, never silently
// lost), follow-tail moves the view while the user follows, a manual
// scroll pauses the follow so arriving lines keep the screen steady
// under the cursor, and reaching the bottom resumes the tail.
func TestLogsFollowTailPausesOnScrollAndBoundsHistory(t *testing.T) {
	app := newUnitApp(20)
	paintOnce(t, app) // viewRows = 10

	if app.viewRows != 10 {
		t.Fatalf("viewRows = %d; want 10", app.viewRows)
	}

	seed := make([]logEntry, 10)
	for i := range seed {
		seed[i] = entry("info", fmt.Sprintf("boot %d", i+1), i)
	}
	app.appendBatch(seed)
	// Exactly one full page: the tail is the page, follow holds it.
	if app.follow != true || app.list.Offset != 0 || app.list.Selected != 9 {
		t.Fatalf("after seed: follow=%v offset=%d selected=%d", app.follow, app.list.Offset, app.list.Selected)
	}

	// An append while following must move the view to the new tail.
	app.appendBatch([]logEntry{entry("info", "tick 11", 10), entry("warn", "tick 12", 11), entry("info", "tick 13", 12)})
	if app.list.Offset != 3 || app.list.Selected != 12 || app.seqAt(app.list.Selected) != 13 {
		t.Fatalf("follow append: offset=%d selected=%d seq=%d", app.list.Offset, app.list.Selected, app.seqAt(app.list.Selected))
	}

	// One manual scroll up pauses the follow.
	app.applyList(keyScroll(event.KeyUp))
	if app.follow {
		t.Fatalf("scroll up did not pause follow")
	}
	if app.list.Selected != 11 || app.list.Offset != 3 {
		t.Fatalf("scroll up: selected=%d offset=%d; want 11/3", app.list.Selected, app.list.Offset)
	}

	// While paused, appends keep the same rows under the cursor: the
	// anchor entry stays on its screen row even as the view grows.
	app.appendBatch([]logEntry{
		entry("info", "tick 14", 13), entry("warn", "tick 15", 14),
		entry("info", "tick 16", 15), entry("info", "tick 17", 16),
	})
	if app.follow || app.list.Selected != 11 || app.list.Offset != 3 || app.seqAt(app.list.Selected) != 12 {
		t.Fatalf("paused append moved the view: follow=%v selected=%d offset=%d seq=%d",
			app.follow, app.list.Selected, app.list.Offset, app.seqAt(app.list.Selected))
	}
	for row := 0; row < 10; row++ {
		if got := app.seqAt(app.list.Offset + row); got != row+4 {
			t.Fatalf("row %d shows seq %d; want %d (view slid)", row, got, row+4)
		}
	}

	// Past the cap, the oldest lines drop and the drop is counted. The
	// anchor survives the trim and the view compensates the shift.
	app.appendBatch(make([]logEntry, 10)) // 27 total, cap 20: 7 dropped
	if app.dropped != 7 || len(app.entries) != 20 {
		t.Fatalf("bound: dropped=%d entries=%d; want 7/20", app.dropped, len(app.entries))
	}
	if app.seqAt(app.list.Selected) != 12 {
		t.Fatalf("anchor lost after trim: selected=%d seq=%d", app.list.Selected, app.seqAt(app.list.Selected))
	}
	// seq 12 sits at index 4 of the retained window; the anchor wanted
	// screen row 8, but only 4 rows precede it, so the offset clamps to
	// the honest top and the cursor stays on the anchor line.
	if app.list.Selected != 4 || app.list.Offset != 0 {
		t.Fatalf("post-trim anchor: selected=%d offset=%d; want 4/0", app.list.Selected, app.list.Offset)
	}

	// End pins the cursor back on the newest line, which resumes follow.
	app.applyList(keyScroll(event.KeyEnd))
	if !app.follow || app.list.Selected != 19 || app.list.Offset != 10 {
		t.Fatalf("end: follow=%v selected=%d offset=%d", app.follow, app.list.Selected, app.list.Offset)
	}

	// Appends while following stay pinned to the tail across further drops.
	app.appendBatch([]logEntry{entry("error", "boom 28", 27), entry("error", "boom 29", 28)})
	if !app.follow || app.list.Selected != 19 || app.list.Offset != 10 || app.seqAt(19) != 29 {
		t.Fatalf("tail pin after drops: follow=%v selected=%d offset=%d seq=%d",
			app.follow, app.list.Selected, app.list.Offset, app.seqAt(19))
	}
	if app.dropped != 9 {
		t.Fatalf("dropped = %d; want 9", app.dropped)
	}
}

// TestLogsFilterNarrowsWidensAndKeepsAnchor covers the filter contract:
// narrowing keeps the cursor on a surviving line (or the nearest honest
// row when the anchor stops matching), widening restores the anchor, and
// a follow-mode filter pins the tail of the matches.
func TestLogsFilterNarrowsWidensAndKeepsAnchor(t *testing.T) {
	app := newUnitApp(100)
	paintOnce(t, app)

	// Twelve lines cycling info/warn/error: error seqs are 3, 6, 9, 12.
	seed := make([]logEntry, 12)
	levels := []string{"info", "warn", "error"}
	for i := range seed {
		seed[i] = entry(levels[i%3], fmt.Sprintf("msg %d", i+1), i)
	}
	app.appendBatch(seed)
	app.applyList(keyScroll(event.KeyUp)) // pause: cursor on seq 11
	if app.seqAt(app.list.Selected) != 11 {
		t.Fatalf("pre-filter cursor seq = %d; want 11", app.seqAt(app.list.Selected))
	}

	// The anchor (warn) does not survive an "error" filter: the cursor
	// clamps to the last error line and the anchor follows it.
	app.setFilter("error")
	if len(app.order) != 4 {
		t.Fatalf("error matches = %d; want 4", len(app.order))
	}
	if app.seqAt(app.list.Selected) != 12 {
		t.Fatalf("narrowed cursor seq = %d; want 12", app.seqAt(app.list.Selected))
	}
	// seq 12 is the last match, so the cursor is on the tail of the
	// filtered view even though the follow is off — the reading position
	// is honest for the narrow view.
	if app.list.Selected != 3 || app.list.Offset != 0 {
		t.Fatalf("narrowed view: selected=%d offset=%d; want 3/0", app.list.Selected, app.list.Offset)
	}

	// Widening restores the anchor: the cursor returns to the entry it
	// held before the query, on the screen row it was left on.
	app.setFilter("")
	if len(app.order) != 12 || app.seqAt(app.list.Selected) != 12 {
		t.Fatalf("widened cursor seq = %d; want 12 (anchor restored)", app.seqAt(app.list.Selected))
	}
	if app.list.Offset != 2 {
		t.Fatalf("widened offset = %d; want 2 (anchor row kept)", app.list.Offset)
	}

	// While following, a filter pins the tail of the matches: the newest
	// matching line is what the follower is following.
	app.applyList(keyScroll(event.KeyEnd))
	app.setFilter("error")
	if len(app.order) != 4 || !app.follow || app.list.Selected != 3 {
		t.Fatalf("follow filter: matches=%d follow=%v selected=%d", len(app.order), app.follow, app.list.Selected)
	}
}

// TestLogsFilterBackspaceNarrowsIncrementally drives the filter the way
// a user types it: one rune at a time, each keystroke rebuilding the
// view incrementally.
func TestLogsFilterBackspaceNarrowsIncrementally(t *testing.T) {
	app := newUnitApp(100)
	paintOnce(t, app)

	seed := make([]logEntry, 10)
	for i := range seed {
		seed[i] = entry("warn", fmt.Sprintf("disk %d%%", 90-i), i)
	}
	// One line that shares the prefix but not the tail.
	seed[3] = entry("info", "disk full", 3)
	app.appendBatch(seed)

	app.setFilter("warn")
	if len(app.order) != 9 {
		t.Fatalf("warn matches = %d; want 9", len(app.order))
	}
	app.setFilter("warnx") // narrows to nothing: honest empty, anchor kept
	if len(app.order) != 0 {
		t.Fatalf("warnx matches = %d; want 0", len(app.order))
	}
	app.setFilter("warn")
	if len(app.order) != 9 {
		t.Fatalf("matches did not return: %d", len(app.order))
	}
	// Level and message both match, case-insensitively. "disk 9" is
	// "disk 90" alone: "disk 89" does not contain the contiguous run.
	app.setFilter("DISK 9")
	if len(app.order) != 1 {
		t.Fatalf("message matches = %d; want 1", len(app.order))
	}
}

// TestLogsPaintsLevelsStatusAndFooter renders a page and checks the
// visible contract: level words paint on their lines, the status reports
// counts, drops, and the follow state, and the footer degrades.
func TestLogsPaintsLevelsStatusAndFooter(t *testing.T) {
	app := newUnitApp(20)
	paintOnce(t, app)

	seed := make([]logEntry, 12)
	levels := []string{"info", "warn", "error"}
	for i := range seed {
		seed[i] = entry(levels[i%3], fmt.Sprintf("msg %d", i+1), i)
	}
	app.appendBatch(seed)

	painted := paintOnce(t, app)
	if !strings.Contains(rowText(painted, 0), "LUX LOGS") {
		t.Fatalf("title missing: %q", rowText(painted, 0))
	}
	if !strings.Contains(rowText(painted, 1), "filter") {
		t.Fatalf("filter placeholder missing: %q", rowText(painted, 1))
	}
	// Follow pins the tail, so the page shows the last ten lines: seqs
	// 3..12 at body rows 2..11, levels cycling info/warn/error.
	if body := rowText(painted, 2); !strings.Contains(body, "10:30:02") || !strings.Contains(body, "ERROR") || !strings.Contains(body, "msg 3") {
		t.Fatalf("first row: %q", body)
	}
	if body := rowText(painted, 3); !strings.Contains(body, "INFO") || !strings.Contains(body, "msg 4") {
		t.Fatalf("info row: %q", body)
	}
	if body := rowText(painted, 4); !strings.Contains(body, "WARN") || !strings.Contains(body, "msg 5") {
		t.Fatalf("warn row: %q", body)
	}
	// The newest line carries the cursor marker.
	if body := rowText(painted, 11); !strings.Contains(body, "> ") || !strings.Contains(body, "msg 12") {
		t.Fatalf("tail marker: %q", body)
	}
	status := rowText(painted, 14)
	if !strings.Contains(status, "12 lines") || !strings.Contains(status, "follow") || !strings.Contains(status, "line 12") {
		t.Fatalf("status: %q", status)
	}
	if footer := rowText(painted, 15); !strings.Contains(footer, "esc quit") || !strings.Contains(footer, "end follow") {
		t.Fatalf("footer: %q", footer)
	}

	// A manual scroll flips the status to paused; the marker moves with
	// the cursor (from body row 11 to row 10).
	app.applyList(keyScroll(event.KeyUp))
	painted = paintOnce(t, app)
	if status := rowText(painted, 14); !strings.Contains(status, "paused") {
		t.Fatalf("paused status: %q", status)
	}
	if body := rowText(painted, 11); strings.Contains(body, "> ") {
		t.Fatalf("marker left the tail while paused: %q", body)
	}
	if body := rowText(painted, 10); !strings.Contains(body, "> ") || !strings.Contains(body, "msg 11") {
		t.Fatalf("marker row: %q", body)
	}

	// Past the cap the status counts the dropped lines.
	app.appendBatch(make([]logEntry, 12)) // 24 total, cap 20: 4 dropped
	painted = paintOnce(t, app)
	if status := rowText(painted, 14); !strings.Contains(status, "+4 dropped") {
		t.Fatalf("drop count missing: %q", status)
	}
}

// TestLogsEmptyFilterPaintsHint checks the empty view tells the truth:
// a query that matches nothing is shown as such, not as a dead blank.
func TestLogsEmptyFilterPaintsHint(t *testing.T) {
	app := newUnitApp(100)
	paintOnce(t, app)
	app.appendBatch([]logEntry{entry("info", "msg 1", 0)})
	app.setFilter("zzz")

	painted := paintOnce(t, app)
	if body := rowText(painted, 3); !strings.Contains(body, `no lines match "zzz"`) {
		t.Fatalf("empty hint: %q", body)
	}
	if status := rowText(painted, 14); !strings.Contains(status, "0 match") {
		t.Fatalf("empty status: %q", status)
	}

	// Before the first line arrives, the view says it is waiting.
	app2 := newUnitApp(100)
	painted = paintOnce(t, app2)
	if body := rowText(painted, 3); !strings.Contains(body, "waiting for lines") {
		t.Fatalf("waiting hint: %q", body)
	}
}

func TestLogsTinyTerminalGuards(t *testing.T) {
	app := newUnitApp(100)
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	if painted := frame.Canvas.Buffer(); !strings.Contains(rowText(painted, 0), "enlarge terminal") {
		t.Fatalf("tiny guard missing: %q", rowText(painted, 0))
	}
}

func typeRunes(backend *interactiveBackend, text string) {
	for _, r := range text {
		backend.events <- event.RuneKey(r, 0)
	}
}

type interactiveBackend struct {
	events chan event.Event
	frames chan *render.Buffer
}

func (b *interactiveBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}
func (b *interactiveBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}
func (b *interactiveBackend) Present(_ context.Context, frame *render.Buffer) error {
	select {
	case b.frames <- frame.Clone():
	default:
	}
	return nil
}
func (b *interactiveBackend) Size() (int, int, error) { return 64, 16, nil }
func (*interactiveBackend) Close() error              { return nil }

// TestLogsEndToEndStreamScrollFilterQuit runs the app against a backend
// whose source the test controls: batches arrive between user actions,
// proving the follow/pause interplay under the real event loop.
func TestLogsEndToEndStreamScrollFilterQuit(t *testing.T) {
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	source := newChanSource()
	app := newLogsDemo(30, source, 5*time.Millisecond)
	app.observe = make(chan logsState, 64)
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)

	// The first resize starts the stream; the first batch lands and the
	// view follows the tail.
	source.batches <- []logEntry{
		entry("info", "boot 1", 0), entry("warn", "boot 2", 1), entry("info", "boot 3", 2),
		entry("error", "boot 4", 3), entry("info", "boot 5", 4), entry("warn", "boot 6", 5),
	}
	waitStateWhere(t, app.observe, done, func(state logsState) bool {
		return state.lines == 6 && state.follow
	})

	// The state above arrived from inside the first View, before the
	// writer's present callbacks committed the region's focus: ui.Context
	// routes events against the last successfully presented snapshot, so a
	// keystroke that beats the first commit is dropped on an empty focus
	// table — the 2-in-4 CI lottery this suite already lost twice. An F5
	// press (bound nowhere here) forces a repaint, and the writer is
	// sequential: a second frame on the wire proves the first frame's
	// commit ran.
	backend.events <- event.KeyPress(event.KeyF5, 0)
	waitFrame(t, backend.frames)

	// One scroll up pauses the follow. Six lines fit one page, so the
	// offset is already 0 and stays there.
	backend.events <- event.KeyPress(event.KeyUp, 0)
	state := waitStateWhere(t, app.observe, done, func(state logsState) bool {
		return !state.follow && state.selected == 4 && state.offset == 0
	})

	// A batch arrives while paused: the same rows stay under the cursor.
	source.batches <- []logEntry{entry("info", "tick 7", 6), entry("error", "boom 8", 7)}
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return next.lines == 8 && !next.follow && next.selected == state.selected && next.offset == state.offset
	})

	// A body click selects the line under the pointer. Body starts at
	// absolute y 2 with offset 0, so screen y 4 is view index 2.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 12, Y: 4}}
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return next.selected == 2
	})

	// End pins the cursor back on the newest line and resumes the follow.
	backend.events <- event.KeyPress(event.KeyEnd, 0)
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return next.follow && next.selected == 7 && next.offset == 0
	})

	// '/' jumps to the filter from the log view without a Tab cycle.
	backend.events <- event.RuneKey('/', 0)
	waitStateWhere(t, app.observe, done, func(next logsState) bool { return next.filterFocus })

	// Typing narrows the view incrementally.
	typeRunes(backend, "error")
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return next.filter == "error" && next.lines == 2
	})

	// Enter commits the query and hands focus back to the log view.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return !next.filterFocus && next.lines == 2 && next.selected == 1
	})

	// A frame exists for the committed filter state.
	frame := waitFrameWhere(t, backend.frames, func(frame *render.Buffer) bool {
		return strings.Contains(rowText(frame, 1), "error")
	})
	if status := rowText(frame, 14); !strings.Contains(status, "2 match") {
		t.Fatalf("filter status: %q", status)
	}

	// Escape over a live query clears the filter instead of quitting; the
	// follow is still on, so the full view pins its tail.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, done, func(next logsState) bool {
		return next.filter == "" && next.lines == 8 && next.follow && next.selected == 7
	})

	// A bare Escape on an empty query is the advertised quit.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
}

func waitStateWhere(t *testing.T, states <-chan logsState, done <-chan error, match func(logsState) bool) logsState {
	t.Helper()
	deadline := time.After(hangTimeout)
	var last logsState
	seen := false
	for {
		select {
		case state := <-states:
			last, seen = state, true
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			detail := "no state observed"
			if seen {
				detail = fmt.Sprintf("last state %+v", last)
			}
			t.Fatalf("matching state timeout (%s); goroutine dump follows\n%s", detail, stack)
		}
	}
}

func waitFrame(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		t.Fatal("frame timeout")
		return nil
	}
}

// waitFrameWhere drains frames until one satisfies check. Events queue one
// frame each, so the frame for a state change sits behind stale frames from
// earlier steps; a plain waitFrame would assert against the past.
func waitFrameWhere(t *testing.T, frames <-chan *render.Buffer, check func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	deadline := time.After(hangTimeout)
	for {
		select {
		case frame := <-frames:
			if check(frame) {
				return frame
			}
		case <-deadline:
			t.Fatal("frame condition never painted")
			return nil
		}
	}
}
