# Quick start

Snapshot: **2026-10-10**. This is the smallest honest path from zero to a
running LUX TUI program: one file, no widget toolkit, no dependencies beyond
the library itself. Every snippet below is lifted from
[examples/01-counter](../examples/01-counter/main.go), which builds with
`go build ./examples/01-counter/` — so this document cannot rot silently.

## The model is the whole program

A LUX application is one struct implementing two methods:

```go
type Model interface {
	Update(event.Event) Command
	View(*Frame)
}
```

`Update` mutates state in response to one terminal event — a key, a resize,
a paste, a mouse click. `View` draws one frame. Both run on the runtime
goroutine, so they never race each other; no locks are needed for anything
the model owns. Persistent state lives in the model, never in the draw path.

The counter model is four fields' worth of behavior:

```go
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
```

Three things to notice:

- **Quitting is a command.** `Update` never calls `os.Exit` or touches the
  terminal; it returns `lux.Quit()` and the runtime does the restore. A panic
  in your code is funneled through the same path, so the terminal is never
  left raw. The runtime recognizes `Quit()` itself: once your model returns
  it, no further frame is composed, one-shot sidebands (notifications,
  clipboard) queued in `View` fire exactly once, and the terminal's last
  image stays the frame your model intended — not an extra frame the
  scheduler happened to draw on the way out.
- **Keys arrive decoded.** `event.Key` carries a `Code` (`KeyUp`,
  `KeyEscape`, …) plus modifiers; printable keys are `KeyRune` with the rune
  in `Key.Rune`. Ctrl+letter is not a fake key — it is a rune with the
  `ModCtrl` bit set, which is why the counter's Ctrl+C check reads
  `next.Key.Is(event.KeyRune, event.ModCtrl)`.
- **Returning `nil` is normal.** Most events do not change state. An empty
  return means "no follow-up work"; the runtime goes back to sleep and the
  idle loop never wakes a timer.

## View draws into cells

`View` receives a `*lux.Frame` valid only for the duration of the call:

```go
func (c *counter) View(frame *lux.Frame) {
	w, h := frame.Size()
	canvas := frame.Canvas

	label := fmt.Sprintf("count: %d", c.count)
	if canvas.TextWidth(label) > w {
		label = fmt.Sprintf("%d", c.count)
	}
	canvas.Text((w-canvas.TextWidth(label))/2, h/2-1, label,
		render.Style{Foreground: ink})
	// ... the hint row, same centered idiom
}
```

`canvas.Text` places a string at cell coordinates and returns the advanced
column, so callers that care about width never re-measure. `TextWidth`
counts terminal cells — combining marks contribute zero, CJK and emoji
contribute their real width — so the centered math above stays correct for
any content.

The frame carries more than the canvas: `frame.Now` is the animation clock,
`frame.Delta` is time since the previous frame, `frame.Capabilities` is the
negotiated feature set, and `frame.Metrics` reports the presentation path.
[ANIMATION.md](ANIMATION.md) uses two of those; the rest are there when you
need them.

## Run it

```go
func main() {
	if err := lux.Run(context.Background(), &counter{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

`lux.Run` negotiates capabilities (kitty keyboard, synchronized output,
scroll regions — whatever the terminal answers within a bounded probe),
installs the renderer, pumps events until `Quit`, and restores the terminal
on every exit path. From a redirected pipe or `TERM=dumb` it degrades to
plain snapshots instead of escaping.

That is the whole program: `go build ./examples/01-counter/`, run it, press
up/down and space, resize the window, press `q`.

## Where to go next

- [ANIMATION.md](ANIMATION.md) — the motion loop: tweens, springs, and why
  animation timing survives late frames.
- [CUSTOM-CONTROL.md](CUSTOM-CONTROL.md) — your own control in an evening:
  the `ui` package's routing, hit-testing, and focus for free.
- [examples/05-widgets](../examples/05-widgets/main.go) — the bundled controls in
  one runnable sheet.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the runtime, renderer, and
  terminal adapter actually fit together.
