// Command procs is the stage-2 reference application: a task and process
// explorer. It shows the full asynchronous pattern on real data — loading,
// empty, error, and retry states; type-to-search filtering with a
// single-flight load and one deferred replay; a virtualized table; a
// master-detail pair; and a command with a side effect (killing a
// process) that confirms first, reports its own failure as a notice next
// to the table, and refreshes the data after it succeeds — with every
// operating system call isolated behind the processSource seam.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"unicode/utf8"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The palette moved to the library after two real screens converged on
// it: ui.ReferencePalette is now the single source, and the local names
// below alias it so the chrome calls stay short and readable.
var (
	palette    = ui.ReferencePalette()
	ink        = palette.Ink
	muted      = palette.Muted
	violet     = palette.Violet
	cyan       = palette.Cyan
	background = palette.Background
	red        = render.RGBColor(248, 113, 113) // the destructive ask, never data rows
)

// tableColumns is package-level so the routed Apply and the draw call share
// the exact same slice: header hit-testing derives from the drawn layout.
var tableColumns = []ui.TableColumn{
	{Title: "PID", Width: 7},
	{Title: "NAME", Weight: 3},
	{Title: "STATE", Weight: 2},
	{Title: "CPU", Width: 7},
	{Title: "MEM", Width: 9},
}

// phase is the app's coarse state; every phase renders a full, honest
// frame, because a stateless repaint of stale data would lie.
type phase uint8

const (
	phaseLoading phase = iota // a request is in flight
	phaseData                 // rows are shown
	phaseEmpty                // the filter matched nothing
	phaseError                // the source failed; the transport is still healthy
)

func (p phase) String() string {
	switch p {
	case phaseLoading:
		return "loading"
	case phaseData:
		return "data"
	case phaseEmpty:
		return "empty"
	case phaseError:
		return "error"
	}
	return "unknown"
}

// procsResult is the application payload a load Command reports back
// through event.Message. A failed load is data: the model renders it, the
// run never ends because of it. Only a transport-level event.Error ends
// the run, and that is the backend's decision, not this app's.
type procsResult struct {
	id   int
	rows []processRow
	err  error
}

// actionResult is the application payload a kill Command reports back
// through the same seam. A failed kill is also data — a notice beside the
// table, while the rows it was aimed at stay on screen — so the run never
// ends because of it either. Load failures and action failures render in
// different places because they are different facts.
type actionResult struct {
	pid int
	err error
}

type procsApp struct {
	width, height int
	ui            ui.Context
	table         ui.TableState
	source        processSource
	observe       chan procsState

	started        bool       // the initial resize has issued the first load
	filter         string     // the filter the user has typed
	refresh        bool       // a replay is deferred behind the in-flight load
	requestID      int        // identity of the in-flight request
	inflight       bool       // at most one List runs at a time
	confirmRow     processRow // the row pending a kill confirmation; pid 0 means off
	actionInflight bool       // at most one Kill runs at a time
	notice         string     // the outcome line for the last action
	phase          phase
	rows           []processRow
	order          []int
	errText        string
}

// procsState is what the tests observe from View; the channel is drained
// non-blockingly so it can never steer the run loop.
type procsState struct {
	phase    phase
	rows     int
	filter   string
	errText  string
	selected int
	detail   string
	notice   string
	confirm  int // pid pending a kill confirmation, 0 when the ask is off
}

func newProcsApp(source processSource, observe chan procsState) *procsApp {
	app := &procsApp{source: source, observe: observe}
	// The palette is named once, in the library, and the widgets
	// resolve it: the table below draws with a zero TableStyle, so its
	// cells read the Text role, its header the Muted role (underlined
	// by the widget), its focused selected row the Focus role, and its
	// unfocused selection the Accent role. Danger stays zero — this app
	// shows no disabled rows, and an empty role is an opt-out, not a
	// promise. Chrome that is not a widget state (borders, the red kill
	// ask, status lines, the detail pane) keeps its explicit styles:
	// those are facts of the layout, not themeable control states.
	app.ui.UseTheme(palette.Theme())
	return app
}

// issueLoad starts a load for the current filter, or defers it while one
// is already in flight: the deferred replay is a single flag, so pending
// work is bounded to one request no matter how fast the user types.
func (app *procsApp) issueLoad() lux.Command {
	if app.inflight {
		app.refresh = true
		return nil
	}
	return app.load()
}

// load returns the Command that asks the source for the current filter.
// Everything the goroutine touches is captured here by value; the model
// is never read or written from the worker side of the seam.
func (app *procsApp) load() lux.Command {
	app.requestID++
	id := app.requestID
	app.inflight = true
	app.refresh = false
	app.phase = phaseLoading
	// A fresh dataset invalidates a pending confirmation: the selected row
	// the user was asked about may not exist in the new answer, so the ask
	// itself is retired rather than kept pointing at stale coordinates.
	app.confirmRow = processRow{}
	filter, source := app.filter, app.source
	return func(ctx context.Context) event.Event {
		rows, err := source.List(ctx, filter)
		return event.Event{Kind: event.Message, Data: procsResult{id: id, rows: rows, err: err}}
	}
}

// kill returns the Command that runs the confirmed termination. It
// follows the load pattern exactly: locals captured by value, the worker
// goroutine never touches the model, and the result crosses the seam as
// an event.Message payload whose error is data for the UI.
func (app *procsApp) kill(row processRow) lux.Command {
	if app.actionInflight {
		return nil // at most one action in flight; the notice already says so
	}
	app.actionInflight = true
	app.notice = fmt.Sprintf("terminating pid %d (%s)...", row.pid, row.name)
	source, pid := app.source, row.pid
	return func(ctx context.Context) event.Event {
		return event.Event{Kind: event.Message, Data: actionResult{pid: pid, err: source.Kill(ctx, pid)}}
	}
}

// applyAction folds one kill result into the model. A failure keeps the
// data and the selection exactly where they were — the notice carries the
// reason — because an action failing must not look like the table failing.
func (app *procsApp) applyAction(result actionResult) {
	app.actionInflight = false
	if result.err != nil {
		app.notice = fmt.Sprintf("kill %d failed: %v", result.pid, result.err)
		return
	}
	app.notice = fmt.Sprintf("terminated pid %d", result.pid)
}

// applyResult folds one load result into the model. It runs inside
// Update, on the run loop, so the model is only ever touched from there.
func (app *procsApp) applyResult(result procsResult) {
	app.inflight = false
	switch {
	case result.err != nil:
		app.phase = phaseError
		app.errText = result.err.Error()
		app.rows, app.order = nil, app.order[:0]
	default:
		app.errText = ""
		app.rows = result.rows
		app.rebuildOrder()
		if len(app.order) == 0 {
			app.phase = phaseEmpty
		} else {
			app.phase = phaseData
		}
	}
}

// rebuildOrder rebuilds the index view and re-sorts it, keeping the
// selection inside the new bounds.
func (app *procsApp) rebuildOrder() {
	app.order = make([]int, len(app.rows))
	for i := range app.order {
		app.order[i] = i
	}
	app.sortRows()
	if app.table.Selected >= len(app.order) {
		app.table.Selected = 0
	}
}

// beginConfirm freezes the selected row as the target of a kill
// confirmation. It is called from the table's Activate routing — Enter or
// Space on the focused table — so the ask is about the row the widget
// itself considers selected, not a re-derivation from app state.
func (app *procsApp) beginConfirm() {
	if app.phase != phaseData || len(app.order) == 0 || app.actionInflight {
		return
	}
	app.confirmRow = app.rows[app.order[app.table.Selected]]
}

// applyTable routes one action into the table state and re-sorts the view
// when the widget reports a sort request. A keyboard activation (Enter or
// Space) on the focused table is the user asking about the selected row,
// so it opens the kill confirmation; a pointer press on a row selects, it
// does not confirm, which keeps the mouse a navigation hand.
func (app *procsApp) applyTable(action ui.Action) {
	_, activated, sortRequested := app.table.Apply(action, tableColumns, len(app.order))
	if activated && action.Event.Kind == event.Key && app.confirmRow.pid == 0 {
		app.beginConfirm()
	}
	if sortRequested {
		app.sortRows()
		app.table.SortRequested = false // request served: keep the state clean
	}
}

// sortRows re-orders the index view. The widget never reorders data
// itself; the app owns the order.
func (app *procsApp) sortRows() {
	column, desc := app.table.SortColumn, app.table.SortDesc
	compare := func(x, y processRow) int {
		switch column {
		case 0:
			return cmpInt(x.pid, y.pid)
		case 3:
			if x.cpu < y.cpu {
				return -1
			} else if x.cpu > y.cpu {
				return 1
			}
			return 0
		case 4:
			return cmpInt64(x.mem, y.mem)
		default:
			if x.name < y.name {
				return -1
			} else if x.name > y.name {
				return 1
			}
			if x.state < y.state {
				return -1
			} else if x.state > y.state {
				return 1
			}
			return 0
		}
	}
	sort.SliceStable(app.order, func(i, j int) bool {
		a, b := app.rows[app.order[i]], app.rows[app.order[j]]
		if desc {
			a, b = b, a
		}
		return compare(a, b) < 0
	})
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// row materializes one row; Table calls it only for the visible window.
func (app *procsApp) row(index int) []string {
	r := app.rows[app.order[index]]
	return []string{
		strconv.Itoa(r.pid),
		r.name,
		r.state,
		fmt.Sprintf("%.1f", r.cpu),
		memString(r.mem),
	}
}

// memString renders a byte count in the column's compact form.
func memString(mem int64) string {
	switch {
	case mem >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(mem)/(1<<30))
	case mem >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(mem)/(1<<20))
	case mem >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(mem)/(1<<10))
	}
	return fmt.Sprintf("%dB", mem)
}

func (app *procsApp) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		if action.Target == "quit" {
			// Escape while a confirmation is on screen answers the ask —
			// cancel it — instead of leaving the app, so the destructive
			// question never turns a reflexive Escape into an exit.
			if app.confirmRow.pid != 0 {
				app.confirmRow = processRow{}
				return nil
			}
			return lux.Quit()
		}
		if action.Target == "table" {
			app.applyTable(action)
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
		if !app.started {
			app.started = true
			return app.load()
		}
	case event.Key:
		switch next.Key.Code {
		case event.KeyEscape:
			return lux.Quit()
		case event.KeyF5:
			return app.issueLoad()
		case event.KeyBackspace:
			if _, size := utf8.DecodeLastRuneInString(app.filter); size > 0 {
				app.filter = app.filter[:len(app.filter)-size]
				return app.issueLoad()
			}
		case event.KeyRune:
			// While the confirmation is on screen, y answers and n or any
			// other rune is swallowed: the filter must not quietly collect
			// the keystrokes a modal ask is consuming.
			if app.confirmRow.pid != 0 {
				switch next.Key.Rune {
				case 'y', 'Y':
					row := app.confirmRow
					app.confirmRow = processRow{}
					return app.kill(row)
				case 'n', 'N':
					app.confirmRow = processRow{}
				}
				return nil
			}
			app.filter += string(next.Key.Rune)
			return app.issueLoad()
		}
	case event.Message:
		// An action result reports its own outcome first: a kill failure is
		// a notice next to the table, never a load error, and a success
		// refreshes the data so the gone process disappears on its own.
		if result, ok := next.Data.(actionResult); ok {
			if !app.actionInflight {
				return nil // a late or foreign action result: ignored
			}
			app.applyAction(result)
			if result.err == nil {
				return app.issueLoad()
			}
			return nil
		}
		result, ok := next.Data.(procsResult)
		if !ok {
			return nil // a foreign payload is not ours to interpret
		}
		if result.id != app.requestID {
			return nil // a superseded request's result: ignored by identity
		}
		app.applyResult(result)
		if app.refresh {
			// The deferred replay of the newest filter, now that the
			// in-flight request has landed.
			return app.load()
		}
	}
	return nil
}

func (app *procsApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	if area.W < 44 || area.H < 14 {
		canvas.Text(0, 0, "LUX procs - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX PROCS ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	// While the kill ask is on screen it replaces the filter line: the same
	// row carries both questions, and the answer never types into the
	// filter, because the runes are swallowed above.
	if app.confirmRow.pid != 0 {
		canvas.Text(2, 1, fmt.Sprintf(" kill pid %d (%s)?  y confirm  n/esc cancel ", app.confirmRow.pid, app.confirmRow.name), render.Style{Foreground: red, Background: background, Attrs: render.AttrBold})
	} else {
		canvas.Text(2, 1, " filter: ", render.Style{Foreground: muted, Background: background})
		filterText := app.filter
		if filterText == "" {
			filterText = "(type to match names)"
		}
		canvas.Text(11, 1, filterText, render.Style{Foreground: ink, Background: background})
	}

	// The table's styles are all theme roles now: a zero TableStyle
	// means cells, header, selection, and focus resolve through the
	// palette newProcsApp installed.
	var style ui.TableStyle
	body := render.Rect{X: 2, Y: 2, W: area.W - 4, H: area.H - 10}
	switch app.phase {
	case phaseLoading:
		canvas.Text(4, 3, "loading processes...", render.Style{Foreground: ink, Background: background})
	case phaseError:
		canvas.Text(4, 3, "error: "+app.errText, render.Style{Foreground: ink, Background: background})
		canvas.Text(4, 4, " F5 retries the request ", render.Style{Foreground: muted, Background: background})
	case phaseEmpty:
		canvas.Text(4, 3, "no processes match "+strconv.Quote(app.filter), render.Style{Foreground: ink, Background: background})
	case phaseData:
		app.ui.Table("table", canvas, body, &app.table, tableColumns, len(app.order), app.row, style)
		app.drawDetail(canvas, area)
	}

	sortInfo := "unsorted"
	if app.table.SortActive {
		direction := "asc"
		if app.table.SortDesc {
			direction = "desc"
		}
		sortInfo = fmt.Sprintf("%s %s", tableColumns[app.table.SortColumn].Title, direction)
	}
	status := fmt.Sprintf(" %s  %d rows  row %d  sort %s ", app.phase, len(app.order), app.table.Selected+1, sortInfo)
	if app.notice != "" {
		status = fmt.Sprintf(" %s  %s  %d rows  row %d  sort %s ", app.phase, app.notice, len(app.order), app.table.Selected+1, sortInfo)
	}
	canvas.Text(2, area.H-2, status, render.Style{Foreground: muted, Background: background})
	footer := " type filter  backspace edit  F5 refresh  up/down select  enter kill  esc quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})
	app.report()
}

// drawDetail renders the master-detail pane for the selected row.
func (app *procsApp) drawDetail(canvas *render.Canvas, area render.Rect) {
	if len(app.order) == 0 {
		return
	}
	r := app.rows[app.order[app.table.Selected]]
	box := render.Rect{X: 2, Y: area.H - 8, W: area.W - 4, H: 5}
	canvas.Border(box, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(4, area.H-8, " DETAIL ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})
	canvas.Text(4, area.H-7, fmt.Sprintf(" pid %d   name %s ", r.pid, r.name), render.Style{Foreground: ink, Background: background})
	canvas.Text(4, area.H-6, fmt.Sprintf(" cpu %.1f   mem %s   state %s ", r.cpu, memString(r.mem), r.state), render.Style{Foreground: muted, Background: background})
}

func (app *procsApp) report() {
	if app.observe == nil {
		return
	}
	detail := ""
	if len(app.order) > 0 {
		detail = app.rows[app.order[app.table.Selected]].name
	}
	select {
	case app.observe <- procsState{
		phase:    app.phase,
		rows:     len(app.order),
		filter:   app.filter,
		errText:  app.errText,
		selected: app.table.Selected,
		detail:   detail,
		notice:   app.notice,
		confirm:  app.confirmRow.pid,
	}:
	default:
	}
}

func main() {
	source, err := newProcessSource()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := lux.Run(context.Background(), newProcsApp(source, nil)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
