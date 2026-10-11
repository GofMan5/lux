// This file is the stage-4 command palette recipe: a keyboard-first
// command surface over the widgets that already exist — modal scope,
// filter input, list, footer — with every command's availability derived
// from live model state. It stays in the example layer on purpose;
// extraction into the library waits for a second real consumer.
package main

import (
	"strings"
	"unicode/utf8"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// paletteCommand is one row of the palette: an id the model switches on,
// a label the filter matches, a keyboard hint drawn right-aligned, and a
// reason that disables the row when non-empty — the same
// disabled-means-explained contract the app's other states follow.
type paletteCommand struct {
	id     string
	label  string
	hint   string
	reason string
}

// paletteMaxCommands caps the visible rows: the palette is a keyboard
// accelerator for a handful of commands, not a second table.
const paletteMaxCommands = 4

// paletteCommands derives the palette from live model state, so the
// reasons cannot drift from the app: kill names the same conditions
// beginConfirm guards, refresh shares the load seam's in-flight flag,
// and clear-filter reflects the table's filter, not the palette's own.
func (app *procsApp) paletteCommands() []paletteCommand {
	commands := []paletteCommand{
		{id: "kill", label: "Kill selected process", hint: "enter"},
		{id: "refresh", label: "Refresh process list", hint: "F5"},
		{id: "clear-filter", label: "Clear filter"},
		{id: "quit", label: "Quit", hint: "esc"},
	}
	switch {
	case app.actionInflight:
		commands[0].reason = "a kill is in flight"
	case app.phase == phaseData && len(app.order) > 0:
		commands[0].reason = ""
	default:
		commands[0].reason = "no process selected"
	}
	if app.inflight {
		commands[1].reason = "a load is in flight"
	}
	if app.filter == "" {
		commands[2].reason = "the filter is empty"
	}
	return commands
}

// filterCommands keeps the filter a plain case-insensitive substring
// match on the label. Ranking stays out until a real usage pattern
// proves it earns its complexity.
func filterCommands(commands []paletteCommand, filter string) []paletteCommand {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return commands
	}
	matches := make([]paletteCommand, 0, len(commands))
	for _, command := range commands {
		if strings.Contains(strings.ToLower(command.label), filter) {
			matches = append(matches, command)
		}
	}
	return matches
}

// paletteMatches is the palette's current answer, derived on demand: no
// cached copy of the list can go stale behind the model.
func (app *procsApp) paletteMatches() []paletteCommand {
	if !app.paletteOpen {
		return nil
	}
	return filterCommands(app.paletteCommands(), app.paletteFilter.Value)
}

// openPalette resets the palette whole: a fresh input, a fresh
// selection. Reopening must never inherit the previous session's
// filter, or the palette would answer a question the user stopped
// asking.
func (app *procsApp) openPalette() {
	app.paletteOpen = true
	app.paletteFilter = ui.TextInput{Placeholder: "type to filter commands"}
	app.paletteList = ui.ListState{}
}

// closePalette only closes the surface; the modal scope restores focus
// by itself on the next frame.
func (app *procsApp) closePalette() {
	app.paletteOpen = false
}

// applyPalette routes every palette action in Update, so all palette
// state changes stay on the run loop and out of View.
func (app *procsApp) applyPalette(action ui.Action) lux.Command {
	switch action.Target {
	case "palette-toggle":
		if app.paletteOpen {
			app.closePalette()
		} else {
			app.openPalette()
		}
	case "palette-run":
		if app.paletteOpen {
			return app.runPaletteCommand()
		}
	case "palette-filter":
		app.paletteFilter.Apply(action)
		// A shrinking match list must pull the selection back inside it;
		// leaving the index alone would point the marker at a row that
		// no longer exists.
		if count := len(app.paletteMatches()); app.paletteList.Selected >= count {
			app.paletteList.Selected = max(count-1, 0)
		}
	case "palette-list":
		if !app.paletteOpen {
			break
		}
		count := len(app.paletteMatches())
		rows := min(paletteMaxCommands, max(count, 1))
		app.paletteList.Apply(action, count, rows)
		// Enter with focus on the list, and the mouse release on a row,
		// both arrive as an activation here: run the command.
		if action.Kind == ui.Activate {
			return app.runPaletteCommand()
		}
	case "palette":
		if action.Kind == ui.Dismiss {
			app.closePalette()
		}
	}
	return nil
}

// runPaletteCommand executes the selected command when it can run. A
// disabled command is not an error: the palette stays open with its
// reason on screen, so Enter gives feedback instead of closing over a
// no-op.
func (app *procsApp) runPaletteCommand() lux.Command {
	matches := app.paletteMatches()
	if len(matches) == 0 {
		return nil
	}
	app.paletteList.Selected = min(app.paletteList.Selected, len(matches)-1)
	command := matches[app.paletteList.Selected]
	if command.reason != "" {
		return nil
	}
	app.closePalette()
	switch command.id {
	case "kill":
		// The palette owns no kill pipeline; it opens the same inline
		// confirmation the table's Enter opens, so the ask, the y/n
		// runes, and the notice stay identical.
		app.beginConfirm()
		return nil
	case "refresh":
		return app.issueLoad()
	case "clear-filter":
		app.filter = ""
		return app.issueLoad()
	case "quit":
		return lux.Quit()
	}
	return nil
}

// paletteRow lays one command out: label left, keyboard hint
// right-aligned, all ASCII so the padding arithmetic matches the cell
// grid. A row too narrow for both drops the hint, never the label.
func paletteRow(command paletteCommand, width int) string {
	label := command.label
	if command.hint == "" {
		return label
	}
	pad := width - utf8.RuneCountInString(label) - utf8.RuneCountInString(command.hint) - 2
	if pad < 1 {
		return label
	}
	return label + strings.Repeat(" ", pad) + command.hint
}

// drawPalette paints the palette: a modal scope, a filter input, the
// command list, and a footer that either explains the disabled
// selection or names the keys.
func (app *procsApp) drawPalette(canvas *render.Canvas, area render.Rect) {
	width := min(max(area.W-16, 20), 44)
	height := paletteMaxCommands + 5
	rect := render.Rect{
		X: max((area.W-width)/2, 0),
		Y: max((area.H-height)/2, 0),
		W: width,
		H: height,
	}
	inner := app.ui.Modal("palette", canvas, rect, "command palette", ui.ModalStyle{
		Panel:  render.Style{Background: background},
		Border: render.Style{Foreground: violet, Background: background},
		Title:  render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold},
	})
	if inner == nil {
		app.ui.EndModal()
		return
	}
	bounds := inner.Bounds()
	// Modal-scope keys first: Enter runs, Up/Down move — all scoped to
	// the palette, so they keep working with focus sitting in the
	// filter input.
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEnter}, Target: "palette-run", Kind: ui.Activate, Scope: ui.ScopeModal, Owner: "palette"})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyUp}, Target: "palette-list", Kind: ui.Scroll, Scope: ui.ScopeModal, Owner: "palette"})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyDown}, Target: "palette-list", Kind: ui.Scroll, Scope: ui.ScopeModal, Owner: "palette"})
	// The input registers before the list, so the modal's first-focus
	// pass lands on the filter: typing is the primary interaction.
	app.ui.Input("palette-filter", inner, render.Rect{X: 1, Y: 0, W: bounds.W - 2, H: 1}, &app.paletteFilter, ui.InputStyle{
		Normal:      render.Style{Foreground: ink, Background: background},
		Placeholder: render.Style{Foreground: muted, Background: background},
	})
	matches := app.paletteMatches()
	if len(matches) == 0 {
		inner.Text(1, 2, "no matching commands", render.Style{Foreground: muted, Background: background})
	} else {
		listWidth := bounds.W - 2
		items := make([]string, len(matches))
		for i, match := range matches {
			items[i] = paletteRow(match, listWidth)
		}
		app.ui.List("palette-list", inner, render.Rect{X: 1, Y: 2, W: bounds.W - 2, H: min(len(matches), paletteMaxCommands)}, &app.paletteList, items, ui.ListStyle{
			Normal:        render.Style{Foreground: ink, Background: background},
			Selected:      render.Style{Foreground: violet, Background: background, Attrs: render.AttrBold},
			Focused:       render.Style{Foreground: background, Background: violet},
			DisabledStyle: render.Style{Foreground: red, Background: background},
			DisabledAt: func(index int) bool {
				return matches[min(index, len(matches)-1)].reason != ""
			},
		})
	}
	// The footer is the palette's outcome line: a disabled command
	// explains itself there in the disabled color; an enabled one gets
	// the key hints.
	footer := " enter run  up/down move  esc close "
	footerStyle := render.Style{Foreground: muted, Background: background}
	if len(matches) > 0 {
		if reason := matches[min(app.paletteList.Selected, len(matches)-1)].reason; reason != "" {
			footer = " " + reason
			footerStyle = render.Style{Foreground: red, Background: background}
		}
	}
	// ASCII footer: rune-cut to fit, losing the tail of a reason, never
	// its head.
	if w := inner.TextWidth(footer); w > bounds.W-2 {
		trimmed := []rune(footer)
		footer = string(trimmed[:len(trimmed)-(w-(bounds.W-2))])
	}
	inner.Text(1, bounds.H-1, footer, footerStyle)
	app.ui.EndModal()
}
