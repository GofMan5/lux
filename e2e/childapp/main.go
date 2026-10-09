// Command childapp is the scripted application the e2e tests run inside a
// pseudo-terminal. It renders one status row plus an event log:
//
//	READY K:<keys> S:<suspends> R:<resumes> <cols>x<rows>
//	EVT:key=a
//	EVT:resize=100x30
//	...
//
// The renderer is diff-based: a frame that changes one cell writes only that
// cell, so a counter tick never appears as contiguous bytes on the pty
// master. The event log exists for exactly that reason: a marker is drawn on
// a previously blank row, and blank-to-text is a whole-row diff — the full
// marker string rides the wire in one piece. The status row, in contrast, is
// only rewritten whole by full repaints (initial paint, post-resume), which
// is what its assertions pin. Markers are space-free on purpose: a diff row
// skips internal space cells with cursor-forward escapes, so a space in a
// marker breaks it into pieces on the wire. Quits on 'q' or Escape; Ctrl+Z
// is claimed by the runtime's suspend handshake and never reaches the model,
// so the S: and R: counts are the suspend oracle.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

const markerRows = 20

type model struct {
	keys     int
	suspends int
	resumes  int
	markers  []string
}

func (m *model) mark(text string) {
	if len(m.markers) < markerRows {
		m.markers = append(m.markers, text)
	}
}

func (m *model) Update(next event.Event) lux.Command {
	switch next.Kind {
	case event.Key:
		if next.Key.Action == event.ActionPress || next.Key.Action == event.ActionRepeat {
			m.keys++
			name := "escape"
			if next.Key.Code == event.KeyRune {
				name = string(next.Key.Rune)
			}
			m.mark("EVT:key=" + name)
		}
		if next.Key.Code == event.KeyEscape || (next.Key.Code == event.KeyRune && next.Key.Rune == 'q') {
			return lux.Quit()
		}
	case event.Resize:
		m.mark(fmt.Sprintf("EVT:resize=%dx%d", next.Width, next.Height))
	case event.Suspend:
		m.suspends++
		m.mark("EVT:suspend")
	case event.Resume:
		m.resumes++
		m.mark("EVT:resume")
	}
	return nil
}

func (m *model) View(frame *lux.Frame) {
	width, height := frame.Size()
	frame.Canvas.Text(0, 0, fmt.Sprintf("READY K:%d S:%d R:%d %dx%d",
		m.keys, m.suspends, m.resumes, width, height), render.Style{})
	for row, marker := range m.markers {
		frame.Canvas.Text(0, row+2, marker, render.Style{})
	}
}

func main() {
	// The error is not printed on purpose: stderr may be the same pty, and
	// one stray byte line would break the parent's byte-level assertions.
	_ = lux.Run(context.Background(), &model{},
		lux.WithIO(os.Stdin, os.Stdout),
		lux.WithTitle("lux-e2e"),
		lux.WithMaxFPS(240))
	// Hold the pty open briefly after the leave bytes are written. Linux
	// flushes the master's pending input when the last slave descriptor
	// closes — the process exit itself — so the test's reader needs a
	// window in which the final Close writes are readable while the slave
	// still exists. The sleep changes nothing under test: it runs after
	// Run has returned, and Run has already written the leave sequences.
	time.Sleep(150 * time.Millisecond)
}
