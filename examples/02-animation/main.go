// Command animation demonstrates the motion loop: a looping tween and a
// spring that retargets on demand, both sampled from frame.Now so late
// frames skip stale states instead of replaying them. It is the walkthrough
// target of docs/ANIMATION.md.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
)

const (
	tweenDuration  = 1200 * time.Millisecond
	springResponse = 900 * time.Millisecond
)

var (
	ink    = render.RGBColor(229, 231, 235)
	track  = render.RGBColor(64, 64, 80)
	violet = render.RGBColor(139, 92, 246)
	muted  = render.RGBColor(107, 114, 128)
)

// animation holds motion as absolute-time values. A Tween or Spring is an
// immutable sample of "value at time t": nothing advances them in Update,
// so animation never competes with input handling.
type animation struct {
	// loop runs 0→1 and restarts when it completes.
	loop motion.Tween

	// spring glides between 0 and 1; space flips its target from wherever
	// it currently is.
	spring motion.Spring
	target float64
}

func newAnimation(now time.Time) *animation {
	return &animation{
		loop: motion.Tween{
			From:     0,
			To:       1,
			Start:    now,
			Duration: tweenDuration,
			Ease:     motion.InOutCubic,
		},
		spring: motion.Spring{
			From:     0,
			To:       1,
			Start:    now,
			Response: springResponse,
		},
		target: 1,
	}
}

func (a *animation) Update(next event.Event) lux.Command {
	if next.Kind != event.Key {
		return nil
	}
	switch {
	case next.Key.Code == event.KeyEscape,
		next.Key.Code == event.KeyRune && next.Key.Rune == 'q':
		return lux.Quit()
	case next.Key.Code == event.KeyRune && next.Key.Rune == ' ':
		// Retarget from the current sampled position: springs are replaced,
		// never nudged. The velocity carries over through From/Velocity.
		position, velocity := a.spring.Value(next.At)
		if a.target > 0 {
			a.target = 0
		} else {
			a.target = 1
		}
		a.spring = motion.Spring{
			From:     position,
			To:       a.target,
			Velocity: velocity,
			Start:    next.At,
			Response: springResponse,
		}
	}
	return nil
}

func (a *animation) View(frame *lux.Frame) {
	w, h := frame.Size()
	canvas := frame.Canvas
	now := frame.Now

	// The looping tween: restart it when it completes.
	progress, done := a.loop.Value(now)
	if done {
		a.loop = motion.Tween{
			From:     0,
			To:       1,
			Start:    now,
			Duration: tweenDuration,
			Ease:     motion.InOutCubic,
		}
		progress = 0
	}
	settle, _ := a.spring.Value(now)

	// Tween row: a filled gauge.
	ty := h/2 - 2
	gaugeW := max(w-14, 4)
	filled := int(progress * float64(gaugeW))
	if filled > gaugeW {
		filled = gaugeW
	}
	if filled > 0 {
		canvas.Fill(render.Rect{X: 10, Y: ty, W: filled, H: 1}, "█",
			render.Style{Foreground: violet})
	}
	if gaugeW-filled > 0 {
		canvas.Fill(render.Rect{X: 10 + filled, Y: ty, W: gaugeW - filled, H: 1}, "─",
			render.Style{Foreground: track})
	}
	canvas.Text(2, ty, fmt.Sprintf("%3d%%", int(progress*100+0.5)),
		render.Style{Foreground: ink})

	// Spring row: a diamond gliding to (or from) the right edge.
	sy := h/2 + 2
	rowW := max(w-12, 1)
	pos := int(settle * float64(rowW-1))
	if pos < 0 {
		pos = 0
	}
	if pos > rowW-1 {
		pos = rowW - 1
	}
	canvas.Fill(render.Rect{X: 6, Y: sy, W: rowW, H: 1}, "·",
		render.Style{Foreground: track})
	canvas.Text(6+pos, sy, "◆", render.Style{Foreground: violet})

	canvas.Text(2, ty-2, "tween · ease in-out cubic · 1.2 s loop", render.Style{Foreground: muted})
	canvas.Text(2, sy+2, "spring · critically damped · space retargets", render.Style{Foreground: muted})

	// Keep frames coming while anything moves: the runtime arms its
	// adaptive pacer from this call, and idle apps never wake.
	frame.RequestFrame()
}

func main() {
	if err := lux.Run(context.Background(), newAnimation(time.Now())); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
