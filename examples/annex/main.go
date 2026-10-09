// Command annex demonstrates the three opt-in sideband channels: OSC 9;4
// taskbar progress, OSC 9;777 desktop notifications, and OSC 52 clipboard
// writes. Every channel is off until the app asks for it, the requests
// ride the frame that queued them, and none of the bytes appear when the
// terminal does not understand the protocol.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	background = render.RGBColor(8, 10, 18)
)

// states are the OSC 9;4 progress states this demo cycles through, in the
// order the keys select them.
var states = []struct {
	label string
	state lux.ProgressState
}{
	{"none", lux.ProgressNone},
	{"indeterminate", lux.ProgressIndeterminate},
	{"normal", lux.ProgressNormal},
	{"error", lux.ProgressError},
	{"paused", lux.ProgressPaused},
}

type model struct {
	width, height int
	stateIndex    int
	percent       int
	// pendingToast and pendingCopy hold the last requested notification
	// and clipboard text. The port pattern from the widget toolkit: state
	// changes land in Update, and View flushes them once the frame that
	// will carry the sideband bytes exists — a request is answered by the
	// frame that proves it happened.
	pendingToast [2]string
	pendingCopy  string
	lastAction   string
}

func (m *model) Update(next event.Event) lux.Command {
	switch next.Kind {
	case event.Resize:
		m.width, m.height = next.Width, next.Height
	case event.Key:
		switch next.Key.Rune {
		case 'q':
			return lux.Quit()
		case ' ':
			m.stateIndex = (m.stateIndex + 1) % len(states)
			m.lastAction = "progress state: " + states[m.stateIndex].label
		case 'a':
			m.percent = (m.percent + 10) % 110
			m.lastAction = "progress percent: " + strconv.Itoa(m.percent)
		case 'n':
			m.pendingToast = [2]string{"Sideband", "a desktop notification, queued from a frame"}
			m.lastAction = "notification queued"
		case 'c':
			m.pendingCopy = "hello from lux OSC 52"
			m.lastAction = "clipboard queued"
		}
	}
	return nil
}

func (m *model) View(frame *lux.Frame) {
	canvas := frame.Canvas
	canvas.Clear(render.Style{Background: background})
	title := render.Style{Foreground: violet, Background: background, Attrs: render.AttrBold}
	body := render.Style{Foreground: ink, Background: background}
	hint := render.Style{Foreground: muted, Background: background}
	canvas.Text(2, 0, "LUX", title)
	canvas.Text(6, 0, "sideband annex", title)
	canvas.Text(2, 2, "frame "+strconv.Itoa(m.width)+"x"+strconv.Itoa(m.height), body)
	canvas.Text(2, 4, "taskbar progress  "+states[m.stateIndex].label, body)
	canvas.Text(2, 5, "percent           "+strconv.Itoa(m.percent), body)
	canvas.Text(2, 7, "last action       "+m.lastAction, body)
	canvas.Text(2, 9, "[space] progress state   [a] percent +10", hint)
	canvas.Text(2, 10, "[n] toast                [c] clipboard", hint)
	canvas.Text(2, 11, "[q] quit", hint)

	// The state, not an event: every frame re-reports the progress, so the
	// taskbar bar survives suspend/resume and terminal redraws for free.
	frame.SetProgress(states[m.stateIndex].state, m.percent)
	if m.pendingToast[0] != "" || m.pendingToast[1] != "" {
		frame.Notify(m.pendingToast[0], m.pendingToast[1])
		m.pendingToast = [2]string{}
	}
	if m.pendingCopy != "" {
		frame.CopyToClipboard(m.pendingCopy)
		m.pendingCopy = ""
	}
}

func main() {
	if err := lux.Run(context.Background(), &model{width: 80, height: 24},
		lux.WithTitle("lux-tui — sideband annex"),
		lux.WithProgress(true),
		lux.WithNotifications(true),
		lux.WithClipboard(true),
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
