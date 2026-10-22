package main

import (
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/layout"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
	"github.com/GofMan5/lux/ui"
)

const minWidth, minHeight = 96, 28

func (app *studio) pal() ui.Palette { return themes[app.theme].palette }

// fg paints a color on the screen background: every panel shares it, so the
// zero-style widgets (which paint on Palette.Background) blend in.
func (app *studio) fg(color render.Color) render.Style {
	return render.Style{Foreground: color, Background: app.pal().Background}
}

func (app *studio) bold(color render.Color) render.Style {
	style := app.fg(color)
	style.Attrs = render.AttrBold
	return style
}

// body is the page area between the header and the footer.
func (app *studio) body() render.Rect {
	return render.Rect{X: 1, Y: 2, W: app.width - 2, H: app.height - 3}
}

func (app *studio) View(frame *lux.Frame) {
	canvas := frame.Canvas
	now := time.Now()
	app.width, app.height = canvas.Size()
	app.frames++
	app.reduced = frame.ReducedMotion || app.reduce.Checked
	pal := app.pal()
	canvas.Clear(render.Style{Foreground: pal.Ink, Background: pal.Background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)

	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'q', Mod: event.ModCtrl}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'p', Mod: event.ModCtrl}, Target: "open-palette", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyF1}, Target: "open-help", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	if app.width < minWidth || app.height < minHeight {
		msg := fmt.Sprintf("LUX Studio needs %dx%d, this terminal is %dx%d", minWidth, minHeight, app.width, app.height)
		canvas.Text(max((app.width-canvas.TextWidth(msg))/2, 0), app.height/2, msg, app.bold(pal.Cyan))
		return
	}

	app.flushSidebands(frame)
	if !app.toastEnd.IsZero() && now.After(app.toastEnd) {
		app.toast, app.toastEnd = "", time.Time{}
	}
	if !app.reduced && !app.paused && app.tabs.Selected == pageMotion {
		app.advanceLab(frame.Delta)
	}

	app.drawHeader(canvas, frame, now)
	switch app.tabs.Selected {
	case pageDashboard:
		if now.Sub(app.sampled) >= sampleEvery[app.rate.Selected] {
			app.sample(now)
		}
		app.drawDashboard(canvas, now)
	case pageServices:
		app.drawServices(canvas, now)
	case pageFiles:
		app.drawFiles(canvas)
	case pageMotion:
		app.drawMotion(canvas, frame)
	case pageSettings:
		app.drawSettings(canvas, frame)
	}
	app.drawFooter(canvas)

	switch {
	case app.confirm != "":
		app.drawConfirm(canvas)
	case app.palette:
		app.drawPalette(canvas)
	case app.help:
		app.drawHelp(canvas)
	}
	app.schedule(frame, now)
}

// flushSidebands sends what Update queued: clipboard, notification, and the
// taskbar progress mirror of the deploy job (only when it changes).
func (app *studio) flushSidebands(frame *lux.Frame) {
	if app.copyText != "" {
		frame.CopyToClipboard(app.copyText)
		app.copyText = ""
	}
	if app.notify[0] != "" {
		frame.Notify(app.notify[0], app.notify[1])
		app.notify = [2]string{}
	}
	progress := -1
	if app.deploy.running {
		progress = app.deploy.step * 100 / (len(stages) * stageSteps)
	}
	if progress != app.progressSent {
		if progress < 0 {
			frame.SetProgress(lux.ProgressNone, 0)
		} else {
			frame.SetProgress(lux.ProgressNormal, progress)
		}
		app.progressSent = progress
	}
}

// schedule asks for the next frame only when something visible will change.
func (app *studio) schedule(frame *lux.Frame, now time.Time) {
	if !frame.Capabilities.Interactive {
		return
	}
	at, continuous := app.wakeup(now)
	switch {
	case continuous:
		frame.RequestFrame()
	case !at.IsZero():
		frame.RequestFrameAt(at)
	}
}

// wakeup returns when the screen next changes: a toast expiring, the next
// dashboard sample, a spinner glyph, or every frame while a spring settles or
// the motion lab runs. A zero time and false mean the app can sleep.
func (app *studio) wakeup(now time.Time) (at time.Time, continuous bool) {
	want := func(t time.Time) {
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	nextGlyph := func(started time.Time) time.Time {
		return now.Add(spinnerGlyph - now.Sub(started)%spinnerGlyph)
	}
	if !app.toastEnd.IsZero() {
		want(app.toastEnd)
	}
	switch app.tabs.Selected {
	case pageDashboard:
		want(app.sampled.Add(sampleEvery[app.rate.Selected]))
		continuous = !app.reduced && app.springsMoving(now)
	case pageServices:
		for _, started := range app.restarting {
			if !app.reduced {
				want(nextGlyph(started))
			}
		}
	case pageMotion:
		continuous = !app.reduced && !app.paused
	}
	if app.deploy.running && !app.reduced {
		want(nextGlyph(app.deploy.spinner.Started))
	}
	return at, continuous
}

func (app *studio) springsMoving(now time.Time) bool {
	for _, m := range app.metrics {
		position, velocity := m.spring.Value(now)
		if math.Abs(position-m.spring.To) > m.max/500 || math.Abs(velocity) > m.max/100 {
			return true
		}
	}
	return false
}

func (app *studio) drawHeader(canvas *render.Canvas, frame *lux.Frame, now time.Time) {
	pal := app.pal()
	canvas.Text(1, 0, " ◆ LUX STUDIO ", render.Style{Foreground: pal.Background, Background: pal.Cyan, Attrs: render.AttrBold})
	right := ""
	if app.showStats.Checked {
		right = fmt.Sprintf("%d fps  %s  frame %d ", frame.Metrics.TargetFPS, frame.Metrics.PresentTime.Round(10*time.Microsecond), app.frames)
	}
	rightW := canvas.TextWidth(right)
	canvas.Text(app.width-rightW-1, 0, right, app.fg(pal.Muted))
	tabsW := app.width - 18 - rightW - 16
	app.ui.Tabs("tabs", canvas, render.Rect{X: 17, Y: 0, W: tabsW, H: 1}, &app.tabs, pages, ui.TabsStyle{})
	if app.deploy.running {
		x := app.width - rightW - 15
		app.spin(canvas, x, 0, &app.deploy.spinner, now)
		canvas.Text(x+2, 0, fmt.Sprintf("deploy %3d%%", app.progressSent), app.fg(pal.Cyan))
	}
	for x := 1; x < app.width-1; x++ {
		canvas.Put(x, 1, "─", app.fg(pal.Muted))
	}
}

func (app *studio) drawFooter(canvas *render.Canvas) {
	pal := app.pal()
	y := app.height - 1
	hints := []string{"1-5", "pages", "Tab", "focus", "Ctrl+P", "commands", "t", "theme", "F1", "help", "q", "quit"}
	x := 2
	for i := 0; i < len(hints); i += 2 {
		x += canvas.Text(x, y, hints[i], app.bold(pal.Cyan))
		x += canvas.Text(x, y, " "+hints[i+1]+"   ", app.fg(pal.Muted))
	}
	if app.toast != "" {
		toast := " " + app.toast + " "
		canvas.Text(app.width-canvas.TextWidth(toast)-1, y, toast, render.Style{Foreground: pal.Background, Background: pal.Violet, Attrs: render.AttrBold})
	}
}

// panel draws a titled rounded frame and returns its padded interior.
func (app *studio) panel(canvas *render.Canvas, r render.Rect, title string) render.Rect {
	pal := app.pal()
	canvas.Border(r, render.RoundedBorder, app.fg(pal.Muted))
	if title != "" {
		canvas.Text(r.X+2, r.Y, " "+title+" ", app.bold(pal.Cyan))
	}
	return r.Inset(2, 1)
}

func (app *studio) spin(canvas *render.Canvas, x, y int, state *ui.SpinnerState, now time.Time) {
	if app.reduced {
		now = state.Started // a still glyph under reduced motion
	}
	app.ui.Spinner(canvas, render.Rect{X: x, Y: y, W: 1, H: 1}, state, now, ui.SpinnerStyle{
		Frames: ui.SpinnerBraille, FrameDuration: spinnerGlyph, Style: app.fg(app.pal().Cyan),
	})
}

func (app *studio) gauge(canvas *render.Canvas, r render.Rect, value, limit float64, fill render.Color) {
	state := ui.GaugeState{Value: value, Max: limit}
	app.ui.Gauge(canvas, r, &state, ui.GaugeStyle{Fill: app.fg(fill), Track: app.fg(app.pal().Muted)})
}

func (app *studio) button(id ui.ID, canvas *render.Canvas, r render.Rect, label string) {
	pal := app.pal()
	app.ui.Button(id, canvas, r, label, ui.ButtonStyle{
		Normal:  app.fg(pal.Ink),
		Hovered: app.fg(pal.Cyan),
		Pressed: render.Style{Foreground: pal.Background, Background: pal.Violet, Attrs: render.AttrBold},
	})
}

// ---- Dashboard ----

func (app *studio) drawDashboard(canvas *render.Canvas, now time.Time) {
	pal, th := app.pal(), themes[app.theme]
	body := app.body()
	top, rest := body.SplitTop(max(8, body.H*2/5))
	cards := layout.Flex(app.slots[:], top, layout.Horizontal, 1,
		layout.Constraint{Grow: 1}, layout.Constraint{Grow: 1}, layout.Constraint{Grow: 1})
	for i, r := range cards {
		m := &app.metrics[i]
		inner := app.panel(canvas, r, m.name)
		value := m.spring.To
		if !app.reduced {
			value, _ = m.spring.Value(now)
		}
		headline := fmt.Sprintf("%.0f%s", value, m.unit)
		canvas.Text(inner.Right()-canvas.TextWidth(headline), inner.Y, headline, app.bold(pal.Ink))
		lo, hi := m.values[0], m.values[0]
		for _, v := range m.values {
			lo, hi = min(lo, v), max(hi, v)
		}
		canvas.Text(inner.X, inner.Y, fmt.Sprintf("lo %.0f  hi %.0f", lo, hi), app.fg(pal.Muted))
		spark := ui.SparklineState{Values: m.values, Max: m.max}
		app.ui.Sparkline(canvas, render.Rect{X: inner.X, Y: inner.Y + 1, W: inner.W, H: inner.H - 3}, &spark, ui.SparklineStyle{Fill: app.fg(pal.Cyan)})
		fill := th.ok
		if fraction := value / m.max; fraction > .8 {
			fill = th.bad
		} else if fraction > .6 {
			fill = th.warn
		}
		app.gauge(canvas, render.Rect{X: inner.X, Y: inner.Bottom() - 1, W: inner.W, H: 1}, value, m.max, fill)
	}

	bottom := layout.Flex(app.slots[:], rest, layout.Horizontal, 1, layout.Constraint{Grow: 3}, layout.Constraint{Grow: 2})
	app.drawPipeline(canvas, bottom[0], now)
	inner := app.panel(canvas, bottom[1], "Activity")
	app.ui.List("activity", canvas, inner, &app.activityAt, app.activity, ui.ListStyle{})
}

func (app *studio) drawPipeline(canvas *render.Canvas, r render.Rect, now time.Time) {
	pal, th := app.pal(), themes[app.theme]
	inner := app.panel(canvas, r, "Deploy pipeline · async lux.Command")
	label := "▶  Deploy"
	if app.deploy.running {
		label = "■  Cancel"
	}
	app.button("deploy", canvas, render.Rect{X: inner.X, Y: inner.Y, W: 15, H: 3}, label)
	total := len(stages) * stageSteps
	status, color := "idle · press d or click Deploy", pal.Muted
	switch {
	case app.deploy.running:
		status, color = fmt.Sprintf("running %s · step %d/%d", stages[min(app.deploy.step/stageSteps, len(stages)-1)], app.deploy.step, total), pal.Cyan
	case app.deploy.failed:
		status, color = "cancelled at step "+fmt.Sprint(app.deploy.step), th.bad
	case !app.deploy.finished.IsZero():
		status, color = "finished at "+app.deploy.finished.Format("15:04:05"), th.ok
	}
	canvas.Text(inner.X+17, inner.Y+1, status, app.fg(color))
	app.gauge(canvas, render.Rect{X: inner.X, Y: inner.Y + 4, W: inner.W - 6, H: 1}, float64(app.deploy.step), float64(total), pal.Violet)
	canvas.Text(inner.Right()-4, inner.Y+4, fmt.Sprintf("%3d%%", app.deploy.step*100/total), app.bold(pal.Ink))
	for i, stage := range stages {
		y := inner.Y + 6 + i
		if y >= inner.Bottom() {
			break
		}
		done := app.deploy.step - i*stageSteps
		switch {
		case done >= stageSteps:
			canvas.Text(inner.X, y, "✓", app.bold(th.ok))
		case done > 0 && app.deploy.running:
			app.spin(canvas, inner.X, y, &app.deploy.spinner, now)
		case done > 0:
			canvas.Text(inner.X, y, "✗", app.bold(th.bad))
		default:
			canvas.Text(inner.X, y, "·", app.fg(pal.Muted))
		}
		canvas.Text(inner.X+2, y, stage, app.fg(pal.Ink))
		app.gauge(canvas, render.Rect{X: inner.X + 12, Y: y, W: inner.W - 12, H: 1}, float64(min(max(done, 0), stageSteps)), stageSteps, pal.Cyan)
	}
}

// ---- Services ----

func (app *studio) drawServices(canvas *render.Canvas, now time.Time) {
	pal, th := app.pal(), themes[app.theme]
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: '/'}, Target: "filter", Kind: ui.Focus, Scope: ui.ScopeApp})
	body := app.body()
	cols := layout.Flex(app.slots[:], body, layout.Horizontal, 1, layout.Constraint{Grow: 1}, layout.Constraint{Basis: 42})
	inner := app.panel(canvas, cols[0], fmt.Sprintf("Services %d/%d · click a header to sort", len(app.view), len(app.services)))
	canvas.Text(inner.X, inner.Y, "Filter ›", app.bold(pal.Cyan))
	app.ui.Input("filter", canvas, render.Rect{X: inner.X + 9, Y: inner.Y, W: inner.W - 9, H: 1}, &app.filter, ui.InputStyle{
		Placeholder: render.Style{Foreground: pal.Muted, Background: pal.Background, Attrs: render.AttrItalic},
	})
	app.ui.Table("services", canvas, render.Rect{X: inner.X, Y: inner.Y + 2, W: inner.W, H: inner.H - 2}, &app.table, serviceColumns, len(app.view),
		func(i int) []string {
			s := app.services[app.view[i]]
			latency := "-"
			if s.latency > 0 {
				latency = fmt.Sprint(s.latency)
			}
			return []string{s.name, s.region, s.status, latency}
		}, ui.TableStyle{})

	detail := app.panel(canvas, cols[1], "Details")
	s, ok := app.current()
	if !ok {
		canvas.Text(detail.X, detail.Y, "no service matches the filter", app.fg(pal.Muted))
		return
	}
	canvas.Text(detail.X, detail.Y, s.name, app.bold(pal.Ink))
	color := map[string]render.Color{"healthy": th.ok, "degraded": th.warn, "down": th.bad, "restarting": pal.Cyan}[s.status]
	y := detail.Y + 2
	if started, ok := app.restarting[s.name]; ok {
		state := ui.SpinnerState{Started: started}
		app.spin(canvas, detail.X, y, &state, now)
	} else {
		canvas.Text(detail.X, y, "●", app.fg(color))
	}
	canvas.Text(detail.X+2, y, s.status, app.bold(color))
	canvas.Text(detail.X+14, y, "region "+s.region, app.fg(pal.Muted))
	if s.latency > 0 {
		canvas.Text(detail.X+2, y+1, fmt.Sprintf("p95 latency %d ms", s.latency), app.fg(pal.Muted))
	}
	wrapped := canvas.TextWrap(render.Rect{X: detail.X, Y: y + 3, W: detail.W, H: 4}, s.about, render.WrapOptions{}, app.fg(pal.Ink))
	y += 4 + wrapped.Height
	canvas.Text(detail.X, y, "Open in browser (OSC 8, Ctrl+click):", app.fg(pal.Muted))
	app.link(canvas, detail.X, y+1, detail.W, s.url)
	buttons := render.Rect{X: detail.X, Y: detail.Bottom() - 3, W: detail.W, H: 3}
	halves := layout.Flex(app.slots[:], buttons, layout.Horizontal, 2, layout.Constraint{Grow: 1}, layout.Constraint{Grow: 1})
	app.button("copy-url", canvas, halves[0], "Copy URL")
	app.button("restart", canvas, halves[1], "Restart…")
}

// link writes text as an OSC 8 hyperlink: the same cells, plus a Link that
// terminals with hyperlink support turn into a clickable destination.
func (app *studio) link(canvas *render.Canvas, x, y, width int, uri string) {
	style := app.fg(app.pal().Cyan)
	style.Attrs, style.UnderlineStyle = render.AttrUnderline, render.UnderlineCurly
	for i, r := range uri {
		if i >= width {
			break
		}
		canvas.PutCell(x+i, y, render.Cell{Grapheme: string(r), Width: 1, Style: style, Link: render.Link{URI: uri}})
	}
}

func (app *studio) drawConfirm(canvas *render.Canvas) {
	pal := app.pal()
	r := render.Rect{X: (app.width - 46) / 2, Y: (app.height - 9) / 2, W: 46, H: 9}
	inner := app.ui.Modal("confirm", canvas, r, "Confirm restart", app.modalStyle())
	inner.Text(1, 1, "Restart "+app.confirm+"?", app.bold(pal.Ink))
	inner.Text(1, 2, "Connections drain first. Esc cancels.", app.fg(pal.Muted))
	app.button("confirm-yes", inner, render.Rect{X: 1, Y: 4, W: 18, H: 3}, "Restart")
	app.button("confirm-no", inner, render.Rect{X: 21, Y: 4, W: 18, H: 3}, "Cancel")
	app.ui.EndModal()
}

func (app *studio) modalStyle() ui.ModalStyle {
	pal := app.pal()
	return ui.ModalStyle{Panel: app.fg(pal.Ink), Border: app.fg(pal.Violet), Title: app.bold(pal.Cyan)}
}

// ---- Files ----

func (app *studio) drawFiles(canvas *render.Canvas) {
	pal, th := app.pal(), themes[app.theme]
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 's', Mod: event.ModCtrl}, Owner: "editor", Target: "editor-save", Kind: ui.Activate})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'c', Mod: event.ModCtrl}, Owner: "editor", Target: "editor-copy", Kind: ui.Activate})
	body := app.body()
	cols := layout.Flex(app.slots[:], body, layout.Horizontal, 1, layout.Constraint{Basis: 30}, layout.Constraint{Grow: 1})
	tree := app.panel(canvas, cols[0], "Project")
	rows := app.treeRows()
	app.ui.Tree("tree", canvas, tree, &app.tree, len(rows), func(i int) ui.TreeRow { return rows[i] },
		ui.TreeStyle{Collapsed: "▸", Expanded: "▾", Leaf: " "})

	title := app.open
	if app.editor.Text() != app.saved[app.open] {
		title += " ●"
	}
	editor := app.panel(canvas, cols[1], title)
	app.ui.TextArea("editor", canvas, render.Rect{X: editor.X, Y: editor.Y, W: editor.W, H: editor.H - 2}, &app.editor, ui.TextAreaStyle{
		Normal: app.fg(pal.Ink),
	})
	line := app.editor.Lines[min(app.editor.Line, len(app.editor.Lines)-1)]
	col := utf8.RuneCountInString(line[:min(app.editor.Offset, len(line))]) + 1
	status := fmt.Sprintf("Ln %d, Col %d · %d lines", app.editor.Line+1, col, len(app.editor.Lines))
	canvas.Text(editor.X, editor.Bottom()-1, status, app.fg(pal.Muted))
	keys := "Ctrl+S save · Ctrl+Z undo · Ctrl+C copy"
	if app.editor.Text() != app.saved[app.open] {
		keys = "unsaved · " + keys
	}
	canvas.Text(editor.Right()-canvas.TextWidth(keys), editor.Bottom()-1, keys, app.fg(th.warn))
}

// ---- Motion ----

func (app *studio) labNow() time.Time { return app.labEpoch.Add(app.labTime) }

// advanceLab moves the lab clock: pausing freezes every animation on the
// page at once because they all sample this clock, not the wall clock.
func (app *studio) advanceLab(delta time.Duration) {
	delta = min(delta, 250*time.Millisecond)
	app.labTime += delta
	app.angle += delta.Seconds() * .9
	now := app.labNow()
	if _, done := app.tween.Value(now); done {
		app.tween = motion.Tween{From: app.tween.To, To: 1 - app.tween.To, Start: now, Duration: app.tween.Duration, Ease: app.tween.Ease}
	}
}

func (app *studio) drawMotion(canvas *render.Canvas, frame *lux.Frame) {
	pal, th := app.pal(), themes[app.theme]
	body := app.body()
	cols := layout.Flex(app.slots[:], body, layout.Horizontal, 1, layout.Constraint{Grow: 1}, layout.Constraint{Grow: 1})
	lab := app.panel(canvas, cols[0], "Motion lab")
	now := app.labNow()

	position, velocity := app.spring.Value(now)
	if app.reduced {
		position, velocity = app.spring.To, 0 // the lab clock is frozen: show where it lands
	}
	canvas.Text(lab.X, lab.Y, "SPRING", app.bold(pal.Ink))
	canvas.Text(lab.X+8, lab.Y, "click anywhere on the track", app.fg(pal.Muted))
	track := render.Rect{X: lab.X, Y: lab.Y + 1, W: lab.W, H: 3}
	app.ui.Region("spring-track", canvas, track, ui.Activatable)
	app.track(canvas, track.Y+1, track, position, app.spring.To, pal.Cyan)
	canvas.Text(lab.X, lab.Y+4, fmt.Sprintf("pos %.3f   vel %+.2f/s   target %.2f", position, velocity, app.spring.To), app.fg(pal.Muted))

	value, _ := app.tween.Value(now)
	canvas.Text(lab.X, lab.Y+6, "TWEEN", app.bold(pal.Ink))
	canvas.Text(lab.X+8, lab.Y+6, "ping-pong 1.2 s, easing:", app.fg(pal.Muted))
	app.track(canvas, lab.Y+8, render.Rect{X: lab.X, W: lab.W}, value, -1, pal.Violet)
	canvas.Text(lab.X, lab.Y+9, fmt.Sprintf("value %.3f", value), app.fg(pal.Muted))

	canvas.Text(lab.X, lab.Y+11, "GAUGE", app.bold(pal.Ink))
	canvas.Text(lab.X+8, lab.Y+11, "follows the spring", app.fg(pal.Muted))
	app.gauge(canvas, render.Rect{X: lab.X, Y: lab.Y + 12, W: lab.W, H: 1}, position, 1, th.ok)

	label := "❚❚ Pause"
	if app.paused {
		label = "▶  Resume"
	}
	app.button("pause", canvas, render.Rect{X: lab.X, Y: lab.Bottom() - 3, W: 14, H: 3}, label)
	note := "space pauses the clock; frames stop"
	if app.reduced {
		note = "reduced motion: everything holds still"
	}
	canvas.Subview(lab).Text(16, lab.H-2, note, app.fg(pal.Muted)) // clipped to the panel
	// Selects draw last so their popups paint over the rows below them.
	app.ui.Select("ease", canvas, render.Rect{X: lab.X + 33, Y: lab.Y + 6, W: min(16, lab.W-33), H: 1}, &app.ease, easings, ui.SelectStyle{})

	scene := app.panel(canvas, cols[1], "CPU 3D · scene3d")
	view := render.Rect{X: scene.X, Y: scene.Y + 2, W: scene.W, H: scene.H - 2}
	app.drawScene(canvas.Subview(view), frame)
	canvas.Text(scene.X, scene.Y, "shape", app.fg(pal.Muted))
	app.ui.Select("shape", canvas, render.Rect{X: scene.X + 6, Y: scene.Y, W: 10, H: 1}, &app.shape, shapes, ui.SelectStyle{})
}

// track draws a horizontal rail with a marker at fraction, plus an optional
// target tick (target < 0 hides it).
func (app *studio) track(canvas *render.Canvas, y int, r render.Rect, fraction, target float64, color render.Color) {
	pal := app.pal()
	width := max(r.W, 2)
	for x := 0; x < width; x++ {
		canvas.Put(r.X+x, y, "─", app.fg(pal.Muted))
	}
	at := func(f float64) int { return r.X + min(max(int(math.Round(f*float64(width-1))), 0), width-1) }
	if target >= 0 {
		canvas.Put(at(target), y-1, "▼", app.fg(pal.Muted))
	}
	canvas.Put(at(fraction), y, "●", app.bold(color))
}

func (app *studio) drawScene(canvas *render.Canvas, frame *lux.Frame) {
	switch frame.Capabilities.Color {
	case capability.Monochrome:
		app.renderer.ColorMode = scene3d.Monochrome
	case capability.ANSI16:
		app.renderer.ColorMode = scene3d.ANSI16
	default:
		app.renderer.ColorMode = scene3d.TrueColor
	}
	if r, g, b, ok := app.pal().Background.RGB(); ok {
		app.renderer.Background = scene3d.RGB(r, g, b)
	}
	object := scene3d.Object{
		Mesh:      app.meshes[app.shape.Selected],
		Transform: scene3d.RotateY(app.angle).Mul(scene3d.RotateX(app.angle * .55)),
	}
	app.renderer.Render(canvas, scene3d.Scene{
		Objects: []scene3d.Object{object},
		Camera:  scene3d.Camera{Position: scene3d.V3(0, 0, 4.2), Target: scene3d.V3(0, 0, 0), Up: scene3d.V3(0, 1, 0), FOVY: math.Pi / 3, Near: .1, Far: 100},
		Light:   scene3d.Light{Direction: scene3d.V3(-.5, -.7, -1), Ambient: .18, Diffuse: .82},
	})
}

// ---- Settings ----

func (app *studio) drawSettings(canvas *render.Canvas, frame *lux.Frame) {
	pal, th := app.pal(), themes[app.theme]
	body := app.body()
	cols := layout.Flex(app.slots[:], body, layout.Horizontal, 1, layout.Constraint{Basis: 50}, layout.Constraint{Grow: 1})
	left := app.panel(canvas, cols[0], "Settings")
	x, y := left.X, left.Y
	canvas.Text(x, y, "PALETTE", app.bold(pal.Muted))
	app.ui.RadioGroup("theme", canvas, render.Rect{X: x, Y: y + 1, W: 24, H: len(themes)}, &app.themeRadio, themeNames, ui.RadioStyle{})
	for i, t := range themes {
		for j, c := range []render.Color{t.palette.Violet, t.palette.Cyan, t.ok, t.warn, t.bad} {
			canvas.Put(x+26+j*2, y+1+i, "█", render.Style{Foreground: c, Background: pal.Background})
		}
	}
	y += 2 + len(themes)
	canvas.Text(x, y, "MOTION & DISPLAY", app.bold(pal.Muted))
	app.ui.Checkbox("reduce", canvas, render.Rect{X: x, Y: y + 1, W: left.W, H: 1}, &app.reduce, "reduce motion (springs snap, 3D stops)", ui.CheckboxStyle{})
	app.ui.Checkbox("stats", canvas, render.Rect{X: x, Y: y + 2, W: left.W, H: 1}, &app.showStats, "show frame metrics in the header", ui.CheckboxStyle{})
	canvas.Text(x, y+3, "dashboard sample rate", app.fg(pal.Ink))
	rateY := y + 3
	y += 5
	canvas.Text(x, y, "TERMINAL SIDEBANDS", app.bold(pal.Muted))
	half := (left.W - 2) / 2
	app.button("notify", canvas, render.Rect{X: x, Y: y + 1, W: half, H: 3}, "Notify")
	app.button("copy", canvas, render.Rect{X: x + half + 2, Y: y + 1, W: half, H: 3}, "Copy text")
	y += 5
	canvas.Text(x, y, "CAPABILITIES", app.bold(pal.Muted))
	caps := frame.Capabilities
	flags := []struct {
		name string
		on   bool
	}{
		{"mouse", caps.Mouse}, {"focus", caps.Focus}, {"paste", caps.BracketedPaste},
		{"kitty keys", caps.KeyboardEnhanced}, {"sync output", caps.SynchronizedOutput}, {"hyperlinks", caps.Hyperlinks},
	}
	for i, f := range flags {
		mark, color := "·", pal.Muted
		if f.on {
			mark, color = "✓", th.ok
		}
		fx, fy := x+(i%3)*16, y+1+i/3
		canvas.Text(fx, fy, mark, app.bold(color))
		canvas.Text(fx+2, fy, f.name, app.fg(pal.Ink))
	}
	terminal := caps.Terminal
	if terminal == "" {
		terminal = "unknown"
	}
	canvas.Text(x, y+3, fmt.Sprintf("terminal %s · color %s", terminal, colorName(caps.Color)), app.fg(pal.Muted))
	app.ui.Select("rate", canvas, render.Rect{X: x + 24, Y: rateY, W: 12, H: 1}, &app.rate, sampleRates, ui.SelectStyle{})

	right := app.panel(canvas, cols[1], "Event inspector · every decoded event")
	view := app.ui.ScrollViewport("events", canvas, right, &app.eventsView, len(app.events), ui.ScrollStyle{})
	if view == nil {
		return
	}
	if len(app.events) == 0 {
		view.Text(0, 0, "press keys, click, scroll or paste: every event lands here", app.fg(pal.Muted))
	}
	visible := app.eventsView.Range(len(app.events), right.H)
	for i := visible.First; i < visible.Last; i++ {
		color := pal.Ink
		if i == len(app.events)-1 {
			color = pal.Cyan
		}
		view.Text(0, i-app.eventsView.Offset, app.events[i], app.fg(color))
	}
}

func colorName(depth capability.ColorDepth) string {
	switch depth {
	case capability.Monochrome:
		return "mono"
	case capability.ANSI16:
		return "16"
	case capability.ANSI256:
		return "256"
	}
	return "truecolor"
}

// ---- Overlays ----

func (app *studio) drawPalette(canvas *render.Canvas) {
	pal := app.pal()
	found := app.matches()
	w := min(64, app.width-8)
	h := min(len(commands)+4, app.height-6)
	r := render.Rect{X: (app.width - w) / 2, Y: 3, W: w, H: h}
	inner := app.ui.Modal("palette", canvas, r, "Command palette · Enter runs · Esc closes", app.modalStyle())
	inner.Text(0, 0, "›", app.bold(pal.Cyan))
	app.ui.Input("palette-query", inner, render.Rect{X: 2, Y: 0, W: inner.Bounds().W - 2, H: 1}, &app.query, ui.InputStyle{
		Placeholder: render.Style{Foreground: pal.Muted, Background: pal.Background, Attrs: render.AttrItalic},
	})
	titles := make([]string, len(found))
	for i, index := range found {
		titles[i] = commands[index].title
	}
	list := render.Rect{X: 0, Y: 1, W: inner.Bounds().W, H: inner.Bounds().H - 1}
	if len(titles) == 0 {
		inner.Text(2, 2, "no command matches", app.fg(pal.Muted))
	}
	app.ui.List("palette-list", inner, list, &app.commands, titles, ui.ListStyle{})
	app.ui.EndModal()
}

func (app *studio) drawHelp(canvas *render.Canvas) {
	pal := app.pal()
	lines := [][2]string{
		{"1 … 5, click a tab", "switch pages"},
		{"Tab / Shift+Tab", "move focus between controls"},
		{"Esc", "leave the filter or editor, close dialogs"},
		{"Enter / Space", "activate the focused control"},
		{"arrows, wheel", "select and scroll"},
		{"Ctrl+P", "command palette (fuzzy)"},
		{"t", "next palette"},
		{"d", "start or cancel the deploy (Dashboard)"},
		{"/", "filter services (Services)"},
		{"Ctrl+S Ctrl+Z Ctrl+C", "save, undo, copy (Files)"},
		{"space", "pause the motion lab (Motion)"},
		{"q, Ctrl+Q", "quit"},
	}
	w, h := min(64, app.width-8), len(lines)+6
	r := render.Rect{X: (app.width - w) / 2, Y: max((app.height-h)/2, 0), W: w, H: h}
	inner := app.ui.Modal("help", canvas, r, "Keys", app.modalStyle())
	for i, l := range lines {
		inner.Text(1, i, l[0], app.bold(pal.Cyan))
		inner.Text(24, i, l[1], app.fg(pal.Ink))
	}
	app.button("help-close", inner, render.Rect{X: inner.Bounds().W - 12, Y: len(lines) + 1, W: 11, H: 3}, "Close")
	app.ui.EndModal()
}
