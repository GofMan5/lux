// Command async is the stage-4 async feedback recipe: a feed list that
// streams in chunks (skeleton, then partial rows, then the committed
// answer), reloads single-flight with a stale result kept on screen
// while the refresh runs, cancels in-flight work, and runs a long
// indexing job with a progress gauge — every phase visible, every
// action with an outcome and a failure path. All the async mechanics
// live here in the example layer on the plain Command/Message seam:
// the ui package owns no scheduler of its own.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The palette names alias the library reference palette so the chrome
// calls stay short and readable; red is reserved for verdicts (load
// failures), never for data.
var (
	palette    = ui.ReferencePalette()
	ink        = palette.Ink
	muted      = palette.Muted
	violet     = palette.Violet
	cyan       = palette.Cyan
	background = palette.Background
	red        = render.RGBColor(248, 113, 113)
)

// feedRow is one feed in the list. status is data (fresh, degraded,
// stale), not a verdict — verdicts are the load-level words on the
// status line.
type feedRow struct {
	name      string
	status    string
	latencyMs int
}

// feedSource is the seam that keeps the example deterministic: the
// demo implementation sleeps, the test implementation answers from
// channels. Feeds streams one chunk of the request; done reports the
// final chunk. Index walks a long-running job one step at a time so
// the progress gauge has real increments to show.
type feedSource interface {
	Feeds(ctx context.Context, req int) (rows []feedRow, done bool, err error)
	Index(ctx context.Context, job int) (done, total int, err error)
}

// chunkResult is a completed Feeds call. The id pairs it with the load
// that issued it: a result from a superseded or cancelled load is
// dropped on arrival, so a late chunk can never clobber a newer answer.
type chunkResult struct {
	id   int
	rows []feedRow
	done bool
	err  error
}

// jobResult is one completed Index step, paired with its job the same
// way.
type jobResult struct {
	id    int
	done  int
	total int
	err   error
}

// feedsState is the committed beacon snapshot: everything a test needs
// to gate input routing on the exact tree it just observed.
type feedsState struct {
	loading    bool
	stale      bool
	loaded     bool
	rows       int
	pending    int
	errText    string
	notice     string
	jobRunning bool
	jobDone    int
	jobTotal   int
	selected   int
	focused    string
}

// feedsApp holds the answer (rows), the next answer under construction
// (next), and the two workers' bookkeeping. rootCtx is cancelled only
// on quit; each load and each job gets its own child so 'x' can cancel
// the work without taking the app down.
type feedsApp struct {
	width, height int
	ui            ui.Context
	source        feedSource
	observe       chan feedsState
	committed     chan feedsState

	rootCtx    context.Context
	rootCancel context.CancelFunc

	// load bookkeeping: one flight at a time, deferred refreshes replay.
	loading        bool
	stale          bool
	loaded         bool
	refreshPending bool
	requestID      int
	next           []feedRow
	rows           []feedRow
	loadCtx        context.Context
	loadCancel     context.CancelFunc

	// index job bookkeeping.
	jobID      int
	jobCtx     context.Context
	jobCancel  context.CancelFunc
	jobRunning bool
	jobDone    int
	jobTotal   int

	order   []int
	table   ui.TableState
	spinner ui.SpinnerState
	notice  string
	errText string
	// seq, noticeSeq, and errSeq order the two writers that compete
	// for the notice row: whichever event happened last owns the row.
	seq       int
	noticeSeq int
	errSeq    int
	started   bool
}

// tableColumns: FEED is fixed-width, STATUS flexes, LATENCY stays
// right-aligned-ish under its header.
var tableColumns = []ui.TableColumn{
	{Title: "FEED", Width: 22},
	{Title: "STATUS", Weight: 2},
	{Title: "LATENCY", Width: 8},
}

func newFeedsApp(source feedSource, observe, committed chan feedsState) *feedsApp {
	ctx, cancel := context.WithCancel(context.Background())
	return &feedsApp{
		source:     source,
		observe:    observe,
		committed:  committed,
		rootCtx:    ctx,
		rootCancel: cancel,
	}
}

// markNotice and markError stamp the notice row's two competing
// writers so the row always shows the newest event: an index rebuilt
// after a failed refresh replaces the error line, and a newer failure
// replaces an older notice. Clearing a field (a new flight resets
// both) is not an event and does not stamp.
func (app *feedsApp) markNotice(text string) {
	app.seq++
	app.noticeSeq, app.notice = app.seq, text
}

func (app *feedsApp) markError(text string) {
	app.seq++
	app.errSeq, app.errText = app.seq, text
}

// displayRows is what the table shows: during a first load the partial
// chunk list grows in place; during a refresh the previous answer
// stays on screen (stale) until the new one completes atomically.
func (app *feedsApp) displayRows() []feedRow {
	if app.loading {
		if app.stale {
			return app.rows
		}
		return app.next
	}
	return app.rows
}

// syncOrder rebuilds the display order after any answer change and
// clamps the selection to the new row count.
func (app *feedsApp) syncOrder() {
	rows := app.displayRows()
	app.order = make([]int, len(rows))
	for i := range app.order {
		app.order[i] = i
	}
	app.table.Selected = min(max(app.table.Selected, 0), max(len(app.order)-1, 0))
}

// sortRows re-slices the order by the clicked column. A new answer
// resets the order to identity (in syncOrder) — a fresh load is
// allowed to come back in source order.
func (app *feedsApp) sortRows() {
	rows := app.displayRows()
	column, desc := app.table.SortColumn, app.table.SortDesc
	sort.SliceStable(app.order, func(i, j int) bool {
		a, b := rows[app.order[i]], rows[app.order[j]]
		result := 0
		switch column {
		case 1:
			result = cmp.Compare(a.status, b.status)
		case 2:
			result = cmp.Compare(a.latencyMs, b.latencyMs)
		default:
			result = cmp.Compare(a.name, b.name)
		}
		if desc {
			return result > 0
		}
		return result < 0
	})
}

// issueLoad is the single-flight gate: while a flight is open, a new
// request is recorded (refreshPending) and replayed when the open one
// finishes, rather than racing two loads against each other.
func (app *feedsApp) issueLoad() lux.Command {
	if app.loading {
		app.refreshPending = true
		return nil
	}
	app.next = nil
	return app.nextChunk(true)
}

// nextChunk issues one Feeds call. The first call opens the load
// (fresh context, fresh request id); continuations keep both, so a
// done=false answer chains its successor with the same identity.
func (app *feedsApp) nextChunk(first bool) lux.Command {
	if first {
		app.requestID++
		app.stale = len(app.rows) > 0
		app.spinner = ui.SpinnerState{Started: time.Now()}
		app.errText = ""
		app.notice = ""
		app.loadCtx, app.loadCancel = context.WithCancel(app.rootCtx)
		app.loading = true
	}
	id := app.requestID
	ctx := app.loadCtx
	source := app.source
	return func(_ context.Context) event.Event {
		rows, done, err := source.Feeds(ctx, id)
		return event.Event{Kind: event.Message, Data: chunkResult{id: id, rows: rows, done: done, err: err}}
	}
}

// applyChunk folds one Feeds answer in. A result whose id is no longer
// current belongs to a superseded or cancelled load and is dropped.
func (app *feedsApp) applyChunk(result chunkResult) lux.Command {
	if result.id != app.requestID {
		return nil
	}
	defer app.syncOrder()
	if result.err != nil {
		// The cancellation verdict must be read before finishLoad
		// clears the context it is judged against.
		cancelled := ctxErr(app.loadCtx, result.err)
		// A load that died of an error or cancellation also retires
		// any deferred refresh: the user who pressed r twice and then
		// cancelled wants to stay on the old answer, not auto-retry.
		app.refreshPending = false
		app.finishLoad()
		if cancelled {
			app.markNotice("load cancelled - r reloads")
		} else {
			app.markError(result.err.Error())
		}
		return nil
	}
	app.next = append(app.next, result.rows...)
	if !result.done {
		return app.nextChunk(false)
	}
	app.rows = app.next
	app.finishLoad()
	app.loaded = true
	if app.refreshPending {
		app.refreshPending = false
		// The deferred reload is replayed here, inside the same Update
		// turn, so the wire never paints a "done" state in between: the
		// user who pressed r twice sees one continuous refresh.
		return app.issueLoad()
	}
	return nil
}

// finishLoad closes the flight and clears its context. The salvage
// rule keeps a partial answer when nothing better exists: fragments
// the user can read outrank an empty list, and the previous full
// answer outranks fragments.
func (app *feedsApp) finishLoad() {
	if len(app.rows) == 0 && len(app.next) > 0 {
		app.rows = app.next
	}
	app.next = nil
	app.loading = false
	app.stale = false
	// refreshPending is deliberately NOT cleared here: whether a
	// deferred reload survives a closing flight is the caller's
	// decision — the success path replays it, the failure path
	// retires it.
	if app.loadCancel != nil {
		app.loadCancel()
	}
	app.loadCtx, app.loadCancel = nil, nil
}

// startIndex opens the index job: another single flight, but an
// independent one — it can run while a load streams.
func (app *feedsApp) startIndex() lux.Command {
	if app.jobRunning {
		return nil
	}
	app.jobID++
	app.jobDone, app.jobTotal = 0, 0
	app.jobCtx, app.jobCancel = context.WithCancel(app.rootCtx)
	app.jobRunning = true
	app.spinner = ui.SpinnerState{Started: time.Now()}
	id := app.jobID
	ctx := app.jobCtx
	source := app.source
	return func(_ context.Context) event.Event {
		done, total, err := source.Index(ctx, id)
		return event.Event{Kind: event.Message, Data: jobResult{id: id, done: done, total: total, err: err}}
	}
}

// applyJob folds one Index step in and chains the next until the job
// reports completion.
func (app *feedsApp) applyJob(result jobResult) lux.Command {
	if result.id != app.jobID || !app.jobRunning {
		return nil
	}
	if result.err != nil {
		app.jobRunning = false
		if ctxErr(app.jobCtx, result.err) {
			app.markNotice("index cancelled - p restarts")
		} else {
			app.markNotice("index failed: " + result.err.Error())
		}
		if app.jobCancel != nil {
			app.jobCancel()
		}
		app.jobCtx, app.jobCancel = nil, nil
		return nil
	}
	app.jobDone, app.jobTotal = result.done, result.total
	if app.jobDone >= app.jobTotal && app.jobTotal > 0 {
		app.jobRunning = false
		app.markNotice(fmt.Sprintf("index rebuilt (%d units)", app.jobDone))
		if app.jobCancel != nil {
			app.jobCancel()
		}
		app.jobCtx, app.jobCancel = nil, nil
		return nil
	}
	id, ctx, source := app.jobID, app.jobCtx, app.source
	return func(_ context.Context) event.Event {
		done, total, err := source.Index(ctx, id)
		return event.Event{Kind: event.Message, Data: jobResult{id: id, done: done, total: total, err: err}}
	}
}

// ctxErr reports whether err is the context's own cancellation: the
// framework delivers work errors and user cancels through the same
// channel, and only the latter gets the "cancelled" wording.
func ctxErr(ctx context.Context, err error) bool {
	return ctx != nil && ctx.Err() != nil && err != nil && (err == context.Canceled || err == context.DeadlineExceeded)
}

// cancelInflight is 'x': it cancels whatever work is open (load, job,
// or both) without quitting. The workers observe the cancellation and
// report back through their results.
func (app *feedsApp) cancelInflight() {
	if app.loadCancel != nil {
		app.loadCancel()
	}
	if app.jobCancel != nil {
		app.jobCancel()
	}
}

func (app *feedsApp) cancelAll() {
	app.cancelInflight()
	app.rootCancel()
}

// applyTable serves the table's interactions: selection (and the
// keyboard/wheel scrolling that carries it), header presses that sort,
// and Enter/Space activation that reads the selected feed.
func (app *feedsApp) applyTable(action ui.Action) {
	_, activated, sortRequested := app.table.Apply(action, tableColumns, len(app.order))
	if sortRequested {
		app.sortRows()
		app.table.SortRequested = false
	}
	if activated && len(app.order) > 0 {
		app.markNotice("reading " + app.displayRows()[app.order[app.table.Selected]].name)
	}
}

func (app *feedsApp) rowAt(index int) []string {
	rows := app.displayRows()
	if index < 0 || index >= len(rows) || index >= len(app.order) {
		return nil
	}
	row := rows[app.order[index]]
	return []string{row.name, row.status, fmt.Sprintf("%dms", row.latencyMs)}
}

// statusWord is the one-word verdict for the status line; the details
// live on the notice row and in the table itself.
func (app *feedsApp) statusWord() (string, render.Style) {
	inkStyle := render.Style{Foreground: ink, Background: background}
	switch {
	case app.errText != "":
		return " error ", render.Style{Foreground: red, Background: background, Attrs: render.AttrBold}
	case app.loading && app.stale:
		return " stale - previous result shown ", render.Style{Foreground: violet, Background: background}
	case app.loading && len(app.next) > 0:
		return fmt.Sprintf(" partial - %d rows in ", len(app.next)), render.Style{Foreground: violet, Background: background}
	case app.loading:
		return " loading ", inkStyle
	case !app.loaded:
		return " starting ", render.Style{Foreground: muted, Background: background}
	case len(app.rows) == 0:
		return " empty ", render.Style{Foreground: muted, Background: background}
	default:
		return fmt.Sprintf(" fresh - %d feeds ", len(app.rows)), inkStyle
	}
}

func (app *feedsApp) report() feedsState {
	focused := ""
	switch {
	case app.ui.Focused("table"):
		focused = "table"
	case app.ui.Focused("reload"):
		focused = "reload"
	}
	return feedsState{
		loading:    app.loading,
		stale:      app.stale,
		loaded:     app.loaded,
		rows:       len(app.order),
		pending:    len(app.next),
		errText:    app.errText,
		notice:     app.notice,
		jobRunning: app.jobRunning,
		jobDone:    app.jobDone,
		jobTotal:   app.jobTotal,
		selected:   app.table.Selected,
		focused:    focused,
	}
}

func (app *feedsApp) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "quit":
			app.cancelAll()
			return lux.Quit()
		case "table":
			app.applyTable(action)
		case "reload":
			if action.Kind == ui.Activate {
				return app.issueLoad()
			}
		case "cancel":
			app.cancelInflight()
		case "index":
			if action.Kind == ui.Activate {
				return app.startIndex()
			}
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
		// The first frame is the signal that input routing exists: the
		// first load starts only then, so the skeleton phase is
		// reachable in the run, not just in theory.
		if !app.started {
			app.started = true
			return app.issueLoad()
		}
	case event.Message:
		if job, ok := next.Data.(jobResult); ok {
			return app.applyJob(job)
		}
		if result, ok := next.Data.(chunkResult); ok {
			return app.applyChunk(result)
		}
	}
	return nil
}

func (app *feedsApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	// The committed beacon: End queues the frame's commit first, then
	// this OnPresented callback right after it, and the writer runs
	// present callbacks in queue order — so the beacon fires strictly
	// after the tree this View built is the tree inputs route against.
	// The send is non-blocking so it can never stall the writer. One
	// composite defer is load-bearing: a second plain defer would run
	// before this one (LIFO) and queue the beacon BEFORE the commit.
	var state feedsState
	defer func() {
		app.ui.End(frame)
		beacon := state
		frame.OnPresented(func() {
			select {
			case app.committed <- beacon:
			default:
			}
		})
	}()

	// Priority so Escape quits even while the table holds focus: an
	// app-scope priority shortcut outranks the focused table's claim on
	// the key. r/x/p need no priority — the table claims no runes.
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'r'}, Target: "reload", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x'}, Target: "cancel", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'p'}, Target: "index", Kind: ui.Activate, Scope: ui.ScopeApp})

	if area.W < 56 || area.H < 16 {
		canvas.Text(0, 0, "LUX feeds - enlarge terminal", render.Style{Foreground: violet})
		state = app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX FEEDS ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	// The status line: a spinner while any work is open, then the
	// one-word verdict. The spinner advances only when a frame is
	// painted — LUX repaints on every input and async message, so the
	// phase animates with the work itself.
	if app.loading || app.jobRunning {
		app.ui.Spinner(canvas, render.Rect{X: 2, Y: 1, W: 1, H: 1}, &app.spinner, time.Now(), ui.SpinnerStyle{})
	} else {
		canvas.Text(2, 1, " ", render.Style{Foreground: ink, Background: background})
	}
	word, wordStyle := app.statusWord()
	canvas.Text(4, 1, word, wordStyle)

	// The body: the table is registered in EVERY phase with the header
	// always painted, so the layout never jumps between skeleton,
	// partial, and ready — the rows simply grow under a fixed header.
	body := render.Rect{X: 2, Y: 3, W: area.W - 4, H: area.H - 11}
	style := ui.TableStyle{}
	style.Normal = render.Style{Foreground: ink, Background: background}
	app.ui.Table("table", canvas, body, &app.table, tableColumns, len(app.order), app.rowAt, style)
	if len(app.order) == 0 {
		y := body.Y + 1
		switch {
		case app.loading:
			// The skeleton occupies exactly the rows the data will:
			// same rect, same pace, zero layout shift on arrival.
			for i := 0; i < 3; i++ {
				canvas.Text(body.X+2, y+i, strings.Repeat("░", min(body.W-4, 24)), render.Style{Foreground: muted, Background: background})
			}
			canvas.Text(body.X+2, y, " loading ", render.Style{Foreground: ink, Background: background})
		case app.errText != "":
			canvas.Text(body.X+2, y, " load failed: "+app.errText+" ", render.Style{Foreground: red, Background: background})
			canvas.Text(body.X+2, y+1, " r or Reload retries ", render.Style{Foreground: muted, Background: background})
		default:
			hint := " no feeds yet - r or Reload starts the first load "
			if app.loaded {
				hint = " no feeds - r or Reload reloads "
			}
			canvas.Text(body.X+2, y, hint, render.Style{Foreground: muted, Background: background})
		}
	}

	// The progress row keeps a fixed slot: blank when idle, label,
	// percentage, and gauge while the job runs. A gauge that appeared
	// and disappeared would move everything below it.
	progressY := area.H - 7
	if app.jobRunning {
		canvas.Text(2, progressY, " indexing ", render.Style{Foreground: muted, Background: background})
		fraction := 0
		if app.jobTotal > 0 {
			fraction = min(100*app.jobDone/app.jobTotal, 100)
		}
		canvas.Text(13, progressY, fmt.Sprintf(" %3d%% ", fraction), render.Style{Foreground: ink, Background: background})
		gauge := ui.GaugeState{Value: float64(app.jobDone), Max: float64(app.jobTotal)}
		app.ui.Gauge(canvas, render.Rect{X: 20, Y: progressY, W: 24, H: 1}, &gauge, ui.GaugeStyle{})
	}

	// Reload is always registered at a fixed slot and only disabled
	// while a flight is open — the mouse retry path stays in place in
	// every other phase, including the error one.
	reload := ui.ButtonStyle{Disabled: app.loading}
	reload.Normal = render.Style{Foreground: ink, Background: background}
	app.ui.Button("reload", canvas, render.Rect{X: 4, Y: area.H - 5, W: 12, H: 3}, " Reload ", reload)

	// The notice row: whichever event happened last owns it — a load
	// failure while the error is the newest event, otherwise the last
	// notice (cancelled, indexed, reading feed). A failed refresh that
	// is later followed by a finished index job gives way to the
	// rebuild notice; the failure itself stays visible in the status
	// verdict and the empty-state body until a reload succeeds.
	noticeStyle := render.Style{Foreground: muted, Background: background}
	switch {
	case app.errText != "" && app.errSeq > app.noticeSeq:
		canvas.Text(2, area.H-2, " load failed: "+app.errText+" ", render.Style{Foreground: red, Background: background})
	case app.notice != "":
		canvas.Text(2, area.H-2, " "+app.notice+" ", noticeStyle)
	}

	footer := " r reload  x cancel  p index  up/down select  enter read  esc quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})

	state = app.report()
	if app.observe != nil {
		select {
		case app.observe <- state:
		default:
		}
	}
}

// demoFeedRows is the deterministic feed table the demo source serves
// in chunks of three.
var demoFeedRows = []feedRow{
	{"builds", "fresh", 40},
	{"releases", "fresh", 85},
	{"logs", "degraded", 640},
	{"errors", "fresh", 120},
	{"metrics", "stale", 910},
	{"api", "fresh", 65},
	{"search", "fresh", 210},
}

// demoSource streams the deterministic table with real sleeps: chunky
// on the Feeds side, stepped on the Index side, and honest about
// cancellation on both.
type demoSource struct {
	chunkDelay time.Duration
	stepDelay  time.Duration

	mu    sync.Mutex
	chunk map[int]int
	step  map[int]int
}

func newDemoSource(chunkDelay, stepDelay time.Duration) *demoSource {
	return &demoSource{
		chunkDelay: chunkDelay,
		stepDelay:  stepDelay,
		chunk:      map[int]int{},
		step:       map[int]int{},
	}
}

func (s *demoSource) Feeds(ctx context.Context, req int) ([]feedRow, bool, error) {
	s.mu.Lock()
	cursor := s.chunk[req]
	s.chunk[req] = cursor + 3
	s.mu.Unlock()
	timer := time.NewTimer(s.chunkDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	start := min(cursor, len(demoFeedRows))
	end := min(cursor+3, len(demoFeedRows))
	return demoFeedRows[start:end], end >= len(demoFeedRows), nil
}

func (s *demoSource) Index(ctx context.Context, job int) (int, int, error) {
	s.mu.Lock()
	cursor := s.step[job]
	s.step[job] = cursor + 1
	s.mu.Unlock()
	timer := time.NewTimer(s.stepDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	}
	done := min(cursor+1, 5)
	return done, 5, nil
}

func main() {
	if err := lux.Run(context.Background(), newFeedsApp(newDemoSource(250*time.Millisecond, 150*time.Millisecond), nil, nil)); err != nil {
		fmt.Fprintln(os.Stderr, "feeds:", err)
		os.Exit(1)
	}
}
