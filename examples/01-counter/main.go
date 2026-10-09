// Command counter is the smallest complete LUX TUI program: a key-driven
// tally with a centered view, resize handling, and an exit path. It is the
// walkthrough target of docs/QUICKSTART.md.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

var (
	ink    = render.RGBColor(229, 231, 235)
	violet = render.RGBColor(139, 92, 246)
)

// counter is the whole application state. Update mutates it, View renders
// it, and nothing else holds state — persistent state always lives in the
// model, not in the draw path.
type counter struct {
	count int
}

func (c *counter) Update(next event.Event) lux.Command {
	switch next.Kind {
	case event.Resize:
		// The runtime sends the new size before the next View; the frame
		// already carries it, so the model only reacts if it caches layout.
		return nil
	case event.Key:
		switch {
		case next.Key.Code == event.KeyEscape:
			return lux.Quit()
		case next.Key.Code == event.KeyRune && next.Key.Is(event.KeyRune, event.ModCtrl) && next.Key.Rune == 'c':
			return lux.Quit()
		case next.Key.Code == event.KeyRune && next.Key.Rune == 'q':
			return lux.Quit()
		case next.Key.Code == event.KeyRune && next.Key.Rune == ' ':
			c.count++
		case next.Key.Code == event.KeyUp:
			c.count++
		case next.Key.Code == event.KeyDown:
			c.count--
		}
	}
	return nil
}

func (c *counter) View(frame *lux.Frame) {
	w, h := frame.Size()
	canvas := frame.Canvas

	label := fmt.Sprintf("count: %d", c.count)
	if canvas.TextWidth(label) > w {
		label = fmt.Sprintf("%d", c.count)
	}
	canvas.Text((w-canvas.TextWidth(label))/2, h/2-1, label,
		render.Style{Foreground: ink})

	hint := " up/down change · space increments · q or esc quits "
	if canvas.TextWidth(hint) > w {
		hint = " esc quits "
	}
	canvas.Text((w-canvas.TextWidth(hint))/2, h/2+1, strings.ToUpper(hint),
		render.Style{Foreground: violet})
}

func main() {
	if err := lux.Run(context.Background(), &counter{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
