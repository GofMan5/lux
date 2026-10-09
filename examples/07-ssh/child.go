//go:build !windows

package main

import (
	"fmt"
	"os"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

var (
	demoBackground = render.RGBColor(8, 10, 18)
	demoInk        = render.RGBColor(229, 231, 235)
	demoAccent     = render.RGBColor(34, 211, 238)
	demoMuted      = render.RGBColor(107, 114, 128)
)

// demoApp is the TUI served to ssh clients by default. It is deliberately
// small, and everything it shows is evidence: the pty size readout updates
// live on resize (SIGWINCH crossed the ssh tunnel, the pty, and the engine),
// TERM is what the client asked for, and [q] quitting maps to exit status 0
// on the ssh client side.
type demoApp struct {
	width, height int
}

func (app *demoApp) Update(next event.Event) lux.Command {
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Key:
		if next.Key.Code == event.KeyEscape {
			return lux.Quit()
		}
		if next.Key.Code == event.KeyRune && (next.Key.Rune == 'q' || next.Key.Rune == 'Q') {
			return lux.Quit()
		}
	}
	return nil
}

func (app *demoApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: demoBackground})

	title := " LUX OVER SSH "
	canvas.Text(max((area.W-canvas.TextWidth(title))/2, 0), 0, title,
		render.Style{Foreground: demoAccent, Background: demoBackground, Attrs: render.AttrBold})

	if area.W < 40 || area.H < 10 {
		canvas.Text(0, 2, "enlarge the terminal for the full readout",
			render.Style{Foreground: demoMuted, Background: demoBackground})
	} else {
		canvas.Border(render.Rect{X: 2, Y: 2, W: area.W - 4, H: 7}, render.RoundedBorder,
			render.Style{Foreground: demoAccent, Background: demoBackground})
		lines := [3]string{
			fmt.Sprintf(" pty size   %d x %d ", app.width, app.height),
			fmt.Sprintf(" TERM       %s ", os.Getenv("TERM")),
			" transport  ssh -> pty -> LUX engine ",
		}
		for i, line := range lines {
			canvas.Text(4, 3+i, line, render.Style{Foreground: demoInk, Background: demoBackground})
		}
	}

	footer := " [q] quit "
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer,
		render.Style{Foreground: demoMuted, Background: demoBackground})
}
