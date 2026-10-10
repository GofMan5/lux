package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
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
// logged, and cancellation during a delay is counted.
type scriptedSource struct {
	mu        sync.Mutex
	steps     []scriptStep
	calls     int
	filterLog []string
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
	for {
		select {
		case state := <-states:
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			t.Fatalf("matching state timeout; want %v", "state")
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

// drainFrames discards the backlog so the next waitFrame observes the
// frame the test just provoked, not an old phase.
func drainFrames(frames <-chan *render.Buffer) {
	for {
		select {
		case <-frames:
		default:
			return
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
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	if painted := waitFrame(t, backend.frames); !strings.Contains(rowText(painted, 3), "error: list unavailable") {
		t.Fatalf("error frame missing the failure: %q", rowText(painted, 3))
	}

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
