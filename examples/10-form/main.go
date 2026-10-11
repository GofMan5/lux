// Command form is the stage-4 forms recipe: a connection-profile editor
// assembled from the plain widgets that already exist — labeled inputs,
// inline validation, a first-invalid focus jump on submit, a read-only
// preview field, an in-flight submit through the Command/Message seam
// with a double-submit guard, and an unsaved-changes ask on quit. The
// field state lives here, in the example layer: the ui package has no
// form builder yet, and the plan is to extract one only after a second
// real form converges on the same shape.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The palette names alias the library reference palette so the chrome
// calls stay short and readable; red is reserved for verdicts (field
// errors, the discard ask), never for data.
var (
	palette    = ui.ReferencePalette()
	ink        = palette.Ink
	muted      = palette.Muted
	violet     = palette.Violet
	cyan       = palette.Cyan
	background = palette.Background
	red        = render.RGBColor(248, 113, 113)
)

// fieldIDs doubles as the Tab order: View registers the inputs in this
// order and stepFocusLocked cycles registration order, so one list keeps
// both honest. The endpoint preview and the submit button register after
// the fields and complete the order.
var fieldIDs = [4]string{"name", "host", "port", "notes"}

// profile is one complete draft: the form's value as a single comparable
// snapshot, so dirty is a comparison against the baseline rather than a
// flag that every edit must remember to raise — and un-raise when an
// edit restores the saved value.
type profile struct {
	name  string
	host  string
	port  string
	notes string
}

// formField is the per-field recipe this example assembles: an input
// widget plus its label, hint, and validator. The validator returns an
// error string or ""; the error replaces the hint in the layout until
// the value passes again.
type formField struct {
	id      string
	label   string
	hint    string
	check   func(value string) string // "" means valid
	input   ui.TextInput
	errText string
}

// formSource is the seam the whole submit pipeline hangs from: the form
// validates, the source attempts the connection, and the elapsed time it
// reports is what the success notice shows. Tests inject a scripted
// source; production dials.
type formSource interface {
	Connect(ctx context.Context, p profile) (time.Duration, error)
}

// dialSource is the production source: it proves the endpoint accepts a
// TCP connection inside the timeout. The reported duration is real
// elapsed time, and the dialer honors the context so quitting cancels
// cleanly.
type dialSource struct {
	timeout time.Duration
}

func (d dialSource) Connect(ctx context.Context, p profile) (time.Duration, error) {
	dialer := net.Dialer{Timeout: d.timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(p.host, p.port))
	elapsed := time.Since(start)
	if conn != nil {
		conn.Close()
	}
	return elapsed, err
}

// connectResult is the payload a connect Command reports back through
// event.Message. A failed connect is data — a notice, not an exit — and
// the attempt id lets Update ignore a late result after a superseding
// attempt.
type connectResult struct {
	attempt int
	err     error
	elapsed time.Duration
}

// formState is what the tests observe from View; the channel is drained
// non-blockingly so it can never steer the run loop.
type formState struct {
	values     [4]string
	errors     [4]string
	endpoint   string
	focused    string
	dirty      bool
	connecting bool
	notice     string
	ask        bool
	attempts   int
}

type formApp struct {
	width, height int
	ui            ui.Context
	source        formSource
	observe       chan formState
	fields        [4]formField
	endpoint      ui.TextInput // read-only preview, recomputed on host/port edits
	baseline      profile      // the last proven (connected) snapshot
	dirty         bool         // derived: profile() != baseline
	connecting    bool         // single-flight guard for the in-flight connect
	attempt       int          // identity of the in-flight connect
	attempts      int          // connects issued, for the double-submit test
	notice        string
	ask           bool // the unsaved-changes question is on screen
}

// checkPort validates the port field in three steps so each verdict
// names exactly one problem.
func checkPort(value string) string {
	if value == "" {
		return "port is required"
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return "port must be digits"
	}
	if number < 1 || number > 65535 {
		return "port must be 1-65535"
	}
	return ""
}

func newFormApp(source formSource, observe chan formState) *formApp {
	app := &formApp{source: source, observe: observe}
	// A zero ButtonStyle reads the theme: the Connect button resolves
	// Text/Focus/Danger roles from this palette. Inputs take no theme —
	// their style is explicit below — and the chrome (labels, hints,
	// status, footer) is layout fact, not widget state.
	app.ui.UseTheme(palette.Theme())
	app.fields = [4]formField{
		{
			id:    "name",
			label: "Name",
			hint:  "shown in the profile list",
			check: func(value string) string {
				if value == "" {
					return "name is required"
				}
				return ""
			},
			input: ui.TextInput{Placeholder: "profile name"},
		},
		{
			id:    "host",
			label: "Host",
			hint:  "hostname or ip, no spaces",
			check: func(value string) string {
				if value == "" {
					return "host is required"
				}
				if strings.ContainsAny(value, " \t") {
					return "host cannot contain spaces"
				}
				return ""
			},
			input: ui.TextInput{Placeholder: "host or address"},
		},
		{
			id:    "port",
			label: "Port",
			hint:  "1-65535",
			check: checkPort,
			input: ui.TextInput{Placeholder: "port number"},
		},
		{
			id:    "notes",
			label: "Notes",
			hint:  "free-form, optional",
			check: func(string) string { return "" },
			input: ui.TextInput{Placeholder: "optional notes"},
		},
	}
	return app
}

// profile snapshots the current draft.
func (app *formApp) profile() profile {
	return profile{
		name:  app.fields[0].input.Value,
		host:  app.fields[1].input.Value,
		port:  app.fields[2].input.Value,
		notes: app.fields[3].input.Value,
	}
}

// fieldIndex maps a region id to its field slot, or -1 for other targets.
func fieldIndex(id ui.ID) int {
	for i, want := range fieldIDs {
		if string(id) == want {
			return i
		}
	}
	return -1
}

func (app *formApp) Update(next event.Event) lux.Command {
	// The unsaved-changes ask consumes its answer before the router sees
	// it. In procs the answer is swallowed after Handle because the
	// filter is plain app state; here the fields are routed widgets, so
	// a 'y' that reached ui.Handle would first type into the focused
	// field. Only bare runes are answers — ctrl-anything stays a key.
	if app.ask && next.Kind == event.Key && next.Key.Code == event.KeyRune && next.Key.Mod == 0 {
		switch next.Key.Rune {
		case 'y', 'Y':
			app.ask = false
			return lux.Quit() // discard: leaving over an unsaved draft is a decision
		case 'n', 'N':
			app.ask = false
		}
		return nil
	}
	if action, handled := app.ui.Handle(next); handled {
		switch {
		case action.Target == "quit" && action.Kind == ui.Activate:
			// Escape while the ask is on screen answers it — keep
			// editing — so a reflexive Escape cannot exit over an
			// unsaved draft.
			if app.ask {
				app.ask = false
				return nil
			}
			// The connect is in flight and lands within the source's
			// timeout; Escape stays inert until then (trapped at most
			// 3s in production), after which quit or discard works.
			if app.connecting {
				return nil
			}
			if app.dirty {
				app.ask = true
				return nil
			}
			return lux.Quit()
		case action.Target == "submit" && action.Kind == ui.Activate:
			// Kind must be Activate: a pointer press on the button
			// first emits a Focus action with this same target, and
			// only the release activates. Dispatching on the target
			// alone would submit twice per click.
			return app.submit()
		case action.Kind == ui.Input && fieldIndex(action.Target) >= 0:
			app.applyField(fieldIndex(action.Target), action)
		}
		// Focus actions (Tab, pointer presses on controls) carry no form
		// semantics of their own; the repaint after every event shows
		// the new caret and selection.
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Message:
		result, ok := next.Data.(connectResult)
		if !ok {
			return nil // a foreign payload is not ours to interpret
		}
		if !app.connecting || result.attempt != app.attempt {
			return nil // a late or superseded result: ignored by identity
		}
		app.applyConnect(result)
	}
	return nil
}

// applyField folds one routed input action into its field and recomputes
// the facts that depend on it: the field's own verdict — inline
// validation clears and raises errors while typing, without a submit —
// and the endpoint preview when host or port changed. The dirty flag is
// derived by comparison, so an edit that restores the baseline value
// un-dirties the form on its own.
func (app *formApp) applyField(index int, action ui.Action) {
	if index < 0 || app.connecting {
		// While connecting every field is drawn Disabled; keys that
		// raced the disabled frame's commit are stopped here as well.
		return
	}
	field := &app.fields[index]
	if !field.input.Apply(action) {
		return
	}
	field.errText = field.check(field.input.Value)
	if field.id == "host" || field.id == "port" {
		app.refreshEndpoint()
	}
	app.dirty = app.profile() != app.baseline
}

// refreshEndpoint recomputes the read-only preview from the current host
// and port values. The preview is a widget so the user can tab to it and
// select the address, but its value is derived, never typed.
func (app *formApp) refreshEndpoint() {
	current := app.profile()
	app.endpoint.SetValue(current.host + ":" + current.port)
}

// submit validates the whole draft and connects. A validation failure
// focuses the first invalid field — all fields are enabled at that
// moment (validation never runs while connecting), so Focus lands on a
// committed region. A valid draft goes in flight: single-flight, one
// attempt identity, everything the worker needs captured by value.
func (app *formApp) submit() lux.Command {
	if app.connecting || app.ask {
		return nil // at most one connect at a time; the notice already says so
	}
	firstInvalid := -1
	for i := range app.fields {
		app.fields[i].errText = app.fields[i].check(app.fields[i].input.Value)
		if app.fields[i].errText != "" && firstInvalid < 0 {
			firstInvalid = i
		}
	}
	if firstInvalid >= 0 {
		app.notice = ""
		app.ui.Focus(ui.ID(fieldIDs[firstInvalid]))
		return nil
	}
	draft := app.profile()
	app.attempt++
	app.attempts++
	app.connecting = true
	app.notice = "connecting to " + draft.host + ":" + draft.port + "..."
	source, id := app.source, app.attempt
	return func(ctx context.Context) event.Event {
		elapsed, err := source.Connect(ctx, draft)
		return event.Event{Kind: event.Message, Data: connectResult{attempt: id, err: err, elapsed: elapsed}}
	}
}

// applyConnect folds one connect result into the model. A failure is
// data: the notice carries the reason and the draft stays exactly as it
// was, because a connect that failed proves nothing and saves nothing. A
// success makes the connected values the baseline — the draft that was
// just proven is now clean.
func (app *formApp) applyConnect(result connectResult) {
	app.connecting = false
	if result.err != nil {
		app.notice = "connect failed: " + result.err.Error()
		return
	}
	app.baseline = app.profile()
	app.dirty = false
	app.notice = fmt.Sprintf("connected to %s:%s in %dms", app.baseline.host, app.baseline.port, result.elapsed.Milliseconds())
}

func (app *formApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	// Priority so Ctrl+S submits even while a field has focus: an
	// app-scope priority shortcut outranks the focused Editable's claim
	// on every key, which would otherwise eat Ctrl+S as input.
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 's', Mod: event.ModCtrl}, Target: "submit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	if area.W < 46 || area.H < 24 {
		canvas.Text(0, 0, "LUX form - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX FORM ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	for i := range app.fields {
		app.drawField(canvas, area, i)
	}

	// The endpoint preview: a read-only input at a stable row, refreshed
	// by applyField. Read-only keeps it tab-reachable and selectable
	// while removing it from the type-ahead path.
	canvas.Text(4, 15, "endpoint", render.Style{Foreground: muted, Background: background})
	preview := fieldInputStyle()
	preview.ReadOnly = true
	preview.Normal = render.Style{Foreground: violet, Background: background}
	if app.connecting {
		preview.Disabled = true
	}
	app.ui.Input("endpoint", canvas, render.Rect{X: 14, Y: 15, W: area.W - 18, H: 1}, &app.endpoint, preview)

	label := "Connect"
	var button ui.ButtonStyle
	if app.connecting {
		label = "connecting..."
		button.Disabled = true
	}
	app.ui.Button("submit", canvas, render.Rect{X: 4, Y: 17, W: 18, H: 3}, label, button)

	// While the ask is on screen it replaces the status line — one row,
	// one question, like the procs kill ask — and the fields behind it
	// stay fully drawn for the moment the answer comes back.
	if app.ask {
		canvas.Text(2, area.H-2, " unsaved changes - y discard and quit  n/esc keep editing ", render.Style{Foreground: red, Background: background, Attrs: render.AttrBold})
	} else {
		status := " clean "
		if app.dirty {
			status = " unsaved "
		}
		canvas.Text(2, area.H-2, status+app.notice, render.Style{Foreground: muted, Background: background})
	}
	footer := " tab fields  type edit  ctrl+s connect  esc quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})
	app.report()
}

// drawField paints one label / input / hint-or-error triple. The input is
// a library widget; the label, the hint, and the verdict row are this
// example's recipe, drawn where the library has no opinion. Disabled
// while a connect is in flight makes the whole recipe inert: the widget
// stops accepting input, the row dims, and Update ignores keys that
// raced the disabled frame's commit.
func (app *formApp) drawField(canvas *render.Canvas, area render.Rect, index int) {
	field := &app.fields[index]
	y := 2 + 3*index
	canvas.Text(4, y, field.label, render.Style{Foreground: muted, Background: background})
	style := fieldInputStyle()
	if app.connecting {
		style.Disabled = true
	}
	app.ui.Input(ui.ID(field.id), canvas, render.Rect{X: 4, Y: y + 1, W: area.W - 8, H: 1}, &field.input, style)
	// The error replaces the hint it invalidates; the hint returns when
	// the error clears. Red is reserved for verdicts, never for hints.
	message, messageStyle := field.hint, render.Style{Foreground: muted, Background: background}
	if field.errText != "" {
		message, messageStyle = field.errText, render.Style{Foreground: red, Background: background}
	}
	canvas.Text(4, y+2, message, messageStyle)
}

// fieldInputStyle is the explicit style for the editable fields: inputs
// take no theme roles, so the example names its colors once, here.
func fieldInputStyle() ui.InputStyle {
	return ui.InputStyle{
		Normal:      render.Style{Foreground: ink, Background: background},
		Placeholder: render.Style{Foreground: muted, Background: background, Attrs: render.AttrDim},
		Selection:   render.Style{Foreground: background, Background: violet},
		Cursor:      render.Style{Foreground: background, Background: cyan},
	}
}

// report publishes the observable state from inside View; the channel
// is drained non-blockingly so observation can never steer the run.
func (app *formApp) report() {
	if app.observe == nil {
		return
	}
	state := formState{
		endpoint:   app.endpoint.Value,
		dirty:      app.dirty,
		connecting: app.connecting,
		notice:     app.notice,
		ask:        app.ask,
		attempts:   app.attempts,
	}
	for i := range app.fields {
		state.values[i] = app.fields[i].input.Value
		state.errors[i] = app.fields[i].errText
		if app.ui.Focused(ui.ID(fieldIDs[i])) {
			state.focused = fieldIDs[i]
		}
	}
	switch {
	case app.ui.Focused("endpoint"):
		state.focused = "endpoint"
	case app.ui.Focused("submit"):
		state.focused = "submit"
	}
	select {
	case app.observe <- state:
	default:
	}
}

func main() {
	// A three-second ceiling keeps a dead endpoint from hanging the
	// app: the failure is a notice, and Ctrl+S retries.
	app := newFormApp(dialSource{timeout: 3 * time.Second}, nil)
	if err := lux.Run(context.Background(), app); err != nil {
		fmt.Fprintln(os.Stderr, "form:", err)
		os.Exit(1)
	}
}
