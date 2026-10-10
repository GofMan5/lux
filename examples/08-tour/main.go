// Command tour is the guided playground: nine tabs, each one a live,
// interactive stand for one area of the library. Resize the terminal, click
// the tab strip, or press 1-9 to walk every feature by eye. Static tabs go
// idle between input; each animated tab asks for frames only while its
// demos actually move.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/layout"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
	"github.com/GofMan5/lux/ui"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	cyan       = render.RGBColor(34, 211, 238)
	green      = render.RGBColor(52, 211, 153)
	red        = render.RGBColor(248, 113, 113)
	amber      = render.RGBColor(251, 191, 36)
	background = render.RGBColor(8, 10, 18)
	panel      = render.RGBColor(15, 18, 31)
)

var tabLabels = []string{"1 Text", "2 Color", "3 Border", "4 Layout", "5 Motion", "6 3D", "7 Widgets", "8 Input", "9 Annex"}

// wrapDemo is the paragraph the text page wraps at the panel width.
const wrapDemo = "LUX measures text in grapheme clusters, so a wide emoji, a flag pair, or a joined family each count as one column of width, exactly like a terminal draws them. Word wrap uses the same authority as the renderer, which is why this paragraph breaks at the same words the terminal would measure, even inside double-width punctuation."

// textSamples is the grapheme sampler: each row shows one class of text the
// cluster engine handles, annotated with its measured cell width.
var textSamples = [][2]string{
	{"latin", "hello LUX"},
	{"cjk wide", "你好，世界"},
	{"wide emoji", "🎉🚀🔥"},
	{"flags", "🇯🇵🇩🇪🇺🇸"},
	{"zwj family", "👨‍👩‍👧‍👦"},
	{"skin tone", "👋🏽"},
	{"combining", "café"},
	{"box drawing", "┌─┐│╭─╮├┤"},
	{"blocks", "▀▄█░▒▓▖▗"},
	{"katakana", "ターミナル"},
}

type tour struct {
	page   int
	tabs   ui.TabsState
	ui     ui.Context
	width  int
	height int

	// Motion tab.
	tween   motion.Tween
	ease    int
	spring  motion.Spring
	retimer int
	back    motion.Tween // fixed OutBack ping-pong, shows overshoot past 1.0
	// 3D tab.
	angle    float32
	paused   bool
	speedIdx int
	renderer scene3d.Renderer
	mesh     scene3d.Mesh
	// Widgets tab.
	name   ui.TextInput
	list   ui.ListState
	scroll ui.ScrollState
	table  ui.TableState
	slow   ui.CheckboxState
	glyph  ui.RadioState
	gauge  float64
	target float64
	trace  []float64
	sync   ui.SpinnerState
	// Input tab.
	feed     []feedRow
	presses  int
	releases int
	// Annex tab.
	progressMode int
	progress     float64
	annexNote    string
	pendingFire  int // 0 none, 1 notify, 2 clipboard: consumed once in View
	// Telemetry.
	frameTimes []float64
	showHelp   bool
	reduced    bool // latched from Frame.ReducedMotion every View
}

// feedRow is one recorded input-tab line: a decoded key or another event
// worth showing (mouse, focus, paste, suspend, resume).
type feedRow struct {
	kind  string
	desc  string
	extra string
}

func newTour() *tour {
	app := &tour{mesh: scene3d.Cube(1.45)}
	app.name.SetValue("retarget me")
	app.list.Selected = 2
	app.gauge = .25
	app.target = .25
	app.sync.Started = time.Now()
	app.spring.Response = 250 * time.Millisecond
	return app
}

func (app *tour) Update(next event.Event) lux.Command {
	// The input inspector logs every decoded event before routing: what the
	// terminal delivered, unfiltered. If a widget then claims a key, the
	// feed row gains an arrow naming the consumer, so the tab doubles as a
	// routing debugger. Resize and Frame are lifecycle traffic, not input.
	app.recordEvent(next)
	action, handled := app.ui.Handle(next)
	if handled {
		if next.Kind == event.Key {
			app.annotateConsumed(action)
		}
		switch action.Target {
		case "tabs":
			_, _ = app.tabs.Apply(action, tabLabels)
			app.page = app.tabs.Selected
		case "name":
			app.name.Apply(action)
		case "list":
			_, _ = app.list.Apply(action, len(terminalList), max(action.Bounds.H, 1))
		case "scroll":
			app.scroll.Apply(action, scrollContent, max(action.Bounds.H, 1))
		case "table":
			app.table.Apply(action, featureColumns, len(featureRows))
		case "slow":
			app.slow.Apply(action)
		case "glyph":
			app.glyph.Apply(action, len(spinnerNames))
		case "gauge":
			if action.Kind == ui.Activate {
				app.target += .37
				if app.target > 1 {
					app.target -= 1
				}
			}
		case "help":
			if action.Kind == ui.Activate {
				app.showHelp = true
			}
		case "help-modal", "close-help":
			if action.Kind == ui.Dismiss || action.Kind == ui.Activate {
				app.showHelp = false
			}
		case "quit":
			return lux.Quit()
		case "demo-retarget":
			if action.Kind == ui.Activate {
				app.retarget(action.Event.At)
			}
		case "demo-ease":
			if action.Kind == ui.Activate {
				app.ease = (app.ease + 1) % len(easings)
				app.retarget(action.Event.At)
			}
		case "demo-spring":
			if action.Kind == ui.Activate {
				app.retimer = (app.retimer + 1) % len(responses)
				app.spring.Response = responses[app.retimer]
			}
		case "demo-pause":
			if action.Kind == ui.Activate {
				app.paused = !app.paused
			}
		case "demo-speed":
			if action.Kind == ui.Activate {
				app.speedIdx = (app.speedIdx + 1) % len(speeds)
			}
		case "annex-run":
			if action.Kind == ui.Activate {
				app.progressMode, app.progress, app.annexNote = 1, 0, "OSC 9;4: determinate bar, 3s loop"
			}
		case "annex-indet":
			if action.Kind == ui.Activate {
				app.progressMode, app.annexNote = 2, "OSC 9;4: indeterminate spinner"
			}
		case "annex-error":
			if action.Kind == ui.Activate {
				app.progressMode, app.annexNote = 3, "OSC 9;4: error state at 66%"
			}
		case "annex-stop":
			if action.Kind == ui.Activate {
				app.progressMode, app.annexNote = 0, "OSC 9;4: indicator removed"
			}
		case "annex-notify":
			if action.Kind == ui.Activate {
				app.annexNote, app.pendingFire = "OSC 9;777: notification sent (support varies; fires on next presented frame)", 1
			}
		case "annex-copy":
			if action.Kind == ui.Activate {
				app.annexNote, app.pendingFire = "OSC 52: clipboard write sent (support varies; fires on next presented frame)", 2
			}
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Frame:
		app.tick(next.Delta, next.At)
	case event.Key:
		return app.keyPress(next)
	}
	return nil
}

// recordEvent logs one decoded input event before routing. Paste gets a
// bounded preview: the feed shows what arrived, not a wall of pasted text.
func (app *tour) recordEvent(next event.Event) {
	switch next.Kind {
	case event.Key:
		app.recordKey(next.Key)
	case event.Mouse:
		app.recordMouse(next.Mouse)
	case event.Focus:
		app.record(next.Kind.String(), focusName(next.Focused), "")
	case event.Paste:
		app.record(next.Kind.String(), pastePreview(next.Text), "")
	case event.Suspend:
		app.record("suspend", "terminal handed away (ctrl+z)", "")
	case event.Resume:
		app.record("resume", "raw mode back, full repaint", "")
	}
}

// pastePreview caps a paste for the feed row: at most the first runes, then
// the total count so a 10 KB dump stays a one-line fact.
func pastePreview(text string) string {
	const limit = 48
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return fmt.Sprintf("%s… (%d runes total)", string(runes[:limit]), len(runes))
}

// annotateConsumed marks the last feed row with the region that claimed the
// key, turning the input tab into a routing trace.
func (app *tour) annotateConsumed(action ui.Action) {
	index := len(app.feed) - 1
	if index < 0 {
		return
	}
	row := &app.feed[index]
	if row.extra != "" {
		row.extra += " "
	}
	row.extra += "→ " + string(action.Target)
}

// keyPress handles the raw keys that no widget or shortcut claimed: plain q
// quits and digits jump between tabs. Page-local demo keys are registered
// as priority shortcuts during View instead, so they keep working while the
// tab strip holds focus. While an editor owns focus its runes never reach
// this path either, so typing "q9" into the input is safe.
func (app *tour) keyPress(next event.Event) lux.Command {
	key := next.Key
	if key.Action != event.ActionPress && key.Action != event.ActionRepeat {
		return nil
	}
	switch {
	case key.Rune == 'q':
		return lux.Quit()
	case key.Rune >= '1' && key.Rune <= '9':
		app.page = int(key.Rune - '1')
		app.tabs.Selected = app.page
		app.tabs.Offset = 0
	}
	return nil
}

// tick advances every time-based demo with the frame's delta, clamped so a
// debugger pause cannot teleport the animations. Under ReducedMotion the
// decorative demos freeze in place (the gauge snaps, the cube stops) while
// the progress sideband keeps reporting — progress is work state, not
// decoration.
func (app *tour) tick(delta time.Duration, now time.Time) {
	if delta > 250*time.Millisecond {
		delta = 250 * time.Millisecond
	}
	if !app.paused && !app.reduced {
		app.angle += float32(delta.Seconds()) * speeds[app.speedIdx]
	}
	if _, done := app.tween.Value(now); done && !app.reduced {
		app.retarget(now)
	} else if app.reduced {
		app.tween.Start = now // hold the sample still: tween keeps its place
	}
	if _, done := app.back.Value(now); done && !app.reduced {
		app.back = motion.Tween{From: app.back.To, To: 1 - app.back.To, Start: now, Duration: 1400 * time.Millisecond, Ease: motion.OutBack}
	} else if app.reduced {
		app.back.Start = now
	}
	if app.progressMode == 1 {
		app.progress += delta.Seconds() / 3
		if app.progress > 1 {
			app.progress -= 1
		}
	}
	if app.reduced {
		app.gauge = app.target
	} else {
		// The gauge eases toward its target at its own pace, so the bar and
		// the sparkline under it keep moving even when nothing else does.
		step := delta.Seconds() * .8
		if math.Abs(app.target-app.gauge) < step {
			app.gauge = app.target
		} else if app.gauge < app.target {
			app.gauge += step
		} else {
			app.gauge -= step
		}
	}
	app.trace = append(app.trace, app.gauge)
	if len(app.trace) > 96 {
		app.trace = append(app.trace[:0], app.trace[1:]...)
	}
}

// retarget flips both motions mid-flight from where the eye actually is:
// the spring samples its live position (not its old target) and carries its
// velocity; the tween restarts from its live sample under the new easing.
func (app *tour) retarget(now time.Time) {
	position, velocity := app.spring.Value(now)
	app.spring = motion.Spring{From: position, To: 1 - app.spring.To, Velocity: velocity, Start: now, Response: app.spring.Response}
	tweenFrom, _ := app.tween.Value(now)
	app.tween = motion.Tween{From: tweenFrom, To: 1 - app.tween.To, Start: now, Duration: 900 * time.Millisecond, Ease: easings[app.ease].fn}
}

func (app *tour) record(kind, desc, extra string) {
	app.feed = append(app.feed, feedRow{kind, desc, extra})
	if len(app.feed) > 14 {
		app.feed = append(app.feed[:0], app.feed[1:]...)
	}
}

func (app *tour) recordKey(key event.KeyEvent) {
	switch key.Action {
	case event.ActionPress:
		app.presses++
	case event.ActionRelease:
		app.releases++
	}
	app.record(actionName(key.Action), key.String(), fmt.Sprintf("x%d", key.Repeat))
}

// recordMouse keeps hover drags out of the feed: pure motion with no button
// and no modifier would flood the inspector.
func (app *tour) recordMouse(mouse event.MouseEvent) {
	if mouse.Action == event.MouseMove && mouse.Button == event.MouseNone && mouse.Mod == 0 {
		return
	}
	pixel := ""
	if mouse.PixelX >= 0 {
		pixel = fmt.Sprintf(" px(%d,%d)", mouse.PixelX, mouse.PixelY)
	}
	app.record(mouseActionName(mouse.Action), fmt.Sprintf("mouse %s %s @(%d,%d)%s", mouseButtonName(mouse.Button), modString(mouse.Mod), mouse.X, mouse.Y, pixel), "")
}

func (app *tour) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	app.width, app.height = area.W, area.H
	app.reduced = frame.ReducedMotion
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyF1}, Target: "help", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	// Ctrl+Q covers three terminal spellings: legacy C0 0x11 decodes to
	// lowercase q+Ctrl, while enhanced kitty keys report Q+Ctrl (shifted
	// capital) or Q+Ctrl+Shift.
	for _, key := range []event.KeyEvent{
		{Code: event.KeyRune, Rune: 'q', Mod: event.ModCtrl},
		{Code: event.KeyRune, Rune: 'Q', Mod: event.ModCtrl},
		{Code: event.KeyRune, Rune: 'Q', Mod: event.ModCtrl | event.ModShift},
	} {
		app.ui.Shortcut(ui.Shortcut{Key: key, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	}
	// Page-local demo keys are priority shortcuts registered only while
	// their page is showing, so they beat the tab strip's Space activation
	// even when the strip holds focus. digits still switch tabs from the
	// fallback path.
	if app.page == 4 {
		app.pageShortcut(' ', "demo-retarget")
		app.pageShortcut('e', "demo-ease")
		app.pageShortcut('E', "demo-ease")
		app.pageShortcut('v', "demo-spring")
		app.pageShortcut('V', "demo-spring")
	}
	if app.page == 5 {
		app.pageShortcut(' ', "demo-pause")
		app.pageShortcut('s', "demo-speed")
		app.pageShortcut('S', "demo-speed")
	}

	if layout.Classify(area) == layout.Tiny {
		canvas.Text(0, 0, "LUX tour - enlarge the terminal", render.Style{Foreground: violet, Attrs: render.AttrBold})
		return
	}
	app.ui.Tabs("tabs", canvas, render.Rect{X: 1, Y: 1, W: max(area.W-2, 1), H: 1}, &app.tabs, tabLabels, tabsStyle())
	header := fmt.Sprintf("lux-tui tour   %dx%d   %s", area.W, area.H, colorName(frame.Capabilities.Color))
	canvas.Text(max(area.W-canvas.TextWidth(header)-1, 0), 0, header, render.Style{Foreground: muted, Background: background})
	app.frameTimes = append(app.frameTimes, frame.Metrics.PresentTime.Seconds())
	if len(app.frameTimes) > 64 {
		app.frameTimes = append(app.frameTimes[:0], app.frameTimes[1:]...)
	}
	body := render.Rect{X: 1, Y: 2, W: max(area.W-2, 1), H: max(area.H-4, 1)}
	switch app.page {
	case 0:
		app.drawText(canvas, body, frame.Capabilities.Unicode)
	case 1:
		app.drawColor(canvas, body, frame.Capabilities.Color)
	case 2:
		app.drawBorder(canvas, body, frame.Capabilities.Unicode)
	case 3:
		app.drawLayout(canvas, body)
	case 4:
		app.drawMotion(canvas, body, frame.Now)
	case 5:
		app.drawScene(canvas, body, frame)
	case 6:
		app.drawWidgets(canvas, body, frame.Now)
	case 7:
		app.drawInput(canvas, body, frame.Capabilities)
	case 8:
		app.drawAnnex(canvas, body, frame)
	}
	app.drawFooter(frame, area)
	if app.showHelp {
		app.drawHelp(canvas, area)
	}
	// Sidebands are app state, not annex-page state: a progress loop keeps
	// its terminal indicator fresh from any tab, and a queued one-shot
	// (notify/copy) fires from the first presented frame after the click.
	// One-shot semantics: a frame dropped before present loses the request,
	// so nothing fires — click again.
	app.applySidebands(frame)
	if delay, moving := app.wantsFrame(frame.Now); moving && frame.Capabilities.Interactive {
		if delay <= 0 {
			frame.RequestFrame()
		} else {
			frame.RequestFrameAfter(delay)
		}
	}
}

// pageShortcut registers one page-local demo key as a priority shortcut.
func (app *tour) pageShortcut(r rune, target ui.ID) {
	app.ui.Shortcut(ui.Shortcut{
		Key:      event.KeyEvent{Code: event.KeyRune, Rune: r},
		Target:   target,
		Kind:     ui.Activate,
		Scope:    ui.ScopeApp,
		Priority: true,
	})
}

// applySidebands drives the terminal progress indicator and fires queued
// one-shot sidebands. Determinate progress (mode 1) is work state: it keeps
// flowing under ReducedMotion and from every tab.
func (app *tour) applySidebands(frame *lux.Frame) {
	switch app.progressMode {
	case 1:
		frame.SetProgress(lux.ProgressNormal, int(app.progress*100))
	case 2:
		frame.SetProgress(lux.ProgressIndeterminate, 0)
	case 3:
		frame.SetProgress(lux.ProgressError, 66)
	default:
		frame.SetProgress(lux.ProgressNone, 0)
	}
	switch app.pendingFire {
	case 1:
		frame.Notify("LUX tour", "sideband notification from the tour's annex tab")
		app.pendingFire = 0
	case 2:
		frame.CopyToClipboard("copied from the LUX tour")
		app.pendingFire = 0
	}
}

// wantsFrame reports whether anything visible still moves and when the
// next frame is due (zero delay = as soon as the cap allows). Static tabs
// report false so the app sleeps between input. ReducedMotion freezes every
// decorative demo; the progress sideband alone keeps asking, because a
// determinate progress bar is work state, not decoration.
func (app *tour) wantsFrame(now time.Time) (time.Duration, bool) {
	if app.progressMode == 1 {
		return 0, true
	}
	switch app.page {
	case 4:
		// The motion page retargets forever, so it animates continuously.
		return 0, !app.reduced
	case 5:
		return 0, !app.paused && !app.reduced
	case 6:
		if app.reduced {
			return 0, false
		}
		if math.Abs(app.target-app.gauge) > 1e-4 {
			return 0, true
		}
		if app.sync.Started.IsZero() {
			return 0, false
		}
		// The spinner glyph is a pure function of absolute time: ask for a
		// frame exactly when the next glyph flip is due, not every tick.
		duration := app.spinnerDuration()
		age := app.sync.Age(now)
		return duration - age%duration, true
	}
	return 0, false
}

// spinnerDuration matches the widget tab's spinner cadence: the slow
// checkbox triples the frame step.
func (app *tour) spinnerDuration() time.Duration {
	if app.slow.Checked {
		return 240 * time.Millisecond
	}
	return 80 * time.Millisecond
}

func (app *tour) drawFooter(frame *lux.Frame, area render.Rect) {
	canvas := frame.Canvas
	hints := map[int]string{
		0: "grapheme clusters, measured widths, word wrap",
		1: "truecolor gradient, 256 cube, 16 base, SGR attributes",
		2: "five border sets + nesting",
		3: "resize the terminal: the flex tree reflows live",
		4: "[space] retarget  [e] easing  [v] spring response",
		5: fmt.Sprintf("[space] %s  [s] speed %.1f rad/s", pauseName(app.paused), speeds[app.speedIdx]),
		6: "type in the editor, click the button, scroll the log",
		7: "press keys: press / repeat / release decoded here",
		8: "sideband demos: taskbar progress, notify, clipboard",
	}
	footer := fmt.Sprintf(" [1-9] tabs  %s  [q] quit  [F1] help ", hints[app.page])
	footerY := max(area.H-1, 0)
	canvas.Text(1, footerY, footer, render.Style{Foreground: muted, Background: background})
	status := fmt.Sprintf("%d fps  %s", frame.Metrics.TargetFPS, frame.Metrics.PresentTime.Round(100*time.Microsecond))
	if area.W > canvas.TextWidth(footer)+canvas.TextWidth(status)+4 {
		canvas.Text(area.W-canvas.TextWidth(status)-1, footerY, status, render.Style{Foreground: cyan, Background: background})
	}
}

// pagePanel draws the standard page chrome: a bordered, titled panel whose
// inset rect is the working area.
func pagePanel(canvas *render.Canvas, area render.Rect, title string) render.Rect {
	canvas.Fill(area, " ", render.Style{Background: panel})
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: violet, Background: panel})
	canvas.Text(area.X+2, area.Y, " "+title+" ", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
	return area.Inset(2, 1)
}

func (app *tour) drawText(canvas *render.Canvas, area render.Rect, unicode bool) {
	columns := layout.Flex(make([]render.Rect, 2), area, layout.Horizontal, 2,
		layout.Constraint{Basis: area.W / 2, Min: 20, Grow: 1, Shrink: 1},
		layout.Constraint{Basis: area.W / 2, Min: 20, Grow: 1, Shrink: 1},
	)
	// Left: the grapheme sampler, each row annotated with measured width.
	left := pagePanel(canvas, columns[0], "GRAPHEME SAMPLER")
	if !left.Empty() {
		y := left.Y
		if unicode {
			for _, sample := range textSamples {
				if y >= left.Bottom() {
					break
				}
				canvas.Text(left.X, y, sample[0], render.Style{Foreground: muted, Background: panel})
				w := canvas.Text(left.X+14, y, sample[1], render.Style{Foreground: ink, Background: panel})
				canvas.Text(left.X+14+w+1, y, fmt.Sprintf("w=%d", w), render.Style{Foreground: muted, Background: panel})
				y++
			}
		} else {
			canvas.Text(left.X, y, "this terminal reported no Unicode mode;", render.Style{Foreground: muted, Background: panel})
			canvas.Text(left.X, y+1, "the sampler degrades to ASCII labels.", render.Style{Foreground: muted, Background: panel})
		}
	}
	// Right: word wrap against the same measuring authority.
	right := pagePanel(canvas, columns[1], "WORD WRAP")
	if right.Empty() {
		return
	}
	width := max(right.W-2, 8)
	wordLines := render.WrapLines(wrapDemo, width, render.WrapOptions{})
	charLines := render.WrapLines(wrapDemo, width, render.WrapOptions{Mode: render.WrapChar})
	canvas.Text(right.X, right.Y, fmt.Sprintf("WrapWord @ %d cols", width), render.Style{Foreground: muted, Background: panel})
	for index, line := range wordLines {
		if index >= max(right.H/2-1, 1) {
			break
		}
		canvas.Text(right.X, right.Y+1+index, line, render.Style{Foreground: ink, Background: panel})
	}
	charY := right.Y + right.H/2 + 1
	canvas.Text(right.X, charY, fmt.Sprintf("WrapChar @ %d cols", width), render.Style{Foreground: muted, Background: panel})
	for index, line := range charLines {
		if index >= max(right.Bottom()-charY-2, 0) {
			break
		}
		canvas.Text(right.X, charY+1+index, line, render.Style{Foreground: amber, Background: panel})
	}
	metrics := render.MeasureWrap(wrapDemo, width, render.WrapOptions{})
	canvas.Text(right.X, max(right.Bottom()-1, right.Y), fmt.Sprintf("word: %d lines, %d cols", metrics.Height, metrics.Width), render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
}

func (app *tour) drawColor(canvas *render.Canvas, area render.Rect, depth capability.ColorDepth) {
	// A truecolor horizontal gradient: one interpolated RGB cell per column.
	gradient, rest := area.SplitTop(4)
	if gradient.H >= 3 {
		canvas.Fill(gradient, " ", render.Style{Background: panel})
		canvas.Text(gradient.X+2, gradient.Y, " 24-BIT GRADIENT (violet -> cyan) ", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
		for x := 0; x < gradient.W-4; x++ {
			t := float64(x) / math.Max(float64(gradient.W-5), 1)
			color := render.RGBColor(byte(139+float64(34-139)*t), byte(92+float64(211-92)*t), byte(246+float64(238-246)*t))
			canvas.Put(gradient.X+2+x, gradient.Y+2, "▀", render.Style{Foreground: violet, Background: color})
		}
	}
	// The xterm 256 cube: two spaces of background per color, 16 per row.
	palette, attrs := rest.SplitTop(min(18, max(rest.H, 0)))
	if !palette.Empty() {
		canvas.Text(palette.X, palette.Y, "XTERM 256 PALETTE (IndexedColor 16..255)", render.Style{Foreground: muted, Background: background})
		for index := 16; index < 256; index++ {
			col := (index-16)%16*3 + 1
			row := (index-16)/16 + 2
			if row >= palette.H || col+2 > palette.W {
				continue
			}
			canvas.Text(palette.X+col, palette.Y+row, "  ", render.Style{Background: render.IndexedColor(byte(index))})
		}
	}
	if attrs.Empty() {
		return
	}
	canvas.Text(attrs.X, attrs.Y, "ANSI 16 BASE COLORS", render.Style{Foreground: muted, Background: background})
	for index := 0; index < 16 && index*3+2 < attrs.W; index++ {
		canvas.Text(attrs.X+1+index*3, attrs.Y+1, fmt.Sprintf("%2d", index), render.Style{Background: render.ANSIColor(byte(index)), Foreground: ink, Attrs: render.AttrBold})
	}
	attrRows := []struct {
		name string
		attr render.Attr
	}{
		{"bold", render.AttrBold},
		{"dim", render.AttrDim},
		{"italic", render.AttrItalic},
		{"underline", render.AttrUnderline},
		{"strikethrough", render.AttrStrike},
		{"reverse", render.AttrReverse},
		{"blink", render.AttrBlink},
		{"hidden", render.AttrHidden},
	}
	canvas.Text(attrs.X, attrs.Y+3, "SGR ATTRIBUTES", render.Style{Foreground: muted, Background: background})
	for index, row := range attrRows {
		if attrs.Y+4+index >= attrs.Bottom() {
			break
		}
		canvas.Text(attrs.X, attrs.Y+4+index, row.name, render.Style{Foreground: muted, Background: background})
		canvas.Text(attrs.X+16, attrs.Y+4+index, "the quick brown fox 0123", render.Style{Foreground: ink, Background: background, Attrs: row.attr})
	}
	if attrs.Y+13 < attrs.Bottom() {
		canvas.Text(attrs.X, attrs.Y+13, "UNDERLINE STYLES (SGR 4:x + SGR 58 color)", render.Style{Foreground: muted, Background: background})
		styles := []struct {
			name  string
			style render.UnderlineStyle
		}{
			{"single", render.UnderlineSingle},
			{"double", render.UnderlineDouble},
			{"curly", render.UnderlineCurly},
			{"dotted", render.UnderlineDotted},
			{"dashed", render.UnderlineDashed},
		}
		for index, row := range styles {
			if attrs.Y+14+index >= attrs.Bottom() {
				break
			}
			canvas.Text(attrs.X, attrs.Y+14+index, row.name, render.Style{Foreground: muted, Background: background})
			canvas.Text(attrs.X+10, attrs.Y+14+index, "underlined words", render.Style{Foreground: ink, Background: background, Attrs: render.AttrUnderline, UnderlineStyle: row.style, UnderlineColor: cyan})
		}
	}
	switch depth {
	case capability.Monochrome, capability.ANSI16, capability.ANSI256:
		canvas.Text(area.X, max(area.Bottom()-1, area.Y), fmt.Sprintf("terminal reports %s: the engine clamps these cells at present", colorName(depth)), render.Style{Foreground: amber, Background: background})
	}
}

var borders = []struct {
	name   string
	border render.Border
}{
	{"SingleBorder", render.SingleBorder},
	{"RoundedBorder", render.RoundedBorder},
	{"DoubleBorder", render.DoubleBorder},
	{"HeavyBorder", render.HeavyBorder},
	{"ASCIIBorder", render.ASCIIBorder},
}

func (app *tour) drawBorder(canvas *render.Canvas, area render.Rect, unicode bool) {
	top, rest := area.SplitTop(7)
	cells := layout.Flex(make([]render.Rect, 3), top, layout.Horizontal, 2,
		layout.Constraint{Basis: top.W / 3, Min: 16, Grow: 1, Shrink: 1},
		layout.Constraint{Basis: top.W / 3, Min: 16, Grow: 1, Shrink: 1},
		layout.Constraint{Basis: top.W / 3, Min: 16, Grow: 1, Shrink: 1},
	)
	drawCell := func(rect render.Rect, index int) {
		if rect.W < 10 || rect.H < 5 {
			return
		}
		border, color := borders[index].border, violet
		if !unicode {
			border, color = render.ASCIIBorder, ink
		}
		canvas.Border(rect, border, render.Style{Foreground: color, Background: panel})
		canvas.Text(rect.X+2, rect.Y, " "+borders[index].name+" ", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
		canvas.Text(rect.X+2, rect.Y+2, "styled border,", render.Style{Foreground: ink, Background: panel})
		canvas.Text(rect.X+2, rect.Y+3, "per-cell styles", render.Style{Foreground: muted, Background: panel})
	}
	drawCell(cells[0], 0)
	drawCell(cells[1], 1)
	drawCell(cells[2], 2)
	if rest.H >= 7 {
		cells2 := layout.Flex(make([]render.Rect, 2), render.Rect{X: rest.X, Y: rest.Y, W: rest.W, H: 7}, layout.Horizontal, 2,
			layout.Constraint{Basis: rest.W / 2, Min: 16, Grow: 1, Shrink: 1},
			layout.Constraint{Basis: rest.W / 2, Min: 16, Grow: 1, Shrink: 1},
		)
		drawCell(cells2[0], 3)
		outer := cells2[1]
		if outer.W >= 14 && outer.H >= 6 {
			border, color := borders[4].border, cyan
			if !unicode {
				border, color = render.ASCIIBorder, ink
			}
			canvas.Border(outer, border, render.Style{Foreground: color, Background: panel})
			canvas.Text(outer.X+2, outer.Y, " ASCIIBorder + nested ", render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold})
			inner := outer.Inset(3, 2)
			if !inner.Empty() {
				canvas.Border(inner, render.RoundedBorder, render.Style{Foreground: violet, Background: panel})
				canvas.Text(inner.X+1, inner.Y+1, "inset(3,2)", render.Style{Foreground: ink, Background: panel})
			}
		}
	}
}

func (app *tour) drawLayout(canvas *render.Canvas, area render.Rect) {
	class := layout.Classify(area)
	canvas.Text(area.X, area.Y, fmt.Sprintf("size class: %s (%dx%d) - resize the window, the tree reflows", className(class), area.W, area.H), render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})
	tree := render.Rect{X: area.X, Y: area.Y + 2, W: area.W, H: max(area.H-2, 1)}
	rows := layout.Flex(make([]render.Rect, 2), tree, layout.Vertical, 1,
		layout.Constraint{Basis: tree.H * 2 / 3, Min: 5, Grow: 3, Shrink: 1},
		layout.Constraint{Basis: tree.H / 3, Min: 3, Grow: 1},
	)
	sidebar, main := rows[0].SplitLeft(min(24, max(rows[0].W/3, 10)))
	labelBox := func(rect render.Rect, label, detail string, color render.Color) {
		if rect.Empty() {
			return
		}
		canvas.Fill(rect, " ", render.Style{Background: panel})
		canvas.Border(rect, render.RoundedBorder, render.Style{Foreground: color, Background: panel})
		canvas.Text(rect.X+2, rect.Y+1, label, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
		if rect.H > 2 {
			canvas.Text(rect.X+2, rect.Y+2, fmt.Sprintf("%s %dx%d", detail, rect.W, rect.H), render.Style{Foreground: muted, Background: panel})
		}
	}
	labelBox(sidebar, "fixed-ish left", "Basis 24, Shrink 1", violet)
	mainRows := layout.Flex(make([]render.Rect, 2), main, layout.Vertical, 1,
		layout.Constraint{Basis: main.H * 3 / 4, Min: 4, Grow: 3, Shrink: 2},
		layout.Constraint{Basis: main.H / 4, Min: 2, Grow: 1},
	)
	labelBox(mainRows[0], "grow 3 / shrink 2", "top of the main column", cyan)
	labelBox(mainRows[1], "grow 1", "bottom of the main column", green)
	labelBox(rows[1], "footer strip", "Basis H/3, Grow 1", amber)
}

type easing struct {
	name string
	fn   motion.Ease
}

var easings = []easing{
	{"Linear", motion.Linear},
	{"OutCubic", motion.OutCubic},
	{"InOutCubic", motion.InOutCubic},
	{"OutBack overshoot", motion.OutBack},
}

var responses = []time.Duration{250 * time.Millisecond, 600 * time.Millisecond, 120 * time.Millisecond}

func (app *tour) drawMotion(canvas *render.Canvas, area render.Rect, now time.Time) {
	value, _ := app.tween.Value(now)
	position, velocity := app.spring.Value(now)
	back, _ := app.back.Value(now)
	canvas.Text(area.X, area.Y, "absolute-time motion: every value is a pure sample of frame.Now, so a lagging frame skips stale states instead of replaying them", render.Style{Foreground: muted, Background: background})
	track := func(y int, label, value string, fraction float64, color render.Color) {
		base := render.Style{Foreground: muted, Background: background}
		canvas.Text(area.X, y, label, render.Style{Foreground: color, Background: background, Attrs: render.AttrBold})
		canvas.Text(max(area.X+area.W-canvas.TextWidth(value), area.X), y, value, base)
		width := max(area.W-4, 2)
		for x := 0; x < width; x++ {
			canvas.Put(area.X+2+x, y+1, "·", base)
		}
		at := area.X + 2 + min(max(int(fraction*float64(width-1)), 0), width-1)
		canvas.Put(at, y+1, "█", render.Style{Foreground: color, Background: background})
	}
	y := area.Y + 2
	track(y, fmt.Sprintf("TWEEN %s 900ms", easings[app.ease].name), fmt.Sprintf("%.3f", value), clamp01(value), violet)
	track(y+4, fmt.Sprintf("SPRING response %s", app.spring.Response), fmt.Sprintf("pos %.3f  vel %.2f/s", position, velocity), clamp01(position), cyan)
	track(y+8, "OUTBACK ping-pong loop", fmt.Sprintf("%.3f (marker clamped)", back), clamp01(back), amber)
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

var speeds = []float32{0.4, 0.8, 1.6}

func (app *tour) drawScene(canvas *render.Canvas, area render.Rect, frame *lux.Frame) {
	if area.Empty() {
		return
	}
	canvas.Fill(area, " ", render.Style{Background: panel})
	canvas.Border(area, render.RoundedBorder, render.Style{Foreground: violet, Background: panel})
	inner := area.Inset(1, 1)
	if inner.Empty() {
		return
	}
	object := scene3d.Object{Mesh: app.mesh, Transform: scene3d.RotateY(float64(app.angle)).Mul(scene3d.RotateX(float64(app.angle) * .45))}
	scene := scene3d.Scene{
		Objects: []scene3d.Object{object},
		Camera:  scene3d.Camera{Position: scene3d.V3(0, 0, 4.1), Target: scene3d.V3(0, 0, 0), Up: scene3d.V3(0, 1, 0), FOVY: math.Pi / 3, Near: .1, Far: 100},
		Light:   scene3d.Light{Direction: scene3d.V3(-.5, -.7, -1), Ambient: .16, Diffuse: .84},
	}
	switch frame.Capabilities.Color {
	case capability.Monochrome:
		app.renderer.ColorMode = scene3d.Monochrome
	case capability.ANSI16:
		app.renderer.ColorMode = scene3d.ANSI16
	default:
		app.renderer.ColorMode = scene3d.TrueColor
	}
	app.renderer.Render(canvas.Subview(inner), scene)
	status := fmt.Sprintf(" CPU 3D · %s · %s ", colorName(frame.Capabilities.Color), pauseName(app.paused))
	if frame.ReducedMotion {
		status = fmt.Sprintf(" CPU 3D · %s · REDUCED MOTION ", colorName(frame.Capabilities.Color))
	}
	canvas.Text(area.X+2, area.Y, status, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
}

var terminalList = []string{"Windows Terminal", "iTerm2", "kitty", "foot", "Alacritty", "wezterm", "gnome-terminal", "plain xterm"}

var featureColumns = []ui.TableColumn{{Title: "CAPABILITY", Weight: 3}, {Title: "STATE", Width: 10}}

var featureRows = [][2]string{
	{"grapheme measurement", "cluster"},
	{"word wrap authority", "same"},
	{"half-block renderer", "cell"},
	{"allocation steady state", "0-1/frame"},
}

var spinnerNames = []string{"braille", "arc", "ascii"}
var spinners = [][]string{ui.SpinnerBraille, ui.SpinnerArc, ui.SpinnerASCII}

const scrollContent = 120

func (app *tour) drawWidgets(canvas *render.Canvas, area render.Rect, now time.Time) {
	columns := layout.Flex(make([]render.Rect, 2), area, layout.Horizontal, 2,
		layout.Constraint{Basis: area.W / 2, Min: 26, Grow: 1, Shrink: 1},
		layout.Constraint{Basis: area.W / 2, Min: 26, Grow: 1, Shrink: 1},
	)
	left := pagePanel(canvas, columns[0], "FORM CONTROLS")
	if !left.Empty() {
		y := left.Y
		canvas.Text(left.X, y, "editor (undo/redo/paste)", render.Style{Foreground: muted, Background: panel})
		app.ui.Input("name", canvas, render.Rect{X: left.X, Y: y + 1, W: left.W, H: 1}, &app.name, inputStyle())
		app.ui.Button("gauge", canvas, render.Rect{X: left.X, Y: y + 3, W: min(left.W, 20), H: 3}, "Retarget gauge", buttonStyle())
		y = y + 8
		if y < left.Bottom() {
			canvas.Text(left.X, y, "terminal list", render.Style{Foreground: muted, Background: panel})
			listH := min(len(terminalList), max(left.Bottom()-y-1, 0))
			app.ui.List("list", canvas, render.Rect{X: left.X, Y: y + 1, W: left.W, H: listH}, &app.list, terminalList, listStyle())
			y = y + 1 + listH + 1
		}
		if y < left.Bottom() {
			app.ui.Checkbox("slow", canvas, render.Rect{X: left.X, Y: y, W: left.W, H: 1}, &app.slow, "slow spinner", checkboxStyle())
			app.ui.RadioGroup("glyph", canvas, render.Rect{X: left.X, Y: y + 1, W: left.W, H: min(len(spinnerNames), max(left.Bottom()-y-1, 0))}, &app.glyph, spinnerNames, radioStyle())
		}
	}
	right := pagePanel(canvas, columns[1], "DISPLAY + DATA")
	if right.Empty() {
		return
	}
	y := right.Y
	canvas.Text(right.X, y, "animated gauge (button sets the target)", render.Style{Foreground: muted, Background: panel})
	app.ui.Gauge(canvas, render.Rect{X: right.X, Y: y + 1, W: right.W, H: 1}, &ui.GaugeState{Value: app.gauge}, gaugeStyle())
	canvas.Text(right.X, y+3, "gauge value trace", render.Style{Foreground: muted, Background: panel})
	trace := ui.SparklineState{Values: app.trace}
	app.ui.Sparkline(canvas, render.Rect{X: right.X, Y: y + 4, W: right.W, H: 2}, &trace, ui.SparklineStyle{Fill: render.Style{Foreground: cyan, Background: panel}})
	canvas.Text(right.X, y+7, "frame present-time sparkline", render.Style{Foreground: muted, Background: panel})
	frameSpark := ui.SparklineState{Values: app.frameTimes}
	app.ui.Sparkline(canvas, render.Rect{X: right.X, Y: y + 8, W: right.W, H: 2}, &frameSpark, ui.SparklineStyle{Fill: render.Style{Foreground: violet, Background: panel}})
	app.ui.Spinner(canvas, render.Rect{X: right.X, Y: y + 11, W: 1, H: 1}, &app.sync, now, ui.SpinnerStyle{Frames: spinners[app.glyph.Selected], FrameDuration: app.spinnerDuration(), Style: render.Style{Foreground: cyan, Background: panel}})
	canvas.Text(right.X+3, y+11, fmt.Sprintf("%s spinner, checkbox slows it", spinnerNames[app.glyph.Selected]), render.Style{Foreground: muted, Background: panel})
	canvas.Text(right.X, y+13, "feature table", render.Style{Foreground: muted, Background: panel})
	app.ui.Table("table", canvas, render.Rect{X: right.X, Y: y + 14, W: right.W, H: min(6, max(right.Bottom()-y-14, 0))}, &app.table, featureColumns, len(featureRows),
		func(index int) []string { return featureRows[index][:] },
		ui.TableStyle{
			Normal:   render.Style{Foreground: ink, Background: panel},
			Header:   render.Style{Foreground: muted, Background: panel, Attrs: render.AttrUnderline},
			Selected: render.Style{Foreground: background, Background: violet},
			Focused:  render.Style{Foreground: background, Background: cyan},
		})
	scrollTop := y + 21
	if right.Bottom()-scrollTop > 0 {
		canvas.Text(right.X, scrollTop-1, "virtual log (120 rows)", render.Style{Foreground: muted, Background: panel})
		view := app.ui.ScrollViewport("scroll", canvas, render.Rect{X: right.X, Y: scrollTop, W: right.W, H: right.Bottom() - scrollTop}, &app.scroll, scrollContent, ui.ScrollStyle{})
		visible := app.scroll.Range(scrollContent, right.Bottom()-scrollTop)
		for index := visible.First; index < visible.Last; index++ {
			view.Text(0, index-visible.First, fmt.Sprintf("%06d scrollable row (arrows or wheel)", index+1), render.Style{Foreground: muted, Background: panel})
		}
	}
}

func (app *tour) drawInput(canvas *render.Canvas, area render.Rect, caps capability.Set) {
	canvas.Text(area.X, area.Y, "every key arrives decoded: code, rune, intent modifiers, press / auto-repeat / release; drags and wheel land too", render.Style{Foreground: muted, Background: background})
	enhanced := "legacy input: presses only"
	if caps.KeyboardEnhanced || caps.Win32Input {
		enhanced = "enhanced protocol: releases and repeats decoded"
	}
	canvas.Text(area.X, area.Y+1, fmt.Sprintf("%s   presses %d · releases %d", enhanced, app.presses, app.releases), render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})
	for index, row := range app.feed {
		y := area.Y + 3 + index
		if y >= area.Bottom() {
			break
		}
		style := render.Style{Foreground: ink, Background: background}
		switch row.kind {
		case "release":
			style.Foreground = muted
		case "repeat":
			style.Foreground = amber
		case "mouse press", "wheel":
			style.Foreground = green
		case "press":
		default:
			style.Foreground = violet
		}
		canvas.Text(area.X, y, fmt.Sprintf("%-13s %-44s %s", row.kind, row.desc, row.extra), style)
	}
}

func (app *tour) drawAnnex(canvas *render.Canvas, area render.Rect, frame *lux.Frame) {
	caps := frame.Capabilities
	columns := layout.Flex(make([]render.Rect, 2), area, layout.Horizontal, 2,
		layout.Constraint{Basis: area.W / 2, Min: 30, Grow: 1, Shrink: 1},
		layout.Constraint{Basis: area.W / 2, Min: 30, Grow: 1, Shrink: 1},
	)
	left := pagePanel(canvas, columns[0], "PROBED CAPABILITIES")
	if !left.Empty() {
		rows := [][2]string{
			{"terminal", caps.Terminal},
			{"interactive", boolName(caps.Interactive)},
			{"unicode", boolName(caps.Unicode)},
			{"color", colorName(caps.Color)},
			{"alt screen", boolName(caps.AlternateScreen)},
			{"cursor addressing", boolName(caps.CursorAddressing)},
			{"mouse", boolName(caps.Mouse)},
			{"pixel mouse", boolName(caps.PixelMouse)},
			{"focus reporting", boolName(caps.Focus)},
			{"bracketed paste", boolName(caps.BracketedPaste)},
			{"kitty keyboard", boolName(caps.KeyboardEnhanced)},
			{"win32 input", boolName(caps.Win32Input)},
			{"scroll regions", boolName(caps.ScrollRegions)},
			{"sync output", boolName(caps.SynchronizedOutput)},
			{"target fps", fmt.Sprint(frame.Metrics.TargetFPS)},
			{"present time", frame.Metrics.PresentTime.Round(100 * time.Microsecond).String()},
			{"dropped frames", fmt.Sprint(frame.Metrics.DroppedFrames)},
		}
		for index, row := range rows {
			y := left.Y + index
			if y >= left.Bottom() {
				break
			}
			canvas.Text(left.X, y, row[0], render.Style{Foreground: muted, Background: panel})
			value := row[1]
			if value == "" {
				value = "(unreported)"
			}
			canvas.Text(left.X+19, y, value, render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold})
		}
	}
	right := pagePanel(canvas, columns[1], "SIDEBANDS (OSC)")
	if right.Empty() {
		return
	}
	y := right.Y
	canvas.Text(right.X, y, "channels that ride the frame, never a second write", render.Style{Foreground: muted, Background: panel})
	buttons := []struct {
		id    ui.ID
		label string
	}{
		{"annex-run", "Progress run"},
		{"annex-indet", "Indeterminate"},
		{"annex-error", "Error 66%"},
		{"annex-stop", "Stop"},
		{"annex-notify", "Notify"},
		{"annex-copy", "Copy"},
	}
	buttonW := min(max(right.W/2-2, 10), 18)
	for index, button := range buttons {
		bx := right.X + (index%2)*(buttonW+1)
		by := y + 2 + (index/2)*4
		if by+3 >= right.Bottom() {
			break
		}
		app.ui.Button(button.id, canvas, render.Rect{X: bx, Y: by, W: buttonW, H: 3}, button.label, buttonStyle())
	}
	// Terminal sidebands (SetProgress/Notify/CopyToClipboard) are driven by
	// View itself, not this page: see applySidebands.
	statusY := right.Bottom() - 2
	if statusY > y+2 {
		canvas.Text(right.X, statusY, app.annexNote, render.Style{Foreground: amber, Background: panel})
		if app.progressMode == 1 {
			canvas.Text(right.X, statusY+1, fmt.Sprintf("bar %3d%%", int(app.progress*100)), render.Style{Foreground: cyan, Background: panel})
		}
	}
}

func (app *tour) drawHelp(canvas *render.Canvas, area render.Rect) {
	width, height := min(max(area.W-8, 24), 58), min(max(area.H-6, 10), 18)
	rect := render.Rect{X: max((area.W-width)/2, 0), Y: max((area.H-height)/2, 0), W: width, H: height}
	inner := app.ui.Modal("help-modal", canvas, rect, "Tour keys", ui.ModalStyle{
		Panel:  render.Style{Background: panel},
		Border: render.Style{Foreground: violet, Background: panel},
		Title:  render.Style{Foreground: cyan, Background: panel, Attrs: render.AttrBold},
	})
	if inner != nil {
		bounds := inner.Bounds()
		lines := []string{
			"1..9 or click        switch tabs",
			"Tab / Shift+Tab      move widget focus",
			"Enter / Space        activate focused control",
			"Arrows / wheel       select, scroll",
			"[e] / [v]            motion: easing / response",
			"[s]                  3D: rotation speed",
			"Esc                  close this dialog",
			"q or Ctrl+Q          quit application",
		}
		for index, line := range lines {
			if index >= bounds.H-4 {
				break
			}
			inner.Text(1, 1+index, line, render.Style{Foreground: ink, Background: panel})
		}
		app.ui.Button("close-help", inner, render.Rect{X: max(bounds.W-11, 1), Y: max(bounds.H-3, 1), W: min(10, max(bounds.W-2, 1)), H: min(3, max(bounds.H-1, 1))}, "Close", buttonStyle())
	}
	app.ui.EndModal()
}

func focusName(focused bool) string {
	if focused {
		return "gained"
	}
	return "lost"
}

func actionName(action event.KeyAction) string {
	switch action {
	case event.ActionPress:
		return "press"
	case event.ActionRepeat:
		return "repeat"
	default:
		return "release"
	}
}

func mouseActionName(action event.MouseAction) string {
	switch action {
	case event.MouseMove:
		return "move"
	case event.MousePress:
		return "mouse press"
	case event.MouseRelease:
		return "mouse release"
	default:
		return "wheel"
	}
}

func mouseButtonName(button event.MouseButton) string {
	switch button {
	case event.MouseNone:
		return "none"
	case event.MouseLeft:
		return "left"
	case event.MouseMiddle:
		return "middle"
	case event.MouseRight:
		return "right"
	case event.MouseBack:
		return "back"
	case event.MouseForward:
		return "forward"
	case event.MouseWheelUp:
		return "up"
	case event.MouseWheelDown:
		return "down"
	case event.MouseWheelLeft:
		return "left"
	default:
		return "right"
	}
}

func modString(mod event.Mod) string {
	if mod == 0 {
		return ""
	}
	names := []struct {
		bit  event.Mod
		name string
	}{
		{event.ModShift, "shift"},
		{event.ModAlt, "alt"},
		{event.ModCtrl, "ctrl"},
		{event.ModSuper, "super"},
		{event.ModHyper, "hyper"},
		{event.ModMeta, "meta"},
		{event.ModCapsLock, "caps"},
		{event.ModNumLock, "num"},
	}
	parts := make([]string, 0, 4)
	for _, name := range names {
		if mod&name.bit != 0 {
			parts = append(parts, name.name)
		}
	}
	return strings.Join(parts, "+")
}

func colorName(depth capability.ColorDepth) string {
	return [...]string{"mono", "ANSI 16", "ANSI 256", "truecolor"}[min(int(depth), 3)]
}

func className(class layout.Class) string {
	return [...]string{"tiny", "compact", "medium", "wide"}[min(int(class), 3)]
}

func boolName(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func pauseName(paused bool) string {
	if paused {
		return "paused"
	}
	return "running"
}

func tabsStyle() ui.TabsStyle {
	return ui.TabsStyle{
		Normal:   render.Style{Foreground: muted, Background: background},
		Selected: render.Style{Foreground: background, Background: violet, Attrs: render.AttrBold},
		Focused:  render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
	}
}

func buttonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		Normal:  render.Style{Foreground: ink, Background: panel},
		Focused: render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Hovered: render.Style{Foreground: cyan, Background: panel},
		Pressed: render.Style{Foreground: background, Background: violet, Attrs: render.AttrBold},
	}
}

func inputStyle() ui.InputStyle {
	return ui.InputStyle{
		Normal:      render.Style{Foreground: ink, Background: background},
		Placeholder: render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim},
		Selection:   render.Style{Foreground: background, Background: cyan},
		Cursor:      render.Style{Foreground: background, Background: violet},
	}
}

func listStyle() ui.ListStyle {
	return ui.ListStyle{
		Normal:   render.Style{Foreground: muted, Background: panel},
		Selected: render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold},
		Focused:  render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Hovered:  render.Style{Foreground: cyan, Background: panel},
		Marker:   "> ",
	}
}

func checkboxStyle() ui.CheckboxStyle {
	return ui.CheckboxStyle{
		Normal:    render.Style{Foreground: ink, Background: panel},
		Focused:   render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Checked:   "[x]",
		Unchecked: "[ ]",
	}
}

func radioStyle() ui.RadioStyle {
	return ui.RadioStyle{
		Normal:   render.Style{Foreground: muted, Background: panel},
		Selected: render.Style{Foreground: ink, Background: panel, Attrs: render.AttrBold},
		Focused:  render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold},
		Marker:   "(*)",
		Empty:    "( )",
	}
}

func gaugeStyle() ui.GaugeStyle {
	return ui.GaugeStyle{
		Fill:  render.Style{Foreground: violet, Background: panel},
		Track: render.Style{Foreground: muted, Background: panel},
	}
}

func main() {
	if err := lux.Run(context.Background(), newTour(),
		lux.WithTitle("lux-tui — guided tour"),
		lux.WithKeyReleases(true),
		lux.WithProgress(true),
		lux.WithNotifications(true),
		lux.WithClipboard(true),
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
