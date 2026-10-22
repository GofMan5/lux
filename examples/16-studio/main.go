// Command studio is the hands-on tour of everything lux does, built as one
// coherent app instead of a widget catalog: a live dashboard with springs,
// sparklines and an async deploy pipeline, a sortable and filterable
// services table with OSC 8 links and a confirm dialog, a file tree with a
// real multiline editor, a motion lab with a CPU-rendered 3D scene, and a
// settings page that swaps the whole palette and shows every raw input event.
// A Ctrl+P command palette reaches all of it from the keyboard.
//
// The app only asks for frames while something on screen moves: park it on
// Settings or Files and the frame counter in the header stops.
package main

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
	"github.com/GofMan5/lux/ui"
)

const (
	spinnerGlyph = 80 * time.Millisecond
	toastFor     = 3 * time.Second
	jobTickEvery = 70 * time.Millisecond
	restartTakes = 1600 * time.Millisecond
	seriesCap    = 64
	eventCap     = 200
)

var sampleEvery = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}

// metric is one dashboard card: a sampled series plus a spring that carries
// the headline number to each new sample instead of snapping.
type metric struct {
	name, unit string
	max        float64
	values     []float64
	spring     motion.Spring
}

type job struct {
	id       int
	running  bool
	step     int
	failed   bool
	spinner  ui.SpinnerState
	finished time.Time
	stop     chan struct{} // closed on cancel, so the pending tick exits at once
}

// Messages produced by async commands and folded back in Update.
type jobTick struct{ id int }
type restartDone struct{ name string }

type studio struct {
	ui     ui.Context
	theme  int
	width  int
	height int
	frames int
	tabs   ui.TabsState
	slots  [3]render.Rect // layout.Flex scratch; each page reads its rects before the next Flex

	// Dashboard.
	metrics    []metric
	sampled    time.Time
	sampleStep int
	rng        *rand.Rand
	deploy     job
	activity   []string
	activityAt ui.ListState

	// Services.
	services   []service
	view       []int
	filter     ui.TextInput
	table      ui.TableState
	restarting map[string]time.Time
	confirm    string

	// Files.
	tree   ui.TreeState
	editor ui.TextArea
	open   string
	files  map[string]string
	saved  map[string]string

	// Motion.
	spring   motion.Spring
	tween    motion.Tween
	ease     ui.SelectState
	shape    ui.SelectState
	angle    float64
	labEpoch time.Time // the lab clock: wall time minus every paused moment
	labTime  time.Duration
	paused   bool
	renderer scene3d.Renderer
	meshes   []scene3d.Mesh

	// Settings.
	themeRadio ui.RadioState
	reduce     ui.CheckboxState
	showStats  ui.CheckboxState
	rate       ui.SelectState
	events     []string
	eventsView ui.ScrollState

	// Overlays.
	help     bool
	palette  bool
	query    ui.TextInput
	commands ui.ListState
	toast    string
	toastEnd time.Time

	// Sidebands queued by Update and flushed by the next View, the only
	// place a Frame exists.
	copyText     string
	notify       [2]string
	progressSent int
	reduced      bool
}

func newStudio() *studio {
	now := time.Now()
	app := &studio{
		rng: rand.New(rand.NewPCG(7, 11)),
		metrics: []metric{
			{name: "CPU", unit: "%", max: 100},
			{name: "MEMORY", unit: "%", max: 100},
			{name: "REQUESTS", unit: "/s", max: 1600},
		},
		services:     append([]service(nil), seedServices...),
		restarting:   map[string]time.Time{},
		files:        map[string]string{},
		saved:        map[string]string{},
		ease:         ui.SelectState{PopupRows: len(easings)},
		shape:        ui.SelectState{PopupRows: len(shapes)},
		rate:         ui.SelectState{Selected: 1, PopupRows: len(sampleRates)},
		showStats:    ui.CheckboxState{Checked: true},
		meshes:       []scene3d.Mesh{scene3d.Cube(1.5), gem()},
		angle:        .6, // start turned so the first frame already reads as 3D
		tree:         ui.TreeState{Expanded: map[string]bool{"cmd": true, "internal": true}},
		progressSent: -1,
		labEpoch:     now,
	}
	app.filter.Placeholder = "type to filter, / to focus"
	app.query.Placeholder = "type a command"
	for name, text := range seedFiles {
		app.files[name], app.saved[name] = text, text
	}
	app.openFile("README.md")
	// Seed the series so the first frame already paints full sparklines.
	for range seriesCap {
		app.sample(now)
	}
	app.spring = motion.Spring{From: .2, To: .8, Start: now, Response: 450 * time.Millisecond}
	app.tween = motion.Tween{From: 0, To: 1, Start: now, Duration: 1200 * time.Millisecond, Ease: motion.OutCubic}
	app.refilter()
	app.log("studio started")
	app.applyTheme()
	return app
}

func (app *studio) applyTheme() { app.ui.UseTheme(themes[app.theme].palette.Theme()) }

func (app *studio) Update(next event.Event) lux.Command {
	app.record(next)
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Message:
		return app.message(next)
	}
	if action, handled := app.ui.Handle(next); handled {
		return app.route(action)
	}
	if next.Kind == event.Key && next.Key.Action != event.ActionRelease {
		return app.key(next.Key)
	}
	return nil
}

func (app *studio) message(next event.Event) lux.Command {
	switch msg := next.Data.(type) {
	case jobTick:
		if msg.id != app.deploy.id || !app.deploy.running {
			return nil // a tick from a cancelled run
		}
		app.deploy.step++
		if app.deploy.step%stageSteps == 0 {
			app.log("deploy: " + stages[app.deploy.step/stageSteps-1] + " ok")
		}
		if app.deploy.step >= len(stages)*stageSteps {
			app.deploy.running, app.deploy.finished = false, time.Now()
			app.say("Deploy finished")
			app.notify = [2]string{"LUX Studio", "Deploy finished: all 5 stages green"}
			return nil
		}
		return tick(msg.id, app.deploy.stop)
	case restartDone:
		delete(app.restarting, msg.name)
		for i := range app.services {
			if app.services[i].name == msg.name {
				app.services[i].status, app.services[i].latency = "healthy", 20+app.rng.IntN(60)
			}
		}
		app.refilter()
		app.say(msg.name + " is healthy again")
		app.log("restart: " + msg.name + " healthy")
		app.notify = [2]string{"LUX Studio", msg.name + " restarted"}
	}
	return nil
}

// tick is the deploy job's async step: a command runs off the UI goroutine,
// honors cancellation, and reports back as one Message event.
func tick(id int, stop <-chan struct{}) lux.Command {
	return func(ctx context.Context) event.Event {
		timer := time.NewTimer(jobTickEvery)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return event.Event{}
		case <-stop:
			return event.Event{}
		case <-timer.C:
		}
		return event.Event{Kind: event.Message, Data: jobTick{id: id}}
	}
}

func restart(name string) lux.Command {
	return func(ctx context.Context) event.Event {
		timer := time.NewTimer(restartTakes)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return event.Event{}
		case <-timer.C:
		}
		return event.Event{Kind: event.Message, Data: restartDone{name: name}}
	}
}

func (app *studio) route(action ui.Action) lux.Command {
	activate := action.Kind == ui.Activate
	switch action.Target {
	case "quit":
		return lux.Quit()
	case "tabs":
		app.tabs.Apply(action, pages)
	case "open-help":
		if activate {
			app.help, app.palette = !app.help, false
		}
	case "help", "help-close":
		if action.Kind == ui.Dismiss || activate {
			app.help = false
		}
	case "open-palette":
		if activate {
			app.togglePalette()
		}
	case "palette":
		if action.Kind == ui.Dismiss {
			app.palette = false
		}
	case "palette-query":
		if app.query.Apply(action) {
			app.commands = ui.ListState{}
		}
		return app.paletteKey(action.Event)
	case "palette-list":
		count := len(app.matches())
		if _, activated := app.commands.Apply(action, count, max(action.Bounds.H, 1)); activated {
			return app.runSelected()
		}

	// Dashboard.
	case "deploy":
		if activate {
			return app.toggleDeploy()
		}
	case "activity":
		app.activityAt.Apply(action, len(app.activity), max(action.Bounds.H, 1))

	// Services.
	case "filter":
		if action.Kind == ui.Focus {
			app.ui.Focus("filter")
		} else if app.filter.Apply(action) {
			app.refilter()
		}
		if code := keyCode(action); code == event.KeyEnter || code == event.KeyEscape {
			app.ui.Focus("services")
		}
	case "services":
		if _, _, sort := app.table.Apply(action, serviceColumns, len(app.view)); sort {
			app.table.SortRequested = false
			app.refilter()
		}
	case "copy-url":
		if s, ok := app.current(); ok && activate {
			app.copyText = s.url
			app.say("Copied " + s.url)
		}
	case "restart":
		if s, ok := app.current(); ok && activate && app.restarting[s.name].IsZero() {
			app.confirm = s.name
		}
	case "confirm", "confirm-no":
		if action.Kind == ui.Dismiss || activate {
			app.confirm = ""
		}
	case "confirm-yes":
		if activate && app.confirm != "" {
			name := app.confirm
			app.confirm = ""
			app.restarting[name] = time.Now()
			app.setStatus(name, "restarting")
			app.log("restart: " + name)
			return restart(name)
		}

	// Files.
	case "tree":
		rows := app.treeRows()
		app.tree.Apply(action, len(rows), func(i int) ui.TreeRow { return rows[i] })
		if rows = app.treeRows(); app.tree.Selected < len(rows) && !rows[app.tree.Selected].HasChildren {
			app.openFile(rows[app.tree.Selected].ID)
		}
	case "editor":
		if keyCode(action) == event.KeyEscape {
			app.ui.Focus("tree") // a way out that no text edit claims
		} else {
			app.editor.Apply(action)
		}
	case "editor-save":
		if activate {
			app.files[app.open] = app.editor.Text()
			app.saved[app.open] = app.files[app.open]
			app.say("Saved " + app.open)
			app.log("saved " + app.open)
		}
	case "editor-copy":
		if text := app.editor.SelectedText(); activate && text != "" {
			app.copyText = text
			app.say(fmt.Sprintf("Copied %d bytes to the clipboard", len(text)))
		}

	// Motion.
	case "spring-track":
		if activate || action.Kind == ui.Input {
			app.retargetSpring(float64(action.X) / float64(max(action.Bounds.W-1, 1)))
		}
	case "ease":
		if changed, _ := app.ease.Apply(action, len(easings)); changed {
			app.tween.Ease = easeFunc(app.ease.Selected)
		}
	case "shape":
		app.shape.Apply(action, len(shapes))
	case "pause":
		if activate {
			app.paused = !app.paused
		}

	// Settings.
	case "theme":
		if changed, _ := app.themeRadio.Apply(action, len(themes)); changed {
			app.theme = app.themeRadio.Selected
			app.applyTheme()
		}
	case "reduce":
		app.reduce.Apply(action)
	case "stats":
		app.showStats.Apply(action)
	case "rate":
		app.rate.Apply(action, len(sampleRates))
	case "events":
		app.eventsView.Apply(action, len(app.events), max(action.Bounds.H, 1))
	case "notify":
		if activate {
			app.notify = [2]string{"LUX Studio", "Hello from a desktop notification"}
			app.say("Notification sent (OSC 9 / 777)")
		}
	case "copy":
		if activate {
			app.copyText = "lux studio says hi"
			app.say("Copied text to the system clipboard (OSC 52)")
		}
	}
	return nil
}

// keyCode is the pressed key behind a widget action, or zero.
func keyCode(action ui.Action) event.KeyCode {
	if action.Event.Kind != event.Key || action.Event.Key.Action == event.ActionRelease {
		return 0
	}
	return action.Event.Key.Code
}

// key handles keys no widget claimed: page digits, quit, and palette
// navigation while the query field keeps focus.
func (app *studio) key(key event.KeyEvent) lux.Command {
	if app.palette {
		return app.paletteKey(event.Event{Kind: event.Key, Key: key})
	}
	switch {
	case key.Rune == 'q' && key.Mod == 0, key.Is(event.KeyRune, event.ModCtrl) && key.Rune == 'c':
		return lux.Quit()
	case key.Rune >= '1' && key.Rune <= '5' && key.Mod == 0:
		app.tabs.Selected = int(key.Rune - '1')
	case key.Rune == '?':
		app.help = true
	case key.Rune == ' ' && app.tabs.Selected == pageMotion:
		app.paused = !app.paused
	case key.Rune == 'd' && app.tabs.Selected == pageDashboard:
		return app.toggleDeploy()
	case key.Rune == 't':
		app.theme = (app.theme + 1) % len(themes)
		app.themeRadio.Selected = app.theme
		app.applyTheme()
	}
	return nil
}

func (app *studio) toggleDeploy() lux.Command {
	if app.deploy.running {
		app.deploy.running, app.deploy.failed = false, true
		close(app.deploy.stop)
		app.say("Deploy cancelled")
		app.log("deploy: cancelled")
		return nil
	}
	app.deploy = job{id: app.deploy.id + 1, running: true, spinner: ui.SpinnerState{Started: time.Now()}, stop: make(chan struct{})}
	app.log("deploy: started")
	return tick(app.deploy.id, app.deploy.stop)
}

func (app *studio) retargetSpring(target float64) {
	now := app.labNow()
	position, velocity := app.spring.Value(now)
	app.spring = motion.Spring{From: position, To: min(max(target, 0), 1), Velocity: velocity, Start: now, Response: app.spring.Response}
}

func easeFunc(index int) motion.Ease {
	return []motion.Ease{motion.OutCubic, motion.InOutCubic, motion.OutBack, motion.Linear}[index]
}

// sample appends one synthetic reading to every series and springs each
// headline number from wherever it is right now to the new value.
func (app *studio) sample(now time.Time) {
	app.sampleStep++
	step := float64(app.sampleStep)
	noise := func(scale float64) float64 { return (app.rng.Float64() - .5) * scale }
	readings := []float64{
		42 + 26*math.Sin(step*.21) + noise(14),
		58 + 9*math.Sin(step*.05) + noise(3),
		820 + 430*math.Sin(step*.13) + noise(220),
	}
	for i := range app.metrics {
		m := &app.metrics[i]
		value := min(max(readings[i], 0), m.max)
		m.values = append(m.values, value)
		if len(m.values) > seriesCap {
			m.values = append(m.values[:0], m.values[1:]...)
		}
		position, velocity := m.spring.Value(now)
		if m.spring.Start.IsZero() {
			position, velocity = value, 0
		}
		m.spring = motion.Spring{From: position, To: value, Velocity: velocity, Start: now, Response: 380 * time.Millisecond}
	}
	app.sampled = now
}

func (app *studio) refilter() {
	query := strings.ToLower(app.filter.Value)
	app.view = app.view[:0]
	for i, s := range app.services {
		if query == "" || strings.Contains(strings.ToLower(s.name+" "+s.region+" "+s.status), query) {
			app.view = append(app.view, i)
		}
	}
	if app.table.SortActive {
		column, desc := app.table.SortColumn, app.table.SortDesc
		slices.SortStableFunc(app.view, func(a, b int) int {
			x, y := app.services[a], app.services[b]
			var order int
			switch column {
			case 1:
				order = cmp.Compare(x.region, y.region)
			case 2:
				order = cmp.Compare(x.status, y.status)
			case 3:
				order = cmp.Compare(x.latency, y.latency)
			default:
				order = cmp.Compare(x.name, y.name)
			}
			if desc {
				return -order
			}
			return order
		})
	}
	app.table.Selected = min(app.table.Selected, max(len(app.view)-1, 0))
}

func (app *studio) current() (service, bool) {
	if app.table.Selected < 0 || app.table.Selected >= len(app.view) {
		return service{}, false
	}
	return app.services[app.view[app.table.Selected]], true
}

func (app *studio) setStatus(name, status string) {
	for i := range app.services {
		if app.services[i].name == name {
			app.services[i].status = status
		}
	}
	app.refilter()
}

func (app *studio) treeRows() []ui.TreeRow {
	return flattenTree(projectTree, app.tree.IsExpanded, 0, nil)
}

// openFile parks the editor's buffer back into the file map before loading
// the next file, so unsaved edits survive switching files.
func (app *studio) openFile(name string) {
	if name == app.open {
		return
	}
	if app.open != "" {
		app.files[app.open] = app.editor.Text()
	}
	app.open = name
	app.editor = ui.TextArea{}
	app.editor.SetValue(app.files[name])
	app.editor.Line, app.editor.Offset, app.editor.AnchorLine, app.editor.AnchorOffset = 0, 0, 0, 0 // open at the top
}

func (app *studio) say(text string) {
	app.toast, app.toastEnd = text, time.Now().Add(toastFor)
}

func (app *studio) log(text string) {
	app.activity = append(app.activity, time.Now().Format("15:04:05")+"  "+text)
	if len(app.activity) > eventCap {
		app.activity = append(app.activity[:0], app.activity[1:]...)
	}
	app.activityAt.Selected = len(app.activity) - 1
}

// record keeps the raw event stream for the Settings inspector. Hover
// motion collapses into one row so the log stays readable.
func (app *studio) record(next event.Event) {
	var row string
	switch next.Kind {
	case event.Key:
		if next.Key.Action == event.ActionRelease {
			row = "key   release " + next.Key.String()
		} else {
			row = "key   " + next.Key.String()
		}
	case event.Text:
		row = fmt.Sprintf("text  %q", next.Text)
	case event.Paste:
		row = fmt.Sprintf("paste %d bytes", len(next.Text))
	case event.Mouse:
		m := next.Mouse
		row = fmt.Sprintf("mouse %s %s @%d,%d", mouseAction(m.Action), mouseButton(m.Button), m.X, m.Y)
		if m.Action == event.MouseMove && len(app.events) > 0 && strings.HasPrefix(app.events[len(app.events)-1], "mouse move") {
			app.events[len(app.events)-1] = row
			return
		}
	case event.Resize:
		row = fmt.Sprintf("size  %dx%d", next.Width, next.Height)
	case event.Focus:
		row = map[bool]string{true: "focus gained", false: "focus lost"}[next.Focused]
	default:
		return
	}
	follow := app.eventsView.Offset >= len(app.events)-app.eventRows()
	app.events = append(app.events, row)
	if len(app.events) > eventCap {
		app.events = append(app.events[:0], app.events[1:]...)
	}
	if follow {
		app.eventsView.Offset = max(len(app.events)-app.eventRows(), 0)
	}
}

// eventRows is the inspector's visible height, derived from the same
// layout the Settings page draws.
func (app *studio) eventRows() int { return max(app.height-8, 1) }

func mouseAction(a event.MouseAction) string {
	return [...]string{"move", "press", "release", "wheel"}[min(int(a), 3)]
}

func mouseButton(b event.MouseButton) string {
	names := [...]string{"-", "left", "middle", "right", "back", "forward", "wheel-up", "wheel-down", "wheel-left", "wheel-right"}
	if int(b) < len(names) {
		return names[b]
	}
	return "?"
}

// command is one Ctrl+P palette entry.
type command struct {
	title string
	run   func(app *studio) lux.Command
}

var commands = []command{
	{"Go to Dashboard", func(a *studio) lux.Command { a.tabs.Selected = pageDashboard; return nil }},
	{"Go to Services", func(a *studio) lux.Command { a.tabs.Selected = pageServices; return nil }},
	{"Go to Files", func(a *studio) lux.Command { a.tabs.Selected = pageFiles; return nil }},
	{"Go to Motion", func(a *studio) lux.Command { a.tabs.Selected = pageMotion; return nil }},
	{"Go to Settings", func(a *studio) lux.Command { a.tabs.Selected = pageSettings; return nil }},
	{"Deploy: start or cancel", func(a *studio) lux.Command { return a.toggleDeploy() }},
	{"Theme: next palette", func(a *studio) lux.Command {
		a.theme = (a.theme + 1) % len(themes)
		a.themeRadio.Selected = a.theme
		a.applyTheme()
		return nil
	}},
	{"Motion: toggle reduced motion", func(a *studio) lux.Command { a.reduce.Checked = !a.reduce.Checked; return nil }},
	{"Motion: pause or resume the lab", func(a *studio) lux.Command { a.paused = !a.paused; return nil }},
	{"Terminal: send a notification", func(a *studio) lux.Command {
		a.notify = [2]string{"LUX Studio", "Sent from the command palette"}
		return nil
	}},
	{"Terminal: copy build info", func(a *studio) lux.Command {
		a.copyText = "lux studio, " + time.Now().Format(time.RFC3339)
		a.say("Copied build info")
		return nil
	}},
	{"Help: show keys", func(a *studio) lux.Command { a.help = true; return nil }},
	{"Quit", func(*studio) lux.Command { return lux.Quit() }},
}

// matches filters the palette by subsequence, so "gtf" finds "Go to Files".
func (app *studio) matches() []int {
	query := strings.ToLower(strings.ReplaceAll(app.query.Value, " ", ""))
	var out []int
	for i, c := range commands {
		title, at := strings.ToLower(c.title), 0
		for _, r := range query {
			next := strings.IndexRune(title[at:], unicode.ToLower(r))
			if next < 0 {
				at = -1
				break
			}
			at += next + 1
		}
		if at >= 0 {
			out = append(out, i)
		}
	}
	return out
}

func (app *studio) togglePalette() {
	app.palette, app.help = !app.palette, false
	app.query = ui.TextInput{Placeholder: app.query.Placeholder}
	app.commands = ui.ListState{}
}

// paletteKey moves the palette selection and runs it on Enter while the
// query field keeps the keyboard.
func (app *studio) paletteKey(next event.Event) lux.Command {
	if next.Kind != event.Key {
		return nil
	}
	count := len(app.matches())
	switch next.Key.Code {
	case event.KeyUp:
		app.commands.Selected = max(app.commands.Selected-1, 0)
	case event.KeyDown:
		app.commands.Selected = min(app.commands.Selected+1, max(count-1, 0))
	case event.KeyEnter:
		return app.runSelected()
	}
	return nil
}

func (app *studio) runSelected() lux.Command {
	found := app.matches()
	if app.commands.Selected >= len(found) {
		return nil
	}
	app.palette = false
	return commands[found[app.commands.Selected]].run(app)
}

func main() {
	err := lux.Run(context.Background(), newStudio(),
		lux.WithTitle("LUX Studio"),
		lux.WithClipboard(true),
		lux.WithNotifications(true),
		lux.WithProgress(true),
		lux.WithHyperlinks(true),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
