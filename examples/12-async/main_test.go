package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// The painted surface is 64x22: wide enough for the full chrome, narrow
// enough that the layout is provable. These row constants pin the
// contract the View paints — the header lives at headerRow in every
// phase, the gauge at gaugeRow, the button at buttonRow — so the
// layout-stability assertions below are pixel-level, not vibes.
const (
	paintWidth  = 64
	paintHeight = 22

	statusRow = 1
	headerRow = 3
	bodyRow   = 4
	gaugeRow  = 15
	buttonRow = 17
	noticeRow = 20
	footerRow = 21

	hangTimeout = 45 * time.Second
)

// rowText joins one row of a painted buffer back into a string, so a
// test can assert on visible words without caring where exactly in the
// row they landed.
func rowText(buffer *render.Buffer, y int) string {
	var b strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		b.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return b.String()
}

// scriptChunk and scriptStep are the scripted source's answers: the
// test sends them at its own pace, so no timing budget can flake.
type scriptChunk struct {
	rows []feedRow
	done bool
	err  error
}

type scriptStep struct {
	done  int
	total int
	err   error
}

// scriptedFeedSource answers from channels and never sleeps: the test
// paces every call. The counters increment before the blocking select,
// so a count is a proof the call entered, not that it returned.
type scriptedFeedSource struct {
	chunks chan scriptChunk
	steps  chan scriptStep

	mu         sync.Mutex
	feedCalls  int
	indexCalls int
}

func (s *scriptedFeedSource) Feeds(ctx context.Context, req int) ([]feedRow, bool, error) {
	s.mu.Lock()
	s.feedCalls++
	s.mu.Unlock()
	select {
	case chunk := <-s.chunks:
		return chunk.rows, chunk.done, chunk.err
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

func (s *scriptedFeedSource) Index(ctx context.Context, job int) (int, int, error) {
	s.mu.Lock()
	s.indexCalls++
	s.mu.Unlock()
	select {
	case step := <-s.steps:
		return step.done, step.total, step.err
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	}
}

func (s *scriptedFeedSource) calls() (feeds, indexes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feedCalls, s.indexCalls
}

// interactiveBackend is the 10-form harness: a raw event channel in, a
// cloned frame channel out, a fixed 64x22 size, no real terminal.
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
		return nil
	default:
		return nil
	}
}

func (b *interactiveBackend) Size() (int, int, error) { return paintWidth, paintHeight, nil }

func (b *interactiveBackend) Close() error { return nil }

// waitFrame reads the next painted frame or fails with the goroutine
// dump: a hung wait is the failure, never a skip.
func waitFrame(t *testing.T, frames chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("timed out waiting for a frame; goroutines:\n%s", buf[:n])
		return nil
	}
}

// drainFrames empties the frame backlog so the next read is the state
// the test just provoked, not history.
func drainFrames(frames chan *render.Buffer) {
	for {
		select {
		case <-frames:
		default:
			return
		}
	}
}

// waitCommittedWhere waits for a state whose tree is already committed
// — the beacon fires strictly after the writer presented the frame
// built from that state, so input routed after this gate provably
// routes against this state. A timeout dumps the goroutines.
func waitCommittedWhere(t *testing.T, app *feedsApp, done <-chan error, pred func(feedsState) bool) feedsState {
	t.Helper()
	select {
	case state := <-app.committed:
		if pred(state) {
			return state
		}
		return waitCommittedWhere(t, app, done, pred)
	case err := <-done:
		t.Fatalf("run ended early: %v", err)
		return feedsState{}
	case <-time.After(hangTimeout):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("timed out waiting for committed state; goroutines:\n%s", buf[:n])
		return feedsState{}
	}
}

// paintWhere drains the backlog, provokes a repaint with F6 (bound
// nowhere), and waits until a frame matching pred arrives. The app
// paints on demand: one F6 yields one frame, and a frame whose content
// never changes cannot be waited into matching, so each attempt sends
// its own F6 and a budget of repaints is spent before failing. The
// failure dumps the last painted rows, because "the frame shows
// something else" is the only honest diagnosis of a pred miss.
// Paint assert only: it proves content reaches the wire, never that the
// tree is committed — input routing gates on the committed beacon above.
func paintWhere(t *testing.T, backend *interactiveBackend, pred func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	const attempts = 4
	var last *render.Buffer
	for attempt := 0; attempt < attempts; attempt++ {
		drainFrames(backend.frames)
		backend.events <- event.KeyPress(event.KeyF6, 0)
		last = waitFrame(t, backend.frames)
		if pred(last) {
			return last
		}
	}
	var dump strings.Builder
	for y := statusRow; y <= footerRow && y < last.Bounds().H; y++ {
		fmt.Fprintf(&dump, "row %2d: %q\n", y, rowText(last, y))
	}
	t.Fatalf("painted frame never matched after %d repaints; last frame rows:\n%s", attempts, dump.String())
	return nil
}

func startFeeds(t *testing.T, source feedSource) (*feedsApp, *interactiveBackend, <-chan error) {
	t.Helper()
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newFeedsApp(source, make(chan feedsState, 64), make(chan feedsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)
	return app, backend, done
}

// TestFeedsLifecycleStreamsStaleCancelsAndReplays walks the whole async
// story through the real run loop: the first load opens on the first
// resize, chunks stream in (skeleton, partial, done), a refresh keeps
// the previous answer on screen while marked stale, 'x' cancels the
// flight, a second 'r' during an open flight defers instead of racing,
// and the deferred reload replays the moment the open one lands.
func TestFeedsLifecycleStreamsStaleCancelsAndReplays(t *testing.T) {
	source := &scriptedFeedSource{chunks: make(chan scriptChunk, 8), steps: make(chan scriptStep, 8)}
	app, backend, done := startFeeds(t, source)

	// The first resize starts the first load: skeleton phase, nothing
	// on screen yet but the skeleton itself.
	state := waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0 && !s.stale
	})
	if state.errText != "" {
		t.Fatalf("no error before any answer: %+v", state)
	}
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, headerRow), "FEED") &&
			strings.Contains(rowText(p, bodyRow), "loading") &&
			strings.Contains(rowText(p, bodyRow), "░")
	})

	// First chunk: partial. Rows grow under the same header.
	source.chunks <- scriptChunk{rows: demoFeedRows[:2], done: false}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 2 && s.pending == 2 && !s.stale
	})
	if state.errText != "" || state.focused != "table" {
		t.Fatalf("partial state wrong: %+v", state)
	}
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "builds") &&
			strings.Contains(rowText(p, statusRow), "partial")
	})

	// Second chunk: done. The answer is fresh, the flight closed.
	source.chunks <- scriptChunk{rows: demoFeedRows[2:4], done: true}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.loaded && s.rows == 4 && !s.stale && s.pending == 0
	})
	if state.errText != "" {
		t.Fatalf("fresh state must be error-free: %+v", state)
	}
	paintWhere(t, backend, func(p *render.Buffer) bool {
		// demoFeedRows[2] is "logs", the third body row: after a done
		// chunk the table shows the full four-row answer.
		return strings.Contains(rowText(p, bodyRow+2), "logs") &&
			strings.Contains(rowText(p, statusRow), "fresh")
	})

	// 'r' refreshes: the old answer stays on screen, marked stale.
	backend.events <- event.RuneKey('r', 0)
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.stale && s.rows == 4
	})
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "builds") &&
			strings.Contains(rowText(p, statusRow), "stale")
	})

	// 'x' cancels: the flight closes with the cancelled verdict, the
	// old answer is the current answer again.
	backend.events <- event.RuneKey('x', 0)
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 4 && !s.stale && strings.Contains(s.notice, "cancelled")
	})
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, statusRow), "fresh") &&
			!strings.Contains(rowText(p, statusRow), "stale")
	})

	// A second 'r' while a flight is open defers: same flight, no new
	// Feeds call. F6 forces a repaint so the still-loading beacon is
	// fresh, not backlog.
	backend.events <- event.RuneKey('r', 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.stale && s.rows == 4
	})
	backend.events <- event.RuneKey('r', 0)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.stale && s.rows == 4
	})
	// Feeds calls so far: initial load, its streaming continuation,
	// first refresh, second refresh. The deferred press adds none.
	if feeds, _ := source.calls(); feeds != 4 {
		t.Fatalf("deferred reload must not open a second flight: %d Feeds calls", feeds)
	}

	// The refresh completes; the deferred one replays inside the same
	// turn — the wire never paints the intermediate fresh state, the
	// verdict goes straight from stale to stale again.
	source.chunks <- scriptChunk{rows: demoFeedRows[:6], done: true}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.stale && s.rows == 6
	})
	// The replay is the fifth call: initial, continuation, two
	// refreshes, and this one.
	if feeds, _ := source.calls(); feeds != 5 {
		t.Fatalf("the deferred reload must have replayed: %d Feeds calls", feeds)
	}

	// The replayed load lands: fresh, and nothing further was issued.
	source.chunks <- scriptChunk{rows: demoFeedRows[:6], done: true}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 6 && !s.stale && s.errText == ""
	})
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 6 && !s.stale
	})
	if feeds, _ := source.calls(); feeds != 5 {
		t.Fatalf("no further load may run after the replay: %d Feeds calls", feeds)
	}
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "builds") &&
			strings.Contains(rowText(p, statusRow), "fresh")
	})

	// Escape quits the run cleanly; the root cancellation unblocks any
	// worker still parked on a channel.
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

// TestFeedsEmptyThenFailureThenButtonRetry proves the empty and error
// states are real, painted, and recoverable — and that the retry path
// works through the mouse on the always-registered Reload button, not
// only through the keyboard.
func TestFeedsEmptyThenFailureThenButtonRetry(t *testing.T) {
	source := &scriptedFeedSource{chunks: make(chan scriptChunk, 8), steps: make(chan scriptStep, 8)}
	app, backend, done := startFeeds(t, source)

	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0
	})
	// A done-but-empty answer: the empty state, with its own hint.
	source.chunks <- scriptChunk{done: true}
	state := waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.loaded && s.rows == 0
	})
	if state.errText != "" {
		t.Fatalf("empty is not an error: %+v", state)
	}
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "no feeds") &&
			strings.Contains(rowText(p, bodyRow), "Reload") &&
			strings.Contains(rowText(p, statusRow), "empty")
	})

	// The reload fails: the error is on screen, the retry hint with it.
	backend.events <- event.RuneKey('r', 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0 && !s.stale
	})
	source.chunks <- scriptChunk{err: errors.New("http 503")}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 0 && strings.Contains(s.errText, "503")
	})
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "load failed") &&
			strings.Contains(rowText(p, bodyRow+1), "retries") &&
			strings.Contains(rowText(p, statusRow), "error")
	})

	// Retry through the button: a mouse press focuses it, the release
	// activates it, and only the release reaches the loader.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 8, Y: buttonRow + 1}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 8, Y: buttonRow + 1}}
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0 && s.errText == ""
	})

	// The retry succeeds: rows land, the error is gone.
	source.chunks <- scriptChunk{rows: demoFeedRows[:3], done: true}
	state = waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 3 && s.errText == "" && s.loaded
	})
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "builds") &&
			strings.Contains(rowText(p, statusRow), "fresh")
	})

	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
	if feeds, _ := source.calls(); feeds != 3 {
		t.Fatalf("Feeds calls = %d; want empty, failure, retry", feeds)
	}
}

// TestFeedsProgressJobCompletesAndCancels walks the long-running job:
// 'p' starts it, steps fill the gauge at its fixed slot, completion
// clears the slot and leaves a notice, and 'x' mid-job cancels it with
// the cancelled verdict.
func TestFeedsProgressJobCompletesAndCancels(t *testing.T) {
	source := &scriptedFeedSource{chunks: make(chan scriptChunk, 8), steps: make(chan scriptStep, 8)}
	app, backend, done := startFeeds(t, source)

	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0
	})
	source.chunks <- scriptChunk{rows: demoFeedRows[:2], done: true}
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 2
	})

	// 'p' starts the job: the progress slot fills.
	backend.events <- event.RuneKey('p', 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.jobRunning
	})
	source.steps <- scriptStep{done: 2, total: 5}
	state := waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.jobRunning && s.jobDone == 2 && s.jobTotal == 5
	})
	if !state.loading {
		// the job runs while the loaded answer stays on screen
		paintWhere(t, backend, func(p *render.Buffer) bool {
			return strings.Contains(rowText(p, gaugeRow), "indexing") &&
				strings.Contains(rowText(p, gaugeRow), "40%") &&
				strings.Contains(rowText(p, gaugeRow), "█")
		})
	}

	// The job completes: the slot clears, the notice reports.
	source.steps <- scriptStep{done: 5, total: 5}
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.jobRunning && s.jobDone == 5 && strings.Contains(s.notice, "index rebuilt")
	})
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return !strings.Contains(rowText(p, gaugeRow), "indexing")
	})

	// Restart and cancel: 'x' mid-job leaves the cancelled verdict.
	backend.events <- event.RuneKey('p', 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.jobRunning && s.jobDone == 0
	})
	source.steps <- scriptStep{done: 1, total: 5}
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.jobRunning && s.jobDone == 1
	})
	backend.events <- event.RuneKey('x', 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.jobRunning && strings.Contains(s.notice, "index cancelled")
	})

	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
	if _, indexes := source.calls(); indexes != 4 {
		t.Fatalf("Index calls = %d; want the start, the step chains, and the restart", indexes)
	}
}

// TestFeedsTableSelectsSortsAndReads proves the table's keyboard and
// mouse paths against the committed tree: arrow selection, Enter
// reading the selected feed, and header clicks sorting in both
// directions.
func TestFeedsTableSelectsSortsAndReads(t *testing.T) {
	source := &scriptedFeedSource{chunks: make(chan scriptChunk, 8), steps: make(chan scriptStep, 8)}
	app, backend, done := startFeeds(t, source)

	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.loading && s.rows == 0
	})
	source.chunks <- scriptChunk{rows: demoFeedRows[:4], done: true}
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return !s.loading && s.rows == 4 && s.selected == 0
	})

	// Down selects the second row; Enter reads it.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return s.selected == 1
	})
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitCommittedWhere(t, app, done, func(s feedsState) bool {
		return strings.Contains(s.notice, "reading releases")
	})

	// A header click sorts ascending by name: builds, errors, logs,
	// releases — and, because a header click is a sort rather than a
	// read, the notice from Enter survives it untouched.
	clickHeader := func() {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 4, Y: headerRow}}
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 4, Y: headerRow}}
	}
	clickHeader()
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "builds") &&
			strings.Contains(rowText(p, bodyRow+1), "errors") &&
			strings.Contains(rowText(p, noticeRow), "reading releases")
	})

	// A second click flips to descending: releases, logs, errors,
	// builds — and the sort still never re-reads the selection.
	clickHeader()
	paintWhere(t, backend, func(p *render.Buffer) bool {
		return strings.Contains(rowText(p, bodyRow), "releases") &&
			strings.Contains(rowText(p, bodyRow+1), "logs") &&
			strings.Contains(rowText(p, noticeRow), "reading releases")
	})

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

// TestFeedsPaintsEveryPhaseWithoutLayoutJump drives the model directly
// (the run-loop tests above prove routing; this one proves pixels) and
// pins the layout contract: the header, gauge slot, button slot, and
// footer hold their rows across skeleton, partial, ready, stale,
// failure, and progress phases.
func TestFeedsPaintsEveryPhaseWithoutLayoutJump(t *testing.T) {
	app := newFeedsApp(nil, nil, nil)
	app.width, app.height = paintWidth, paintHeight
	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(paintWidth, paintHeight)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}
	assertChrome := func(painted *render.Buffer, phase string) {
		t.Helper()
		for row, want := range map[int]string{
			headerRow: "FEED",
			// The reload button's rect starts at buttonRow; its label
			// sits on the middle row of the three-row button.
			buttonRow + 1: "Reload",
			// At 64 columns the full shortcut list does not fit, so the
			// footer degrades to the minimal escape hint; the row
			// itself never moves.
			footerRow: "esc quit",
		} {
			if !strings.Contains(rowText(painted, row), want) {
				t.Fatalf("%s: row %d missing %q: %q", phase, row, want, rowText(painted, row))
			}
		}
	}

	// Skeleton: the header is already where data will land under it.
	app.issueLoad()
	skeleton := paint()
	assertChrome(skeleton, "skeleton")
	if !strings.Contains(rowText(skeleton, statusRow), "loading") {
		t.Fatalf("skeleton status: %q", rowText(skeleton, statusRow))
	}
	if !strings.Contains(rowText(skeleton, bodyRow), "░") || !strings.Contains(rowText(skeleton, bodyRow), "loading") {
		t.Fatalf("skeleton body: %q", rowText(skeleton, bodyRow))
	}
	if strings.Contains(rowText(skeleton, bodyRow), "builds") {
		t.Fatalf("data painted during skeleton: %q", rowText(skeleton, bodyRow))
	}

	// Partial: rows grow under the same header, same rows.
	app.applyChunk(chunkResult{id: 1, rows: demoFeedRows[:2], done: false})
	partial := paint()
	assertChrome(partial, "partial")
	if !strings.Contains(rowText(partial, bodyRow), "builds") || !strings.Contains(rowText(partial, bodyRow+1), "releases") {
		t.Fatalf("partial body: %q / %q", rowText(partial, bodyRow), rowText(partial, bodyRow+1))
	}
	if !strings.Contains(rowText(partial, statusRow), "partial") {
		t.Fatalf("partial status: %q", rowText(partial, statusRow))
	}

	// Ready: full answer, fresh verdict.
	app.applyChunk(chunkResult{id: 1, rows: demoFeedRows[2:4], done: true})
	ready := paint()
	assertChrome(ready, "ready")
	if !strings.Contains(rowText(ready, bodyRow+2), "logs") {
		t.Fatalf("ready body: %q", rowText(ready, bodyRow+2))
	}
	if !strings.Contains(rowText(ready, statusRow), "fresh") {
		t.Fatalf("ready status: %q", rowText(ready, statusRow))
	}

	// Stale refresh: the previous answer stays on screen.
	app.issueLoad()
	stale := paint()
	assertChrome(stale, "stale")
	if !strings.Contains(rowText(stale, bodyRow), "builds") || !strings.Contains(rowText(stale, statusRow), "stale") {
		t.Fatalf("stale: %q / %q", rowText(stale, bodyRow), rowText(stale, statusRow))
	}

	// The refresh fails: the rows survive, the error is on the notice
	// row, not replacing the table.
	app.applyChunk(chunkResult{id: 2, err: errors.New("http 503")})
	failed := paint()
	assertChrome(failed, "failed")
	if !strings.Contains(rowText(failed, bodyRow), "builds") {
		t.Fatalf("failed body must keep the rows: %q", rowText(failed, bodyRow))
	}
	if !strings.Contains(rowText(failed, statusRow), "error") || !strings.Contains(rowText(failed, noticeRow), "503") {
		t.Fatalf("failed verdict: %q / %q", rowText(failed, statusRow), rowText(failed, noticeRow))
	}

	// Progress: the gauge slot fills at its fixed row, then clears.
	app.startIndex()
	running := paint()
	assertChrome(running, "indexing")
	if !strings.Contains(rowText(running, gaugeRow), "indexing") {
		t.Fatalf("indexing row: %q", rowText(running, gaugeRow))
	}
	app.applyJob(jobResult{id: 1, done: 3, total: 5})
	gauged := paint()
	assertChrome(gauged, "gauged")
	if !strings.Contains(rowText(gauged, gaugeRow), "60%") || !strings.Contains(rowText(gauged, gaugeRow), "█") {
		t.Fatalf("gauged row: %q", rowText(gauged, gaugeRow))
	}
	app.applyJob(jobResult{id: 1, done: 5, total: 5})
	built := paint()
	assertChrome(built, "rebuilt")
	if strings.Contains(rowText(built, gaugeRow), "indexing") {
		t.Fatalf("gauge row must clear: %q", rowText(built, gaugeRow))
	}
	if !strings.Contains(rowText(built, noticeRow), "index rebuilt") {
		t.Fatalf("rebuilt notice: %q", rowText(built, noticeRow))
	}
}

// TestFeedsTinyTerminalGuard pins the degrade path: below the layout
// budget the app paints the enlarge hint instead of a broken tree.
func TestFeedsTinyTerminalGuard(t *testing.T) {
	app := newFeedsApp(nil, nil, nil)
	app.width, app.height = 30, 8
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	painted := rowText(frame.Canvas.Buffer(), 0)
	if !strings.Contains(painted, "enlarge terminal") {
		t.Fatalf("tiny terminal hint missing: %q", painted)
	}
}
