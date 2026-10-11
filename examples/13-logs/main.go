// Command logs demonstrates a bounded, live log view: a capped history
// ring that counts what it drops, an incremental filter over level and
// message, and a follow-tail that pins the newest line — yet yields the
// moment the user scrolls away, keeping the screen steady under the
// cursor until the view is back at the tail.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	cyan       = render.RGBColor(34, 211, 238)
	red        = render.RGBColor(239, 68, 68)
	amber      = render.RGBColor(245, 158, 11)
	background = render.RGBColor(8, 10, 18)
)

// historyCap bounds the retained history; anything older is dropped and
// counted, never silently discarded.
const historyCap = 500

// logEntry is one line of history. seq is the arrival order and the
// identity the cursor anchors to: indices shift when the cap drops old
// lines, seq never does.
type logEntry struct {
	seq   int
	at    time.Time
	level string
	msg   string
}

// logBatch is the payload a stream tick reports back through
// event.Message.
type logBatch struct {
	lines []logEntry
}

// logSource is where lines come from. Poll waits until a batch is ready
// or ctx ends; Done reports whether anything more can arrive. A
// production app would tail a file or subscribe to a service — the seam
// is what the demo keeps honest.
type logSource interface {
	Poll(ctx context.Context) []logEntry
	Done() bool
}

// logLines seeds a plausible service log: mostly routine traffic with
// enough warn/error lines to make level colors and the filter matter.
var logLines = []struct{ level, msg string }{
	{"info", "route matched /api/v1"},
	{"info", "cache hit"},
	{"warn", "cache miss"},
	{"info", "conn opened"},
	{"error", "upstream 503"},
	{"info", "conn closed"},
	{"warn", "gc pause"},
	{"info", "heartbeat ok"},
	{"warn", "retry backoff"},
	{"error", "route missed /old"},
}

// timedSource generates one to three lines per poll, weighted by the
// table above, with times from the clock. It never runs dry: a live tail
// has no end.
type timedSource struct {
	n int
}

func newTimedSource() *timedSource { return &timedSource{} }

func (s *timedSource) Poll(ctx context.Context) []logEntry {
	if ctx.Err() != nil {
		return nil
	}
	s.n++
	count := 1 + s.n%3
	lines := make([]logEntry, 0, count)
	for i := 0; i < count; i++ {
		h := uint64(s.n+i) * 0x9E3779B97F4A7C15
		h ^= h >> 30
		t := logLines[int(h%uint64(len(logLines)))]
		lines = append(lines, logEntry{at: time.Now(), level: t.level, msg: fmt.Sprintf("%s seq=%d", t.msg, s.n+i)})
	}
	s.n += count - 1
	return lines
}

func (s *timedSource) Done() bool { return false }

// logsState is the snapshot the View reports for tests and any external
// observer.
type logsState struct {
	lines       int // view size (entries matching the filter)
	entries     int // retained history
	dropped     int // lines dropped by the cap, all time
	selected    int
	offset      int
	anchor      int // seq of the entry under the cursor
	filter      string
	filterFocus bool
	follow      bool
}

// logsApp is the Model: bounded history plus the view state around it.
type logsApp struct {
	width, height int
	ui            ui.Context
	list          ui.ListState
	filter        ui.TextInput
	entries       []logEntry
	cap           int
	dropped       int
	nextSeq       int
	order         []int
	follow        bool
	anchorSeq     int // identity the reading position is anchored to
	anchorRow     int // screen row the anchor occupies
	viewRows      int
	source        logSource
	interval      time.Duration
	started       bool
	observe       chan logsState
}

func newLogsDemo(cap int, source logSource, interval time.Duration) *logsApp {
	return &logsApp{
		cap:      max(cap, 1),
		filter:   ui.TextInput{Placeholder: "filter level or message"},
		follow:   true,
		source:   source,
		interval: interval,
		observe:  make(chan logsState, 16),
	}
}

// appendBatch is the write path: it stamps identities, enforces the cap
// by dropping the oldest lines (counting every drop), and rebuilds the
// view — pinning the tail while following, otherwise keeping the entry
// the cursor holds on the screen row it already occupies.
func (app *logsApp) appendBatch(lines []logEntry) {
	for _, line := range lines {
		app.nextSeq++
		line.seq = app.nextSeq
		app.entries = append(app.entries, line)
	}
	if overflow := len(app.entries) - app.cap; overflow > 0 {
		app.entries = append(app.entries[:0], app.entries[overflow:]...)
		app.dropped += overflow
	}
	app.refilter()
}

// refilter rebuilds the view: the filter decides which lines survive,
// then the reading position is restored — pinned to the tail in follow
// mode, otherwise onto the anchor's screen row.
func (app *logsApp) refilter() {
	app.order = app.order[:0]
	query := strings.ToLower(app.filter.Value)
	for i := range app.entries {
		if filterMatches(app.entries[i], query) {
			app.order = append(app.order, i)
		}
	}
	if app.follow {
		app.pinTail()
		return
	}
	app.reanchor()
}

// pinTail parks the cursor on the newest line. While the user follows,
// arriving lines must move the view — that is what follow means.
func (app *logsApp) pinTail() {
	app.list.Selected = max(len(app.order)-1, 0)
	app.list.Offset = app.maxOffset()
}

// reanchor restores the reading position after a rebuild: the anchor
// entry returns to the screen row it occupied, which cancels the index
// shift a cap trim causes. An anchor that left — dropped past the cap or
// filtered away — cannot hold the cursor: it clamps to a valid row,
// stays visible, and the anchor becomes the line the cursor lands on.
func (app *logsApp) reanchor() {
	for i, idx := range app.order {
		if app.entries[idx].seq == app.anchorSeq {
			app.list.Selected = i
			app.list.Offset = app.clampOffset(i - app.anchorRow)
			return
		}
	}
	app.clampSelection()
	if len(app.order) > 0 {
		app.anchorSeq = app.entries[app.order[app.list.Selected]].seq
	}
	app.anchorRow = min(max(app.list.Selected-app.list.Offset, 0), max(app.viewRows-1, 0))
}

// clampSelection bounds the cursor to the view: a rebuild (filter or
// drops) can leave the old indices past the end, and the cursor must
// never point at a line that is not on screen.
func (app *logsApp) clampSelection() {
	app.list.Selected = min(max(app.list.Selected, 0), max(len(app.order)-1, 0))
	app.list.Offset = app.clampOffset(app.list.Offset)
	if app.list.Selected < app.list.Offset {
		app.list.Offset = app.list.Selected
	}
	if last := app.list.Offset + app.viewRows - 1; app.viewRows > 0 && app.list.Selected > last {
		app.list.Offset = app.list.Selected - app.viewRows + 1
	}
}

// syncAnchor records the reading position — which entry the cursor
// holds, and on which screen row — so the next rebuild can restore it.
// After a deliberate user move, identity follows the cursor.
func (app *logsApp) syncAnchor() {
	app.clampSelection()
	if len(app.order) > 0 {
		app.anchorSeq = app.entries[app.order[app.list.Selected]].seq
	}
	app.anchorRow = min(max(app.list.Selected-app.list.Offset, 0), max(app.viewRows-1, 0))
}

// applyList routes one user action into the view, then re-derives
// follow: the user owns the scroll until the cursor is back on the
// newest line. Appends never recompute follow — only the user can.
func (app *logsApp) applyList(action ui.Action) {
	app.list.Apply(action, len(app.order), app.viewRows)
	app.follow = app.cursorAtTail()
	app.syncAnchor()
}

// cursorAtTail reports whether the cursor sits on the newest line with
// the view fully scrolled to it: the state in which arriving lines may
// move the screen again.
func (app *logsApp) cursorAtTail() bool {
	if len(app.order) == 0 {
		return true
	}
	return app.list.Selected >= len(app.order)-1 && app.list.Offset >= app.maxOffset()
}

// maxOffset is the lowest offset that still shows the newest line.
func (app *logsApp) maxOffset() int {
	return max(len(app.order)-max(app.viewRows, 1), 0)
}

func (app *logsApp) clampOffset(offset int) int {
	return min(max(offset, 0), app.maxOffset())
}

// seqAt returns the identity of the view row at index, or -1 when the
// index is out of the view.
func (app *logsApp) seqAt(index int) int {
	if index < 0 || index >= len(app.order) {
		return -1
	}
	return app.entries[app.order[index]].seq
}

// filterMatches compares case-insensitively against level and message;
// an empty query matches everything.
func filterMatches(e logEntry, query string) bool {
	return query == "" ||
		strings.Contains(strings.ToLower(e.level), query) ||
		strings.Contains(strings.ToLower(e.msg), query)
}

// setFilter is the filter write path shared by typing and clearing.
func (app *logsApp) setFilter(query string) {
	app.filter.SetValue(query)
	app.refilter()
}

// tick returns the Command that waits one interval and reports the next
// batch. The source is read on the worker side of the seam — the model
// is never touched from there — and a cancelled run yields the zero
// event, which the loop drops.
func (app *logsApp) tick() lux.Command {
	source := app.source
	interval := app.interval
	return func(ctx context.Context) event.Event {
		if ctx.Err() != nil {
			return event.Event{}
		}
		select {
		case <-ctx.Done():
			return event.Event{}
		case <-time.After(interval):
			return event.Event{Kind: event.Message, Data: logBatch{lines: source.Poll(ctx)}}
		}
	}
}

func (app *logsApp) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch {
		case action.Target == "quit" && action.Kind == ui.Activate:
			// Escape over a live query is a filter clear, not an exit;
			// only a bare Escape on an empty query quits.
			if app.filter.Value != "" {
				app.setFilter("")
				return nil
			}
			return lux.Quit()
		case action.Target == "filter" && action.Kind == ui.Focus:
			app.ui.Focus("filter")
		case action.Target == "filter" && action.Kind == ui.Input:
			if app.filter.Apply(action) {
				app.refilter()
			}
			// Enter commits the query and hands focus back to the log
			// view, where the reading position has been following along.
			if action.Event.Kind == event.Key && action.Event.Key.Code == event.KeyEnter {
				app.ui.Focus("logs")
			}
		case action.Target == "logs":
			app.applyList(action)
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
		if !app.started {
			// The first resize starts the stream; the demo has no other
			// entry point into the command seam.
			app.started = true
			return app.tick()
		}
	case event.Message:
		if batch, ok := next.Data.(logBatch); ok {
			app.appendBatch(batch.lines)
			if !app.source.Done() {
				return app.tick() // the stream continues
			}
		}
	case event.Key:
		if next.Key.Code == event.KeyEscape {
			return lux.Quit()
		}
	}
	return nil
}

func (app *logsApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)

	// Priority Escape first: whatever has focus, a bare Escape can leave.
	app.ui.Shortcut(ui.Shortcut{
		Key:      event.KeyEvent{Code: event.KeyEscape},
		Target:   "quit",
		Kind:     ui.Activate,
		Scope:    ui.ScopeApp,
		Priority: true,
	})

	if area.W < 44 || area.H < 12 {
		canvas.Text(0, 0, "LUX logs - enlarge terminal", render.Style{Foreground: violet, Background: background})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX LOGS ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	// The log region registers before the filter on purpose: the stream
	// is the primary surface, so it keeps the initial focus, and
	// refinement is one '/' away.
	body := render.Rect{X: 2, Y: 2, W: area.W - 4, H: area.H - 6}
	app.viewRows = max(body.H, 0)
	app.clampSelection()
	app.ui.RegionWithOptions("logs", canvas, body, ui.Focusable|ui.Scrollable|ui.Activatable, ui.RegionOptions{
		ContentHeight: min(body.H, max(len(app.order)-app.list.Offset, 0)),
		AtTop:         app.list.Offset <= 0,
		AtBottom:      app.list.Offset >= max(len(app.order)-body.H, 0),
	})
	app.paintBody(canvas, body)

	filterStyle := ui.InputStyle{
		Normal:      render.Style{Foreground: ink, Background: background},
		Placeholder: render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim},
	}
	app.ui.Input("filter", canvas, render.Rect{X: 2, Y: 1, W: area.W - 4, H: 1}, &app.filter, filterStyle)
	// Non-priority, so '/' typed into the editor still reaches it as
	// text.
	app.ui.Shortcut(ui.Shortcut{
		Key:    event.KeyEvent{Code: event.KeyRune, Rune: '/'},
		Target: "filter",
		Kind:   ui.Focus,
		Scope:  ui.ScopeApp,
	})

	// Status: view size, drop count, cursor position, follow state.
	lineInfo := "line -"
	if len(app.order) > 0 {
		lineInfo = fmt.Sprintf("line %d", app.list.Selected+1)
	}
	viewInfo := fmt.Sprintf("%d lines", len(app.order))
	if app.filter.Value != "" {
		viewInfo = fmt.Sprintf("%d match %q", len(app.order), app.filter.Value)
	}
	parts := []string{viewInfo}
	if app.dropped > 0 {
		parts = append(parts, fmt.Sprintf("+%d dropped", app.dropped))
	}
	followInfo := "follow"
	if !app.follow {
		followInfo = "paused"
	}
	parts = append(parts, lineInfo, followInfo)
	canvas.Text(2, area.H-2, clipLine(" "+strings.Join(parts, "  ")+" ", area.W-4), render.Style{Foreground: muted, Background: background})

	// Footer with honest degrade.
	footer := " up/down/page scroll  wheel scroll  click line  end follow  / filter  esc quit "
	if len([]rune(footer)) > area.W-4 {
		footer = " scroll  end follow  / filter  esc quit "
	}
	if len([]rune(footer)) > area.W-4 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-len([]rune(footer)))/2, 0), area.H-1, footer, render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim})

	app.report()
}

// paintBody draws the log window: a per-level color per line, the cursor
// reversed on its line. A row longer than the window clips at the edge —
// a log line is a line, not a scrollable column. The empty view states
// why it is empty: waiting, or filtered to nothing.
func (app *logsApp) paintBody(canvas *render.Canvas, body render.Rect) {
	rows := canvas.Clip(body)
	if body.Empty() {
		return
	}
	if len(app.order) == 0 {
		hint := " waiting for lines "
		if app.filter.Value != "" {
			hint = fmt.Sprintf(" no lines match %q ", app.filter.Value)
		}
		rows.Fill(body, " ", render.Style{Foreground: ink, Background: background})
		x := max((body.W-runeLen(hint))/2, 0)
		rows.Text(body.X+x, body.Y+1, clipLine(hint, body.W-x), render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim})
		return
	}
	count := len(app.order)
	for row := 0; row < body.H; row++ {
		index := app.list.Offset + row
		if index >= count {
			break
		}
		entry := app.entries[app.order[index]]
		style := levelStyle(entry.level)
		if index == app.list.Selected {
			if app.ui.Focused("logs") {
				style = render.Style{Foreground: background, Background: cyan}
			} else {
				style = render.Style{Foreground: background, Background: violet}
			}
		}
		y := body.Y + row
		rows.Fill(render.Rect{X: body.X, Y: y, W: body.W, H: 1}, " ", style)
		marker := "  "
		if index == app.list.Selected {
			marker = "> "
		}
		rows.Text(body.X, y, clipLine(marker+formatEntry(entry), body.W), style)
	}
}

// levelStyle colors a line by severity: warn amber, error red, the rest
// ink. The theme has no warn/success colors, so levels own these.
func levelStyle(level string) render.Style {
	switch level {
	case "warn":
		return render.Style{Foreground: amber, Background: background}
	case "error":
		return render.Style{Foreground: red, Background: background}
	}
	return render.Style{Foreground: ink, Background: background}
}

// formatEntry renders one history line: clock, level, message.
func formatEntry(e logEntry) string {
	return fmt.Sprintf("%02d:%02d:%02d %-5s %s", e.at.Hour(), e.at.Minute(), e.at.Second(), strings.ToUpper(e.level), e.msg)
}

// clipLine truncates a line to width graphemes; ui.clipText stays
// package-private, and a log line must never spill past its window.
func clipLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runeLen(s) <= width {
		return s
	}
	return string([]rune(s)[:width])
}

func runeLen(s string) int {
	return len([]rune(s))
}

// report publishes a non-blocking snapshot of the view state.
func (app *logsApp) report() {
	if app.observe == nil {
		return
	}
	select {
	case app.observe <- logsState{
		lines:       len(app.order),
		entries:     len(app.entries),
		dropped:     app.dropped,
		selected:    app.list.Selected,
		offset:      app.list.Offset,
		anchor:      app.anchorSeq,
		filter:      app.filter.Value,
		filterFocus: app.ui.Focused("filter"),
		follow:      app.follow,
	}:
	default:
	}
}

func main() {
	if err := lux.Run(context.Background(), newLogsDemo(historyCap, newTimedSource(), 250*time.Millisecond)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
