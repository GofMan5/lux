// Command jobs is the stage-6 reference flow: bounded jobs with progress,
// telemetry, a follow-tail log, cancellation, and retry. The source seam is
// deterministic in the demo and replaceable by a real queue in an app.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

const logCap = 80

type jobStatus string

const (
	queued    jobStatus = "queued"
	running   jobStatus = "running"
	succeeded jobStatus = "succeeded"
	failed    jobStatus = "failed"
	canceled  jobStatus = "canceled"
)

type job struct {
	name, detail string
	status       jobStatus
	done, total  int
	logs         []string
	dropped      int
}

type telemetry struct {
	events, dropped, maxQueue, completed int
}

type jobStep struct {
	id, done, total int
	line            string
	final           bool
	err             error
}

type jobSource interface {
	Step(context.Context, int, int) jobStep
}

type demoSource struct{}

func (demoSource) Step(ctx context.Context, id, cursor int) jobStep {
	if err := ctx.Err(); err != nil {
		return jobStep{id: id, err: err}
	}
	if cursor >= 5 {
		return jobStep{id: id, done: 5, total: 5, final: true, line: "verified output"}
	}
	return jobStep{id: id, done: cursor + 1, total: 5, line: fmt.Sprintf("worker %d: completed unit %d", id, cursor+1)}
}

type jobsApp struct {
	width, height int
	ui            ui.Context
	list          ui.ListState
	jobs          []job
	source        jobSource
	rootCtx       context.Context
	cancel        context.CancelFunc
	nextID        int
	activeID      int
	activeJob     int
	jobCtx        context.Context
	jobCancel     context.CancelFunc
	cursor        int
	follow        bool
	logOffset     int
	notice        string
	telemetry     telemetry
	started       bool
}

func newJobsApp(source jobSource) *jobsApp {
	ctx, cancel := context.WithCancel(context.Background())
	return &jobsApp{source: source, rootCtx: ctx, cancel: cancel, follow: true, jobs: []job{
		{name: "nightly backup", detail: "archive + checksum", status: queued, total: 5},
		{name: "index rebuild", detail: "search documents", status: queued, total: 5},
		{name: "report export", detail: "write csv bundle", status: queued, total: 5},
	}}
}

func (app *jobsApp) selectedJob() *job {
	if app.list.Selected < 0 || app.list.Selected >= len(app.jobs) {
		return nil
	}
	return &app.jobs[app.list.Selected]
}

func (app *jobsApp) startJob(retry bool) lux.Command {
	if app.activeID != 0 {
		return nil
	}
	j := app.selectedJob()
	if j == nil || (!retry && j.status == succeeded) {
		return nil
	}
	if retry {
		j.logs, j.dropped, j.done = nil, 0, 0
	}
	j.status, j.total = running, 5
	app.nextID++
	app.activeID = app.nextID
	app.activeJob = app.list.Selected
	app.jobCtx, app.jobCancel = context.WithCancel(app.rootCtx)
	id, ctx, source := app.activeID, app.jobCtx, app.source
	app.cursor, app.logOffset, app.notice = 0, 0, ""
	return app.stepCommand(id, ctx, source)
}

func (app *jobsApp) stepCommand(id int, ctx context.Context, source jobSource) lux.Command {
	cursor := app.cursor
	return func(_ context.Context) event.Event {
		return event.Event{Kind: event.Message, Data: source.Step(ctx, id, cursor)}
	}
}

func (app *jobsApp) appendLog(j *job, line string) {
	if line == "" {
		return
	}
	j.logs = append(j.logs, line)
	app.telemetry.events++
	if len(j.logs) > logCap {
		j.logs = j.logs[len(j.logs)-logCap:]
		j.dropped++
		app.telemetry.dropped++
	}
	app.telemetry.maxQueue = max(app.telemetry.maxQueue, len(j.logs))
	if app.follow {
		app.logOffset = max(len(j.logs)-app.logRows(), 0)
	}
}

func (app *jobsApp) logRows() int { return max(app.height-10, 1) }

func (app *jobsApp) applyStep(step jobStep) lux.Command {
	if step.id != app.activeID {
		return nil
	}
	if app.activeJob < 0 || app.activeJob >= len(app.jobs) {
		return nil
	}
	j := &app.jobs[app.activeJob]
	if step.err != nil {
		app.activeID = 0
		if errors.Is(step.err, context.Canceled) {
			j.status, app.notice = canceled, "cancelled; r retries"
		} else {
			j.status, app.notice = failed, step.err.Error()
		}
		if app.jobCancel != nil {
			app.jobCancel()
		}
		app.jobCtx, app.jobCancel = nil, nil
		return nil
	}
	j.done, j.total = step.done, max(step.total, 1)
	app.cursor++
	app.appendLog(j, step.line)
	if step.final || j.done >= j.total {
		j.status, app.activeID, app.telemetry.completed = succeeded, 0, app.telemetry.completed+1
		app.notice = fmt.Sprintf("%s completed", j.name)
		if app.jobCancel != nil {
			app.jobCancel()
		}
		app.jobCtx, app.jobCancel = nil, nil
		return nil
	}
	return app.stepCommand(step.id, app.jobCtx, app.source)
}

func (app *jobsApp) cancelJob() {
	if app.activeID == 0 {
		return
	}
	if app.jobCancel != nil {
		app.jobCancel()
	}
	app.notice = "cancelling..."
}

func (app *jobsApp) moveLog(delta int) {
	j := app.selectedJob()
	if j == nil {
		return
	}
	app.follow = false
	app.logOffset = min(max(app.logOffset+delta, 0), max(len(j.logs)-app.logRows(), 0))
}

func (app *jobsApp) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "quit":
			app.cancel()
			return lux.Quit()
		case "jobs":
			app.list.Apply(action, len(app.jobs), max(app.height-7, 1))
			app.follow, app.logOffset = true, 0
		case "run":
			return app.startJob(false)
		case "retry":
			return app.startJob(true)
		case "cancel":
			app.cancelJob()
		case "follow":
			app.follow = true
			if j := app.selectedJob(); j != nil {
				app.logOffset = max(len(j.logs)-app.logRows(), 0)
			}
		case "log-up":
			app.moveLog(-1)
		case "log-down":
			app.moveLog(1)
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
		if !app.started {
			app.started = true
			return app.startJob(false)
		}
	case event.Message:
		if step, ok := next.Data.(jobStep); ok {
			return app.applyStep(step)
		}
	}
	return nil
}

func (app *jobsApp) View(frame *lux.Frame) {
	canvas, area := frame.Canvas, frame.Canvas.Bounds()
	canvas.Clear(render.Style{Background: render.RGBColor(8, 10, 18)})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'r'}, Target: "retry", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x'}, Target: "cancel", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'f'}, Target: "follow", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'l'}, Target: "log-up", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'L'}, Target: "log-down", Kind: ui.Activate, Scope: ui.ScopeApp})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: ' '}, Target: "run", Kind: ui.Activate, Scope: ui.ScopeApp})
	if area.W < 60 || area.H < 18 {
		canvas.Text(0, 0, "LUX jobs - enlarge terminal", render.Style{})
		return
	}
	ink := render.Style{Foreground: render.RGBColor(229, 231, 235), Background: render.RGBColor(8, 10, 18)}
	muted := render.Style{Foreground: render.RGBColor(107, 114, 128), Background: ink.Background}
	accent := render.Style{Foreground: render.RGBColor(34, 211, 238), Background: ink.Background, Attrs: render.AttrBold}
	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, accent)
	canvas.Text(3, 0, " LUX JOBS ", accent)
	canvas.Text(3, 1, " queue ", muted)
	items := make([]string, len(app.jobs))
	for i, j := range app.jobs {
		items[i] = fmt.Sprintf("%-18s %-9s %d/%d", j.name, j.status, j.done, j.total)
	}
	app.ui.List("jobs", canvas, render.Rect{X: 2, Y: 3, W: 34, H: area.H - 8}, &app.list, items, ui.ListStyle{Normal: ink, Selected: render.Style{Foreground: render.RGBColor(34, 211, 238), Background: ink.Background}})
	j := app.selectedJob()
	if j != nil {
		canvas.Text(38, 2, j.name+" / "+j.detail, accent)
		pct := 100 * j.done / max(j.total, 1)
		canvas.Text(38, 3, fmt.Sprintf("progress %3d%%  status %s", pct, j.status), ink)
		follow := "paused"
		if app.follow {
			follow = "follow"
		}
		canvas.Text(38, 4, fmt.Sprintf("events %d  dropped %d  queue-max %d  %s", app.telemetry.events, app.telemetry.dropped, app.telemetry.maxQueue, follow), muted)
		logs := j.logs
		start := min(max(app.logOffset, 0), max(len(logs)-app.logRows(), 0))
		for row, line := range logs[start:] {
			if row >= app.logRows() {
				break
			}
			canvas.Text(38, 6+row, line, ink)
		}
	}
	status := app.notice
	if status == "" {
		status = "space run  r retry  x cancel  f follow-tail  up/down select  esc quit"
	}
	canvas.Text(2, area.H-2, strings.TrimSpace(status), muted)
}

func main() {
	if err := lux.Run(context.Background(), newJobsApp(demoSource{})); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
