package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
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

// seedProcs builds a deterministic process table: four name patterns with
// stable pids, load, memory, and state, so filter counting is exact.
func seedProcs(count int) []processRow {
	rows := make([]processRow, count)
	patterns := []string{"relay", "node", "cache", "worker"}
	for i := range rows {
		rows[i] = processRow{
			pid:   1000 + i,
			name:  fmt.Sprintf("%s-%02d", patterns[i%len(patterns)], i),
			cpu:   float64(i%97) / 4,
			mem:   int64(200_000 + 37*i),
			state: []string{"running", "sleep"}[i%2],
		}
	}
	return rows
}

// scriptStep is one scripted List answer: after the delay, the step yields
// its rows or its error.
type scriptStep struct {
	delay time.Duration
	rows  []processRow
	err   error
}

// scriptedSource is the deterministic stand-in for the operating system:
// steps are consumed in order (the last one repeats), the filter is
// applied exactly like the real sources apply it, every call's filter is
// logged, and cancellation during a delay is counted. Kill is scripted
// the same way: a delay, an error, and a log of the pids it saw.
type scriptedSource struct {
	mu        sync.Mutex
	steps     []scriptStep
	calls     int
	filterLog []string
	killDelay time.Duration
	killErr   error
	kills     []int
	cancelled atomic.Int32
}

func newScriptedSource(steps ...scriptStep) *scriptedSource {
	return &scriptedSource{steps: steps}
}

func (s *scriptedSource) List(ctx context.Context, filter string) ([]processRow, error) {
	s.mu.Lock()
	s.filterLog = append(s.filterLog, filter)
	step := s.steps[min(s.calls, len(s.steps)-1)]
	s.calls++
	s.mu.Unlock()
	if step.delay > 0 {
		timer := time.NewTimer(step.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			s.cancelled.Add(1)
			return nil, ctx.Err()
		}
	}
	if step.err != nil {
		return nil, step.err
	}
	rows := make([]processRow, 0, len(step.rows))
	for _, row := range step.rows {
		if matchesFilter(row.name, filter) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (s *scriptedSource) filters() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.filterLog...)
}

// Kill mirrors List: it honors the context through the delay, logs the
// pid, and answers with the configured error.
func (s *scriptedSource) Kill(ctx context.Context, pid int) error {
	s.mu.Lock()
	s.kills = append(s.kills, pid)
	s.mu.Unlock()
	if s.killDelay > 0 {
		timer := time.NewTimer(s.killDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			s.cancelled.Add(1)
			return ctx.Err()
		}
	}
	return s.killErr
}

func (s *scriptedSource) killList() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.kills...)
}

func (s *scriptedSource) cancellations() int {
	return int(s.cancelled.Load())
}

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

func TestProcsViewPaintsEveryPhase(t *testing.T) {
	full := seedProcs(40)
	app := newProcsApp(nil, nil)
	app.width, app.height = 80, 24

	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(80, 24)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	app.phase = phaseLoading
	if painted := paint(); !strings.Contains(rowText(painted, 3), "loading processes") {
		t.Fatalf("loading hint missing: %q", rowText(painted, 3))
	}

	app.phase = phaseEmpty
	app.filter = "zzz"
	if painted := paint(); !strings.Contains(rowText(painted, 3), `no processes match "zzz"`) {
		t.Fatalf("empty hint missing: %q", rowText(painted, 3))
	}

	app.phase = phaseError
	app.errText = "no route to host"
	if painted := paint(); !strings.Contains(rowText(painted, 3), "error: no route to host") {
		t.Fatalf("error text missing: %q", rowText(painted, 3))
	}

	app.phase = phaseData
	app.filter = ""
	app.rows = full
	app.rebuildOrder()
	painted := paint()
	if !strings.Contains(rowText(painted, 2), "PID") || !strings.Contains(rowText(painted, 2), "NAME") {
		t.Fatalf("table header missing: %q", rowText(painted, 2))
	}
	if !strings.Contains(rowText(painted, 3), "relay-00") {
		t.Fatalf("first row missing: %q", rowText(painted, 3))
	}
	if !strings.Contains(strings.Join([]string{rowText(painted, 17), rowText(painted, 18)}, "\n"), "name relay-00") {
		t.Fatalf("detail pane missing selected row: %q / %q", rowText(painted, 17), rowText(painted, 18))
	}
	if !strings.Contains(rowText(painted, 1), "(type to match names)") {
		t.Fatalf("filter placeholder missing: %q", rowText(painted, 1))
	}
	foundStatus, foundFooter := false, false
	for y := 14; y < 24; y++ {
		row := rowText(painted, y)
		foundStatus = foundStatus || strings.Contains(row, "data  40 rows")
		foundFooter = foundFooter || strings.Contains(row, "esc quit")
	}
	if !foundStatus || !foundFooter {
		t.Fatalf("status=%v footer=%v", foundStatus, foundFooter)
	}

	app.filter = "rel"
	painted = paint()
	if !strings.Contains(rowText(painted, 1), " filter: ") || strings.Contains(rowText(painted, 1), "(type to match names)") {
		t.Fatalf("typed filter missing: %q", rowText(painted, 1))
	}
}

func TestProcsTinyTerminalGuard(t *testing.T) {
	app := newProcsApp(nil, nil)
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	if painted := frame.Canvas.Buffer(); !strings.Contains(rowText(painted, 0), "enlarge terminal") {
		t.Fatalf("tiny guard missing: %q", rowText(painted, 0))
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

func waitStateWhere(t *testing.T, states <-chan procsState, done <-chan error, match func(procsState) bool) procsState {
	t.Helper()
	deadline := time.After(hangTimeout)
	seen := 0
	var last procsState
	for {
		select {
		case state := <-states:
			seen++
			last = state
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			// CI run 38099373704 (commit 1444a2d) timed out in this
			// helper with nothing but "want state" to show for it: no
			// count of what the app did report, no last state, and no
			// goroutine dump, so the stall could not be attributed
			// either way. The failure now carries the same evidence
			// the other interactive suites print.
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			t.Fatalf("matching state timeout after %d states; last: phase=%v rows=%d selected=%d confirm=%d filter=%q notice=%q err=%q; goroutine dump follows\n%s",
				seen, last.phase, last.rows, last.selected, last.confirm, last.filter, last.notice, last.errText, stack)
		}
	}
}

func waitFrame(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		stack := make([]byte, 1<<20)
		stack = stack[:runtime.Stack(stack, true)]
		t.Fatalf("frame timeout; goroutine dump follows\n%s", stack)
		return nil
	}
}

// drainFrames discards the frames that have already landed, so the
// next waitFrame starts from the backlog's edge — not from an old
// phase. It cannot discard frames still queued inside the writer: see
// waitFrameWhere for reads that must skip those.
func drainFrames(frames <-chan *render.Buffer) {
	for {
		select {
		case <-frames:
		default:
			return
		}
	}
}

// waitFrameWhere reads frames until one satisfies pred and returns it.
// The writer presents asynchronously and the channel is ordered by
// paint time, so a frame painted before a state change can land after
// a drain and before the frame the F6 repaint provoked — the first
// frame after a drain can still be stale. The model already holds the
// state the test observed, so every frame painted from the forced
// repaint on carries it; the loop keeps reading until one does. A
// wrong expectation hangs into waitFrame's dump, exactly like the
// single-shot waits did.
func waitFrameWhere(t *testing.T, frames <-chan *render.Buffer, pred func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	for {
		frame := waitFrame(t, frames)
		if pred(frame) {
			return frame
		}
	}
}

// commitFrames drains the frame backlog, provokes one repaint, and waits
// for a frame on the wire. The writer presents asynchronously and
// sequentially, so that frame's arrival proves every frame painted
// BEFORE it was presented and committed. That is a proof about the
// past: it is the right tool for first-commit gates (a keystroke that
// must not beat the first frame's focus/shortcut commit — the read
// frame is at-or-after the first frame). It proves NOTHING about
// frames painted after a state change: those may still be queued in
// the writer, so the read frame can predate the change and the next
// keystroke routes against a dead snapshot. A keystroke that must
// route against the NEW state needs commitFrameWhere below. F6 is
// bound nowhere, and every event dirties the model, so the press
// repaints without touching any state under test.
func commitFrames(t *testing.T, backend *interactiveBackend) *render.Buffer {
	t.Helper()
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	return waitFrame(t, backend.frames)
}

// commitFrameWhere is commitFrames as a state gate: it keeps reading
// until a frame whose PAINTED content satisfies pred, so the next
// keystroke routes against a snapshot committed from the state the
// pred recognizes. waitStateWhere observes state from inside View —
// before the writer presents and commits — and the single read in
// commitFrames can land on a frame painted before the change (an
// 'n' typed after a palette-run landed in the just-closed palette's
// input; an Escape dismissed the already-closed modal instead of
// quitting; a click fell through a still-Disabled button). The F6
// frame is painted from the current model, which already holds the
// target state, so the loop always terminates; a wrong expectation
// hangs into waitFrame's dump like the single-shot waits did.
func commitFrameWhere(t *testing.T, backend *interactiveBackend, pred func(painted *render.Buffer) bool) *render.Buffer {
	t.Helper()
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	for {
		painted := waitFrame(t, backend.frames)
		if pred(painted) {
			return painted
		}
	}
}

func TestProcsEndToEndFilterErrorRetryQuit(t *testing.T) {
	full := seedProcs(40)
	source := newScriptedSource(
		scriptStep{delay: 40 * time.Millisecond, rows: full}, // initial load, empty filter
		scriptStep{delay: 60 * time.Millisecond, rows: full}, // "r" load, typed during flight
		scriptStep{delay: 30 * time.Millisecond, rows: full}, // coalesced replay of "rel"
		scriptStep{delay: 30 * time.Millisecond, err: errors.New("list unavailable")},
		scriptStep{delay: 30 * time.Millisecond, rows: full}, // retry with "rel"
	)
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseData && state.rows == 40 })

	// The state above arrived from inside the first View, before the
	// writer's present callbacks committed the table's focus: a
	// keystroke that beats the first commit is dropped on an empty focus
	// table. F5 is refresh here, so F6 (bound nowhere) forces the
	// repaint, and the writer is sequential: a second frame on the wire
	// proves the first frame's commit ran.
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrame(t, backend.frames)

	// Typing "rel": the first keystroke issues a load, the next two
	// coalesce behind it, and the replay lands the filtered set.
	backend.events <- event.RuneKey('r', 0)
	backend.events <- event.RuneKey('e', 0)
	backend.events <- event.RuneKey('l', 0)
	final := waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.phase == phaseData && state.rows == 10 && state.filter == "rel"
	})
	if final.detail == "" || !strings.Contains(final.detail, "relay") {
		t.Fatalf("selected detail = %q; want a relay row", final.detail)
	}

	// A forced refresh fails: the error phase renders, the run survives.
	backend.events <- event.KeyPress(event.KeyF5, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.phase == phaseError && state.errText == "list unavailable"
	})
	// The frames channel holds a backlog of phase transitions; drain it,
	// then force one repaint (F6 is bound nowhere, but every event
	// dirties the model) — the next frame is painted while the failure
	// is showing, proving input still flows after a recoverable error.
	// Frames still queued in the writer can predate the failure, so the
	// read skips them and lands on the error frame itself.
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, 3), "error: list unavailable")
	})

	// Retry recovers into the same filtered view.
	backend.events <- event.KeyPress(event.KeyF5, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.phase == phaseData && state.rows == 10 && state.filter == "rel"
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

	// The seam carried the query: the source saw the empty initial filter
	// and the typed one, every time a load ran.
	filters := source.filters()
	if len(filters) < 3 || filters[0] != "" {
		t.Fatalf("filter log = %q; want the empty initial filter first", filters)
	}
	for _, want := range filters[2:] {
		if want != "rel" {
			t.Fatalf("filter log = %q; want only \"rel\" after typing", filters)
		}
	}
}

func TestProcsEndToEndKillConfirmCancelAndExecute(t *testing.T) {
	full := seedProcs(40)
	after := seedProcs(39)
	source := newScriptedSource(
		scriptStep{delay: 40 * time.Millisecond, rows: full},  // initial load, empty filter
		scriptStep{delay: 30 * time.Millisecond, rows: after}, // post-kill auto-refresh
	)
	source.killDelay = 30 * time.Millisecond
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseData && state.rows == 40 })

	// The keystroke must land after the table's focus commit: F6 forces a
	// repaint and the writer is sequential, so a second frame on the wire
	// proves the first frame's present callbacks ran.
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrame(t, backend.frames)

	// Enter on the focused table asks about the selected row (relay-00,
	// pid 1000 in the seed).
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	confirmState := waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.confirm == 1000 })
	if confirmState.phase != phaseData || confirmState.filter != "" {
		t.Fatalf("confirm state: phase=%v filter=%q", confirmState.phase, confirmState.filter)
	}
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted := waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		line := rowText(painted, 1)
		return strings.Contains(line, "kill pid 1000") && strings.Contains(line, "y confirm")
	})
	if strings.Contains(rowText(painted, 1), "filter") {
		t.Fatalf("confirm line must replace the filter line: %q", rowText(painted, 1))
	}

	// While the ask is up, an unrelated rune is swallowed: it types
	// nothing into the filter and does not answer the ask.
	backend.events <- event.RuneKey('x', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.confirm == 1000 && state.filter == ""
	})

	// 'n' cancels the ask; the data is untouched.
	backend.events <- event.RuneKey('n', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.confirm == 0 && state.filter == "" && state.rows == 40
	})

	// Enter re-asks, and Escape answers the ask — it does not exit the app.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.confirm == 1000 })
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.confirm == 0 && state.rows == 40
	})

	// The third ask is answered with 'y': the kill runs, the notice reports
	// the in-flight state, then the outcome — and a success refreshes the
	// data, so the gone row disappears from the table on its own.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.confirm == 1000 })
	backend.events <- event.RuneKey('y', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.confirm == 0 && strings.Contains(state.notice, "terminating pid 1000")
	})
	final := waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.phase == phaseData && state.rows == 39 && strings.Contains(state.notice, "terminated pid 1000")
	})
	if final.filter != "" {
		t.Fatalf("filter = %q; the confirm dialog must leave it untouched", final.filter)
	}
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, 14), "terminated pid 1000")
	})

	// With no ask on screen, Escape exits the run.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}

	// Exactly one kill was issued, for the confirmed pid, despite the
	// cancel-and-reask dance above.
	if kills := source.killList(); len(kills) != 1 || kills[0] != 1000 {
		t.Fatalf("kill log = %v; want one kill of pid 1000", kills)
	}
}

func TestProcsActionFailureKeepsDataAndBoundsDoubleFire(t *testing.T) {
	source := newScriptedSource(scriptStep{rows: seedProcs(8)})
	source.killErr = errors.New("access denied")
	source.killDelay = 80 * time.Millisecond
	app := newProcsApp(source, nil)
	app.width, app.height = 80, 24

	load := app.Update(event.ResizeTo(80, 24))
	app.Update(load(context.Background()))
	if app.phase != phaseData || len(app.order) != 8 {
		t.Fatalf("setup: phase=%v rows=%d", app.phase, len(app.order))
	}

	// The activation hook is the routed-Activate seam: a keyboard
	// activation opens the ask, a pointer activation only selects.
	app.applyTable(ui.Action{Kind: ui.Activate, Event: event.KeyPress(event.KeyEnter, 0), Bounds: render.Rect{W: 60, H: 10}})
	if app.confirmRow.pid != 1000 {
		t.Fatalf("confirm pid = %d; want 1000", app.confirmRow.pid)
	}
	// A second activation while the ask is up re-captures nothing.
	app.applyTable(ui.Action{Kind: ui.Activate, Event: event.RuneKey(' ', 0), Bounds: render.Rect{W: 60, H: 10}})
	if app.confirmRow.pid != 1000 {
		t.Fatalf("re-activation changed the target: pid=%d", app.confirmRow.pid)
	}

	// 'y' executes: single-flight state and the in-flight notice.
	if kill := app.Update(event.RuneKey('y', 0)); kill == nil {
		t.Fatal("y on a confirm must issue the kill command")
	}
	if !app.actionInflight || app.confirmRow.pid != 0 {
		t.Fatalf("action state: inflight=%v confirm=%d", app.actionInflight, app.confirmRow.pid)
	}
	if !strings.Contains(app.notice, "terminating pid 1000") {
		t.Fatalf("in-flight notice = %q", app.notice)
	}
	// A second kill while one runs is refused outright.
	if second := app.kill(app.rows[app.order[1]]); second != nil {
		t.Fatal("double fire: a second kill while one is in flight was issued")
	}

	// The action result is a Message payload with its own shape.
	result := app.Update(event.Event{Kind: event.Message, Data: actionResult{pid: 1000, err: errors.New("access denied")}})
	if result != nil {
		t.Fatal("a failed kill must not trigger the auto-refresh")
	}
	if app.actionInflight {
		t.Fatal("the action was not marked done")
	}
	if app.phase != phaseData || len(app.order) != 8 {
		t.Fatalf("failure must keep the data: phase=%v rows=%d", app.phase, len(app.order))
	}
	if !strings.Contains(app.notice, "failed: access denied") {
		t.Fatalf("failure notice = %q", app.notice)
	}
	// A late or foreign action result cannot overwrite the notice.
	if foreign := app.Update(event.Event{Kind: event.Message, Data: actionResult{pid: 12345, err: nil}}); foreign != nil {
		t.Fatal("a foreign action result must be ignored, not acted on")
	}
	if !strings.Contains(app.notice, "failed: access denied") {
		t.Fatalf("foreign action result overwrote the notice: %q", app.notice)
	}

	// The pointer-activation guard: a mouse Activate on the table selects,
	// it does not open the kill ask.
	app.applyTable(ui.Action{Kind: ui.Activate, Event: event.Event{Kind: event.Mouse}, Bounds: render.Rect{W: 60, H: 10}})
	if app.confirmRow.pid != 0 {
		t.Fatalf("pointer activation opened the ask: pid=%d", app.confirmRow.pid)
	}
	// And the app still serves input after the failure.
	if retry := app.Update(event.KeyPress(event.KeyF5, 0)); retry == nil {
		t.Fatal("refresh after an action failure must issue a load")
	}
}

func TestProcsViewPaintsConfirmAndNotice(t *testing.T) {
	app := newProcsApp(nil, nil)
	app.width, app.height = 80, 24
	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(80, 24)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	app.phase = phaseData
	app.rows = seedProcs(40)
	app.rebuildOrder()
	app.confirmRow = app.rows[app.order[0]]
	painted := paint()
	if line := rowText(painted, 1); !strings.Contains(line, "kill pid 1000") || !strings.Contains(line, "y confirm") {
		t.Fatalf("confirm line missing the ask: %q", line)
	} else if strings.Contains(line, "filter") {
		t.Fatalf("the ask must replace the filter line: %q", line)
	}

	app.notice = "terminated pid 1000"
	painted = paint()
	found := false
	for y := 14; y < 24; y++ {
		found = found || strings.Contains(rowText(painted, y), "terminated pid 1000")
	}
	if !found {
		t.Fatalf("status line missing the notice")
	}
}

func TestProcsStaleAndForeignResultsIgnored(t *testing.T) {
	source := newScriptedSource(
		scriptStep{rows: seedProcs(8)},
		scriptStep{err: errors.New("transient")},
	)
	app := newProcsApp(source, nil)
	app.width, app.height = 80, 24

	load := app.Update(event.ResizeTo(80, 24))
	if load == nil {
		t.Fatal("first resize must issue the initial load")
	}
	if app.requestID != 1 || !app.inflight {
		t.Fatalf("load state: id=%d inflight=%v", app.requestID, app.inflight)
	}

	// A result with a foreign id is a superseded request's answer.
	app.Update(event.Event{Kind: event.Message, Data: procsResult{id: 99, rows: seedProcs(3)}})
	if app.phase != phaseLoading || !app.inflight || app.rows != nil {
		t.Fatalf("stale result was applied: phase=%v inflight=%v rows=%d", app.phase, app.inflight, len(app.rows))
	}
	// A foreign payload type is not ours to interpret.
	app.Update(event.Event{Kind: event.Message, Data: "not a procs result"})
	if app.phase != phaseLoading || !app.inflight {
		t.Fatalf("foreign payload was applied: phase=%v inflight=%v", app.phase, app.inflight)
	}

	// The real result lands through the same event the run loop would
	// deliver, and the model serves input afterwards.
	result := load(context.Background())
	if result.Kind != event.Message {
		t.Fatalf("load result kind = %v; want message", result.Kind)
	}
	app.Update(result)
	if app.phase != phaseData || app.inflight || len(app.order) != 8 {
		t.Fatalf("result state: phase=%v inflight=%v rows=%d", app.phase, app.inflight, len(app.order))
	}

	// A recoverable failure is data, not an ending: the next F5 still
	// issues a load command.
	retry := app.Update(event.KeyPress(event.KeyF5, 0))
	if retry == nil {
		t.Fatal("refresh after data must issue a load")
	}
	failed := retry(context.Background())
	app.Update(failed)
	if app.phase != phaseError || app.inflight {
		t.Fatalf("error state: phase=%v inflight=%v", app.phase, app.inflight)
	}
	after := app.Update(event.KeyPress(event.KeyF5, 0))
	if after == nil {
		t.Fatal("retry after a recoverable failure must issue a load")
	}
}

func TestProcsQuitCancelsInFlightLoad(t *testing.T) {
	source := newScriptedSource(scriptStep{delay: 8 * time.Second, rows: nil})
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseLoading })

	started := time.Now()
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("quit waited %v for the in-flight load; the run must not block on worker goroutines", elapsed)
	}

	// Run's context cancellation reaches the source mid-listing: the
	// worker observes the cancelled context instead of sleeping it out.
	deadline := time.After(3 * time.Second)
	for source.cancellations() == 0 {
		select {
		case <-deadline:
			t.Fatal("the source never observed the cancellation")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestParseMemUsage(t *testing.T) {
	if got := parseMemUsage("9,216 K"); got != 9216*1024 {
		t.Fatalf("parseMemUsage = %d; want %d", got, 9216*1024)
	}
	if got := parseMemUsage("12 K"); got != 12*1024 {
		t.Fatalf("parseMemUsage = %d; want %d", got, 12*1024)
	}
	if got := parseMemUsage(""); got != 0 {
		t.Fatalf("parseMemUsage of junk = %d; want 0", got)
	}
}

func TestProcessSourceListsRealProcesses(t *testing.T) {
	source, err := newProcessSource()
	if err != nil {
		t.Skipf("no source on this system: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := source.List(ctx, "")
	if err != nil {
		t.Fatalf("real source: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("real source listed no processes")
	}
	sane, named := 0, false
	for _, row := range rows {
		if row.pid > 0 && row.name != "" {
			sane++
		}
		if strings.Contains(row.name, "procs") {
			named = true // this test's own process is in the list
		}
	}
	if sane == 0 {
		t.Fatal("no row carries a pid and a name")
	}
	if !named {
		t.Fatalf("this test's own process is missing from %d rows", len(rows))
	}

	filtered, err := source.List(ctx, "procs")
	if err != nil {
		t.Fatalf("filtered source: %v", err)
	}
	if len(filtered) == 0 || len(filtered) >= len(rows) {
		t.Fatalf("filtered rows = %d of %d; the filter must cross the seam and narrow the list", len(filtered), len(rows))
	}
}

// TestProcsPaletteFlowsThroughTheme pins the stage-3 recipe: the app
// names its palette once, through Context.UseTheme, and the table's
// zero-value styles resolve through the roles. The first paint checks
// the roles this app actually shows — Focus for the selected row of the
// focused table, Text for body rows, Muted (underlined) for the header.
// The second paint swaps the palette with zero Focus — the zero-role
// opt-out — so the focused selected row falls back to the Accent role
// live: restyling is a theme swap, not an edit to any style block.
func TestProcsPaletteFlowsThroughTheme(t *testing.T) {
	app := newProcsApp(nil, nil)
	app.width, app.height = 80, 24
	app.phase = phaseData
	app.rows = seedProcs(4)
	app.rebuildOrder()

	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(80, 24)), Capabilities: capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	painted := paint()
	if got := painted.Cell(2, 3).Style; got.Foreground != background || got.Background != cyan {
		t.Fatalf("focused selected row must paint the theme Focus role: fg=%v bg=%v", got.Foreground, got.Background)
	}
	if got := painted.Cell(2, 4).Style; got.Foreground != ink {
		t.Fatalf("body row must paint the theme Text role: fg=%v", got.Foreground)
	}
	if got := painted.Cell(2, 2).Style; got.Foreground != muted || got.Attrs&render.AttrUnderline == 0 {
		t.Fatalf("header must paint the theme Muted role underlined: fg=%v attrs=%v", got.Foreground, got.Attrs)
	}

	app.ui.UseTheme(ui.Theme{
		Text:   render.Style{Foreground: muted},
		Muted:  render.Style{Foreground: ink},
		Accent: render.Style{Foreground: background, Background: violet},
	})
	painted = paint()
	if got := painted.Cell(2, 3).Style; got.Background != violet {
		t.Fatalf("zero Focus role must fall back to Accent for the selected row: bg=%v", got.Background)
	}
	if got := painted.Cell(2, 4).Style; got.Foreground != muted {
		t.Fatalf("restyled body row must paint the swapped Text role: fg=%v", got.Foreground)
	}
}

func TestProcessSourceKillMissingPidFails(t *testing.T) {
	source, err := newProcessSource()
	if err != nil {
		t.Skipf("no source on this system: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// pid 2147483632 (2^31-16) sits above every supported system's pid
	// ceiling: linux caps pid_max at 2^22, darwin at 99999, and windows
	// allocates far below this range, so the pid cannot name a live
	// process on any host — the adapter is asked to terminate a process
	// that cannot exist, which must fail without ever touching a real
	// one. Real targets stay a manual, human-reviewed gate.
	const missing = 2147483632
	err = source.Kill(ctx, missing)
	if err == nil {
		t.Fatal("killing a nonexistent pid must fail")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("kill of a missing pid must fail fast, not exhaust the context: %v", err)
	}
}

// TestProcsResizeGateKeepsTasksAndFocus pins the stage-3 size gate: the
// real data app, driven through the live runtime, must keep its task
// table and the focused selection through 240×80, 80×24, and 120×40 —
// resize repaints the layout, it never drops the work the screen was
// showing. The selected row is pushed down with real keystrokes first,
// so the shrink is not the trivial top-of-list case: the table must
// re-derive its viewport and keep row 12 both visible and focused.
func TestProcsResizeGateKeepsTasksAndFocus(t *testing.T) {
	source := newScriptedSource(scriptStep{delay: 20 * time.Millisecond, rows: seedProcs(40)})
	backend := &interactiveBackend{events: make(chan event.Event, 64), frames: make(chan *render.Buffer, 64)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseData && state.rows == 40 })
	// Force one repaint so the writer's present callbacks have committed
	// the table's focus: keystrokes below route to the focused table.
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrame(t, backend.frames)

	// The gate sizes, one at a time: assert on the first frame painted at
	// the new size. Reports leave View before Present, so a stale
	// pre-resize frame can still be in flight; the bounds filter skips
	// those instead of trusting the drain.
	//
	// A frame on the wire proves only the PREVIOUS frame's commit: the
	// writer pushes the buffer to Present before running its presented
	// callbacks, so the resized frame's region re-registration can still
	// be pending when its paint is read. resizeLocked clears the
	// committed regions on the way in, and a keystroke routed through an
	// empty snapshot is dropped by design — returning the moment the
	// paint arrives would race the next input against the commit. One
	// forced repaint after the match is the barrier: its arrival proves
	// the resized frame's presented callbacks (the commit) already ran,
	// because the writer presents surfaces sequentially.
	resize := func(w, h int) *render.Buffer {
		drainFrames(backend.frames)
		backend.events <- event.ResizeTo(w, h)
		for {
			painted := waitFrame(t, backend.frames)
			if bw, bh := painted.Bounds().W, painted.Bounds().H; bw == w && bh == h {
				// F6 is bound nowhere; it only forces the repaint that
				// certifies the commit above.
				backend.events <- event.KeyPress(event.KeyF6, 0)
				waitFrame(t, backend.frames)
				return painted
			}
		}
	}

	painted := resize(240, 80)
	if w, h := painted.Bounds().W, painted.Bounds().H; w != 240 || h != 80 {
		t.Fatalf("resized frame is %d×%d; want 240×80", w, h)
	}
	if !strings.Contains(rowText(painted, 2), "PID") {
		t.Fatalf("240×80 header missing: %q", rowText(painted, 2))
	}
	if !strings.Contains(rowText(painted, 3), "relay-00") || !strings.Contains(rowText(painted, 42), "worker-39") {
		t.Fatalf("240×80 must show the deep task rows: %q / %q", rowText(painted, 3), rowText(painted, 42))
	}
	if got := painted.Cell(2, 3).Style; got.Foreground != background || got.Background != cyan {
		t.Fatalf("240×80 focused row 0 must paint the Focus role: fg=%v bg=%v", got.Foreground, got.Background)
	}

	// Push the selection to row 12 with real routed keystrokes, on the
	// large screen where all rows sit below the viewport-free top.
	for i := 0; i < 12; i++ {
		backend.events <- event.KeyPress(event.KeyDown, 0)
	}
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.selected == 12 })

	for _, probe := range []struct {
		w, h int
		name string
	}{
		{80, 24, "80×24"},
		{120, 40, "120×40"},
	} {
		painted = resize(probe.w, probe.h)
		if w, h := painted.Bounds().W, painted.Bounds().H; w != probe.w || h != probe.h {
			t.Fatalf("%s frame is %d×%d", probe.name, w, h)
		}
		if strings.Contains(rowText(painted, 0), "enlarge terminal") {
			t.Fatalf("%s must fit the real layout, not the tiny guard", probe.name)
		}
		if !strings.Contains(rowText(painted, 2), "PID") {
			t.Fatalf("%s header missing: %q", probe.name, rowText(painted, 2))
		}
		if !strings.Contains(rowText(painted, 3), "relay-00") {
			t.Fatalf("%s task rows missing: %q", probe.name, rowText(painted, 3))
		}
		// Row 12 stays both selected and focused: a solid focus block on
		// the row the keystrokes chose, at body row 12 → screen row 15.
		if got := painted.Cell(2, 15).Style; got.Foreground != background || got.Background != cyan {
			t.Fatalf("%s focused row 12 must keep the Focus role: fg=%v bg=%v", probe.name, got.Foreground, got.Background)
		}
		if !strings.Contains(rowText(painted, 15), "relay-12") {
			t.Fatalf("%s focused row text changed: %q", probe.name, rowText(painted, 15))
		}
		foundStatus := false
		for y := probe.h - 8; y < probe.h; y++ {
			if strings.Contains(rowText(painted, y), "data  40 rows  row 13 ") {
				foundStatus = true
			}
		}
		if !foundStatus {
			t.Fatalf("%s status must still report row 13 of 40 tasks", probe.name)
		}
	}

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

// ctrlP synthesizes the palette chord the way a real terminal delivers
// it: Ctrl riding a rune press.
func ctrlP() event.Event {
	return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'p', Mod: event.ModCtrl}}
}

// hasRow reports whether any painted row contains text, without
// failing the test: waitFrameWhere uses it to skip stale frames.
func hasRow(painted *render.Buffer, text string) bool {
	for y := 0; y < painted.Bounds().H; y++ {
		if strings.Contains(rowText(painted, y), text) {
			return true
		}
	}
	return false
}

// The palette commit gates, shared by the two end-to-end palette
// tests: Enter/Down route modal-scoped only against a committed open
// palette, runes feed the committed palette input, and the closing
// frame's commit disarms the modal scope so a later Escape reaches the
// app-scope quit shortcut instead of dismissing a dead modal.
func paletteOpenFrame(painted *render.Buffer) bool {
	return hasRow(painted, "command palette")
}

func paletteClosedFrame(painted *render.Buffer) bool {
	return !hasRow(painted, "command palette")
}

// killAskPainted matches the inline confirm ask, which only paints when
// the palette modal is gone (the modal covers its row): the gates for
// the 'n'/'y' runes need the closed palette AND the armed ask
// committed, or the rune routes into the dead palette input.
func killAskPainted(painted *render.Buffer) bool {
	return !paletteOpenFrame(painted) && hasRow(painted, "kill pid 1000")
}

// findRow returns the first painted row containing text, failing the
// test when nothing matches.
func findRow(t *testing.T, painted *render.Buffer, text string) int {
	t.Helper()
	for y := 0; y < painted.Bounds().H; y++ {
		if strings.Contains(rowText(painted, y), text) {
			return y
		}
	}
	t.Fatalf("no painted row contains %q", text)
	return -1
}

// forbidRow asserts no painted row contains text.
func forbidRow(t *testing.T, painted *render.Buffer, text string) {
	t.Helper()
	for y := 0; y < painted.Bounds().H; y++ {
		if strings.Contains(rowText(painted, y), text) {
			t.Fatalf("painted row %d must not contain %q: %q", y, text, rowText(painted, y))
		}
	}
}

// TestCommandPaletteLifecycle drives the palette through its interactive
// surface: open with Ctrl+P, live filtering, the no-result state, inert
// Enter on a no-match and on a disabled command with its reason, close
// with Escape and an outside click, filter reset on reopen, and focus
// handing back to the table.
func TestCommandPaletteLifecycle(t *testing.T) {
	full := seedProcs(40)
	source := newScriptedSource(scriptStep{delay: 40 * time.Millisecond, rows: full})
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseData && state.rows == 40 })
	// The F6 dance: a second frame on the wire proves the first frame's
	// present callbacks ran, so the Ctrl+P shortcut is committed.
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrame(t, backend.frames)

	// Ctrl+P opens the palette with all commands listed; only "Clear
	// filter" is disabled, because the table filter is empty.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.palette && state.paletteRows == 4 && state.paletteSelected == 0
	})
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted := waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return hasRow(painted, "command palette")
	})
	titleRow := findRow(t, painted, "command palette")
	if findRow(t, painted, "type to filter commands") != titleRow+1 {
		t.Fatalf("filter placeholder must sit on the row under the title: %q", rowText(painted, titleRow+1))
	}
	for _, want := range []string{"Kill selected process", "Refresh process list", "Clear filter", "Quit"} {
		findRow(t, painted, want)
	}
	if killRow := findRow(t, painted, "Kill selected process"); !strings.Contains(rowText(painted, killRow), "enter") {
		t.Fatalf("kill row must carry its shortcut hint: %q", rowText(painted, killRow))
	}
	if refreshRow := findRow(t, painted, "Refresh process list"); !strings.Contains(rowText(painted, refreshRow), "F5") {
		t.Fatalf("refresh row must carry its shortcut hint: %q", rowText(painted, refreshRow))
	}
	findRow(t, painted, " enter run  up/down move  esc close ")
	// The table stays painted behind the modal.
	findRow(t, painted, "PID")

	// Typing filters live; the runes feed the palette's input, so the
	// table's filter and its loads stay untouched.
	for _, r := range "refresh" {
		backend.events <- event.RuneKey(r, 0)
	}
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.palette && state.paletteRows == 1 && state.rows == 40 && state.filter == ""
	})
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted = waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		// The stale pre-filter frames are the four-command palette; the
		// target view lists only the refresh command.
		return hasRow(painted, "Refresh process list") && !hasRow(painted, "Kill selected process")
	})
	findRow(t, painted, "Refresh process list")
	forbidRow(t, painted, "Kill selected process")
	forbidRow(t, painted, "type to filter commands")

	// A filter that matches nothing paints the no-result line; Enter
	// stays inert instead of inventing an outcome.
	backend.events <- event.RuneKey('z', 0)
	backend.events <- event.RuneKey('z', 0)
	backend.events <- event.RuneKey('z', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 0 })
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return hasRow(painted, "no matching commands")
	})
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted = waitFrame(t, backend.frames)
	findRow(t, painted, "command palette") // still open: Enter on nothing is a no-op
	if got := source.filters(); len(got) != 1 || got[0] != "" {
		t.Fatalf("filter log = %q; palette typing and dead Enter must not issue loads", got)
	}

	// Backspace edits the filter like any text field.
	for i := 0; i < 3; i++ {
		backend.events <- event.KeyPress(event.KeyBackspace, 0)
	}
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 1 })

	// Escape closes; Ctrl+P reopens with the filter reset.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return !state.palette })
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 4 })
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return hasRow(painted, "type to filter commands")
	})

	// Down twice lands on "Clear filter", disabled because the table
	// filter is empty: the row paints disabled and the footer replaces
	// the key hints with the reason.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.palette && state.paletteRows == 4 && state.paletteSelected == 2
	})
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted = waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return hasRow(painted, "the filter is empty")
	})
	clearRow := findRow(t, painted, "Clear filter")
	// The modal paints over the table and the detail pane, so the row
	// carries background runes beside the list; the marker proves
	// selection only next to the command it marks.
	if clear := rowText(painted, clearRow); !strings.Contains(clear, "> Clear filter") {
		t.Fatalf("selected disabled row must keep its marker: %q", clear)
	}
	disabledPainted := false
	for x := 0; x < painted.Bounds().W; x++ {
		if cell := painted.Cell(x, clearRow); cell.Grapheme == "C" && cell.Style.Foreground == red {
			disabledPainted = true
		}
	}
	if !disabledPainted {
		t.Fatalf("disabled row must paint in the disabled color: %q", rowText(painted, clearRow))
	}

	// Enter on a disabled command is inert: the palette stays open and
	// no load runs.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	painted = waitFrame(t, backend.frames)
	findRow(t, painted, "command palette")
	if got := source.filters(); len(got) != 1 {
		t.Fatalf("filter log = %q; a disabled command must not run", got)
	}

	// Escape closes the palette and hands focus back to the table: the
	// next Down moves the table's selection.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return !state.palette })
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	// The stale frames here are still-open palettes; the target view
	// dropped the modal entirely, so the negative predicate skips them.
	waitFrameWhere(t, backend.frames, func(painted *render.Buffer) bool {
		return !hasRow(painted, "command palette")
	})
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.selected == 1 && state.phase == phaseData
	})

	// Ctrl+P toggles both ways; a click outside the open palette
	// dismisses it.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette })
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return !state.palette })
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette })
	// The click routes against the modal only once the OPEN palette
	// frame is committed: frames painted before the toggle can still be
	// queued in the writer, so gate on the open palette's content, not
	// on whichever frame arrives next.
	commitFrameWhere(t, backend, paletteOpenFrame)
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		Action: event.MousePress, Button: event.MouseLeft, X: 1, Y: 15,
	}}
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return !state.palette })

	// The mouse press closed the palette, but that closing frame must
	// commit before Escape or the key dismisses the already-closed
	// modal instead of quitting. A single post-drain read is not enough
	// here: frames painted before the click can still be queued in the
	// writer, so gate on the CLOSED palette's content — the frame that
	// proves the modal scope is disarmed.
	commitFrameWhere(t, backend, paletteClosedFrame)
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit")
	}
	if got := source.filters(); len(got) != 1 || got[0] != "" {
		t.Fatalf("filter log = %q; the palette must never touch the table's loads", got)
	}
}

// TestCommandPaletteRunsCommands proves the palette's commands change
// real state: kill opens the existing confirm ask, refresh reloads
// through the app's load seam, clear-filter edits the table filter, and
// quit ends the run.
func TestCommandPaletteRunsCommands(t *testing.T) {
	full := seedProcs(40)
	refreshed := seedProcs(38)
	afterKill := seedProcs(39)
	cleared := seedProcs(37)
	source := newScriptedSource(
		scriptStep{delay: 40 * time.Millisecond, rows: full},      // initial load
		scriptStep{delay: 30 * time.Millisecond, rows: refreshed}, // palette refresh
		scriptStep{delay: 30 * time.Millisecond, rows: afterKill}, // post-kill auto-refresh
		scriptStep{delay: 30 * time.Millisecond, rows: full},      // typed 'r' filter
		scriptStep{delay: 30 * time.Millisecond, rows: cleared},   // palette clear-filter
	)
	source.killDelay = 30 * time.Millisecond
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newProcsApp(source, make(chan procsState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.phase == phaseData && state.rows == 40 })
	// The F6 dance: a second frame on the wire proves the first frame's
	// present callbacks ran, so the Ctrl+P shortcut is committed.
	commitFrames(t, backend)

	// Run the kill command from the palette: the palette closes and the
	// existing inline confirm ask names the selected row. Cancelling
	// with 'n' leaves the table alone.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette })
	// The palette's shortcuts are modal-scoped: Enter routes against the
	// palette only once its frame is committed — an open-palette frame,
	// not whichever frame the writer delivers next.
	commitFrameWhere(t, backend, paletteOpenFrame)
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.confirm == 1000
	})
	// Running the command closed the palette as a side effect; that
	// closing frame must commit before 'n' or the rune routes into the
	// dead palette input instead of the confirm ask. Gate on the ask
	// being painted — which also proves the modal is gone, since the
	// palette covers the ask's row.
	commitFrameWhere(t, backend, killAskPainted)
	backend.events <- event.RuneKey('n', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.confirm == 0 && state.rows == 40
	})

	// Refresh from the palette: the command reuses the app's load seam,
	// the palette owns no separate pipeline.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette })
	// Down and Enter are modal-scoped here too: gate on the open
	// palette's frame, not the first post-drain frame.
	commitFrameWhere(t, backend, paletteOpenFrame)
	backend.events <- event.KeyPress(event.KeyDown, 0)
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	// The loading phase between the palette closing and the refresh
	// landing is transient: under coverage instrumentation and parallel
	// package load the runner can be starved long enough for the result
	// to coalesce with the Enter event, and no loading frame ever
	// paints. The durable contract is the closed palette plus the
	// reloaded data.
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.phase == phaseData && state.rows == 38
	})

	// Kill again, this time confirming: the pid dies, the notice lands,
	// and the post-kill auto-refresh repaints the shorter table.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette })
	// Enter must route against the committed open palette, so the kill
	// command runs rather than an event into the stale table snapshot.
	commitFrameWhere(t, backend, paletteOpenFrame)
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.confirm == 1000
	})
	// Same closing race as the 'n' step: the 'y' must answer the ask,
	// not the dead palette input, so the armed-ask frame must commit.
	commitFrameWhere(t, backend, killAskPainted)
	backend.events <- event.RuneKey('y', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return strings.Contains(state.notice, "terminated pid 1000") && state.rows == 39
	})

	// A typed table filter makes "Clear filter" runnable; running it
	// clears the filter and reloads through the same seam. Of the 40
	// seeded rows, "r" matches the relay and worker names: 20.
	backend.events <- event.RuneKey('r', 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.filter == "r" && state.phase == phaseData && state.rows == 20
	})
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 4 })
	// The Downs move the modal's selection only against a committed
	// open-palette frame.
	painted := commitFrameWhere(t, backend, paletteOpenFrame)
	forbidRow(t, painted, "the filter is empty")
	backend.events <- event.KeyPress(event.KeyDown, 0)
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return state.palette && state.paletteSelected == 2
	})
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	// Same coalescing hazard as the refresh step above: wait for the
	// outcome (filter cleared, palette closed, data reloaded), not the
	// transient loading phase.
	waitStateWhere(t, app.observe, done, func(state procsState) bool {
		return !state.palette && state.filter == "" && state.phase == phaseData && state.rows == 37
	})

	// Quit runs from the palette: filter down to one command, Enter ends
	// the run cleanly.
	backend.events <- ctrlP()
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 4 })
	// The runes feed the palette's input; an open-palette frame must be
	// committed first or they fall through to the table's filter.
	commitFrameWhere(t, backend, paletteOpenFrame)
	for _, r := range "quit" {
		backend.events <- event.RuneKey(r, 0)
	}
	waitStateWhere(t, app.observe, done, func(state procsState) bool { return state.palette && state.paletteRows == 1 })
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("palette quit did not end the run")
	}

	// The commands ran through the app's own seams: five loads with the
	// exact filters the table state dictated, one kill.
	want := []string{"", "", "", "r", ""}
	if filters := source.filters(); !slices.Equal(filters, want) {
		t.Fatalf("filter log = %q; want %q", filters, want)
	}
	if kills := source.killList(); len(kills) != 1 || kills[0] != 1000 {
		t.Fatalf("kills = %v; want [1000]", kills)
	}
}
