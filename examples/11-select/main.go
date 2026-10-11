// Command select is the stage-4 dropdown recipe: three selects that open
// below their anchor, one at the bottom edge whose popup flips above, and
// a button that proves an open popup owns the pointer. While a popup is
// open it is registered inside a modal scope, so the framework provides
// the capture: Tab traps in the popup, Escape and an outside press both
// arrive as one Dismiss action (revert, no click-through), and focus
// returns to the select when it closes. Enter or an option click commits;
// the values row echoes the committed choices.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The example owns its colors explicitly; only the closed box and the
// button take theme roles, like the form's Connect button.
var (
	palette    = ui.ReferencePalette()
	ink        = palette.Ink
	muted      = palette.Muted
	violet     = palette.Violet
	cyan       = palette.Cyan
	background = palette.Background
)

// Layout rows on the reference 64x24 terminal. sortRow sits low enough
// that its popup cannot fit below (18+1+8 > 24) and has room above, so it
// is the flip case; the others demonstrate the plain below placement.
const (
	paintWidth  = 64
	paintHeight = 24
	styleRow    = 3
	motionRow   = 6
	sideRow     = 9
	sortRow     = 18
	applyRow    = 20
	echoRow     = 22
)

// setting is one dropdown: its options, its widget state, and the value
// the current popup opened with (the revert target when it cancels).
// anchor is the closed box, also the input the placement decision needs.
type setting struct {
	id      string
	label   string
	options []string
	state   ui.SelectState
	revert  int
	anchor  render.Rect
}

// selectState is what the tests observe from View; the channel is drained
// non-blockingly so observation can never steer the run loop.
type selectState struct {
	open    string   // id of the open popup, "" when all are closed
	values  []string // committed (or live, while open) choices
	focused string
	applied int
	status  string
}

type selectApp struct {
	width, height int
	ui            ui.Context
	observe       chan selectState
	// committed receives a beacon the instant the frame that built the
	// state is committed to the event tree — see View for why the wire
	// alone cannot prove that.
	committed chan selectState
	settings  []setting
	applied   int
	status    string
}

func newSelectApp(observe, committed chan selectState) *selectApp {
	app := &selectApp{observe: observe, committed: committed}
	app.ui.UseTheme(palette.Theme())
	app.settings = []setting{
		{id: "style", label: "Style", options: []string{"sharp", "rounded", "double", "ascii"}, anchor: render.Rect{X: 4, Y: styleRow, W: 22, H: 1}},
		{id: "motion", label: "Motion", options: []string{"instant", "glide", "ease", "spring", "settle", "drift"}, anchor: render.Rect{X: 4, Y: motionRow, W: 22, H: 1}},
		{id: "side", label: "Side", options: []string{"left", "right", "top", "bottom"}, anchor: render.Rect{X: 36, Y: sideRow, W: 22, H: 1}},
		{id: "sort", label: "Sort", options: []string{"newest", "oldest", "name", "size", "votes", "recent", "trending", "hot", "watched", "archived"}, anchor: render.Rect{X: 4, Y: sortRow, W: 22, H: 1}},
	}
	app.status = "ready"
	return app
}

func (app *selectApp) find(id ui.ID) *setting {
	for i := range app.settings {
		if app.settings[i].id == string(id) {
			return &app.settings[i]
		}
	}
	return nil
}

func (app *selectApp) openSetting() *setting {
	for i := range app.settings {
		if app.settings[i].state.Open {
			return &app.settings[i]
		}
	}
	return nil
}

// cancel reverts the named popup to the value it opened with. Escape and
// an outside press arrive as the same Dismiss action, so both paths share
// this one revert — and the press that dismissed does not reach the
// control underneath, so there is no click-through to undo.
func (app *selectApp) cancel(id string) {
	s := app.find(ui.ID(id))
	if s == nil || !s.state.Open {
		return
	}
	s.state.Selected = s.revert
	s.state.Open = false
	app.status = s.id + " cancelled"
}

func (app *selectApp) values() []string {
	values := make([]string, len(app.settings))
	for i := range app.settings {
		values[i] = app.settings[i].options[app.settings[i].state.Selected]
	}
	return values
}

func (app *selectApp) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Kind {
		case ui.Dismiss:
			// The open popup's modal scope routed the press or the Escape
			// here: cancel is the one outcome for both.
			app.cancel(string(action.Target))
			return nil
		case ui.Activate:
			switch action.Target {
			case "quit":
				return lux.Quit()
			case "apply":
				app.applied++
				app.status = "applied"
				return nil
			}
		}
		if s := app.find(action.Target); s != nil {
			// Snapshot the pre-open choice before Apply: a navigation key
			// opens the popup and moves the choice in one step, so the
			// revert target is knowable only beforehand.
			if !s.state.Open {
				s.revert = s.state.Selected
			}
			if _, activated := s.state.Apply(action, len(s.options)); activated {
				app.status = s.id + " = " + s.options[s.state.Selected]
			}
			// A second select cannot open while one is open: the modal
			// scope traps the keyboard and turns any outside press into
			// the open one's Dismiss, so no event reaches another
			// dropdown's anchor.
		}
		return nil
	}
	if next.Kind == event.Resize {
		app.width, app.height = next.Width, next.Height
	}
	return nil
}

func (app *selectApp) View(frame *lux.Frame) {
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
	var state selectState
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
	// Escape quits when nothing is open. While a popup is open the
	// framework turns Escape into the popup's Dismiss before any shortcut
	// can see it, so this binding never races the cancel.
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})

	if area.W < 44 || area.H < paintHeight {
		canvas.Text(0, 0, "LUX select - enlarge terminal", render.Style{Foreground: violet})
		state = app.report()
		return
	}

	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX SELECT ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	// Closed selects register in focus order; the open one re-registers
	// inside its modal scope below, so the popup owns all input.
	for i := range app.settings {
		s := &app.settings[i]
		canvas.Text(s.anchor.X, s.anchor.Y-1, s.label, render.Style{Foreground: muted, Background: background})
		if s.state.Open {
			continue
		}
		app.ui.Select(ui.ID(s.id), canvas, s.anchor, &s.state, s.options, selectStyle())
	}

	// The open popup: the same anchor and state the closed box would use,
	// but the rect (anchor included, possibly flipped above) becomes a
	// modal scope. That is the whole capture story — Tab traps because
	// only this region is focusable in the scope, an outside press or
	// Escape becomes Dismiss because the framework's modal owns them, and
	// focus lands back on the select when the scope ends.
	if s := app.openSetting(); s != nil {
		placement := ui.SelectPlacement(s.anchor, &s.state, len(s.options), canvas.Bounds().H)
		app.ui.BeginModal(ui.ID(s.id), canvas, placement)
		app.ui.Select(ui.ID(s.id), canvas, s.anchor, &s.state, s.options, selectStyle())
		app.ui.EndModal()
	}

	app.ui.Button("apply", canvas, render.Rect{X: 4, Y: applyRow, W: 10, H: 1}, "Apply", ui.ButtonStyle{})
	canvas.Text(16, applyRow, app.status, render.Style{Foreground: muted, Background: background})

	canvas.Text(4, echoRow, " "+strings.Join(app.values(), "   "), render.Style{Foreground: ink, Background: background})

	footer := " tab move  enter open or commit  esc cancel or quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " esc cancel or quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})
	state = app.report()
}

// selectStyle is the explicit style for the dropdowns: the popup fills
// with the background so it covers what it overlaps, and the choice row
// carries the accent color while the anchor keeps the plain text look.
func selectStyle() ui.SelectStyle {
	return ui.SelectStyle{
		Popup:          render.Style{Foreground: ink, Background: background},
		OptionNormal:   render.Style{Foreground: muted, Background: background},
		OptionSelected: render.Style{Foreground: cyan, Background: background},
		OptionFocused:  render.Style{Foreground: cyan, Background: background},
	}
}

// report publishes the observable state from inside View; the channel is
// drained non-blockingly so observation can never steer the run. It also
// returns the state, because View's beacon re-publishes that same value
// on the committed channel once the frame's tree is live.
func (app *selectApp) report() selectState {
	state := selectState{values: app.values(), applied: app.applied, status: app.status}
	for i := range app.settings {
		if app.settings[i].state.Open {
			state.open = app.settings[i].id
		}
		if app.ui.Focused(ui.ID(app.settings[i].id)) {
			state.focused = app.settings[i].id
		}
	}
	if app.ui.Focused("apply") {
		state.focused = "apply"
	}
	if app.observe != nil {
		select {
		case app.observe <- state:
		default:
		}
	}
	return state
}

func main() {
	app := newSelectApp(nil, nil)
	if err := lux.Run(context.Background(), app); err != nil {
		fmt.Fprintln(os.Stderr, "select:", err)
		os.Exit(1)
	}
}
