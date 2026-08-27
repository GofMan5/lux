# LUX TUI

**A small Go foundation for fast, Unicode-correct terminal interfaces.**

[![License: MIT OR Apache-2.0](https://img.shields.io/badge/license-MIT%20OR%20Apache--2.0-blue.svg)](#license)

> **Pre-release:** the public runtime, renderer, responsive layout, immediate-
> mode interaction context, core controls, motion, terminal adapter, and CPU 3D
> rasterizer exist. API stability is not promised yet.

LUX TUI treats the terminal as a constrained rendering target, not a stream of
decorated strings. The current core stores complete extended grapheme clusters,
protects wide-cell continuations, computes deterministic cell diffs, adapts
colors to terminal depth, and samples motion from absolute time so a delayed
machine skips stale animation frames rather than replaying them.

## Why another TUI library?

The Go ecosystem offers excellent event loops, styling libraries, widgets, and
cell backends. The gap is a single lightweight stack that combines:

- Unicode grapheme correctness from layout through diffing;
- responsive, content-driven layout without a browser-sized runtime;
- smooth motion whose timing remains correct when frames are late;
- conservative capability degradation for local, remote, and redirected output;
- a terminal-independent renderer that is easy to benchmark and snapshot-test;
- one ordinary Go binary, without a JavaScript or native-GPU runtime.

The project does **not** claim to be faster than existing libraries yet. The
measurement contract is documented in [docs/PERFORMANCE.md](docs/PERFORMANCE.md);
comparative claims require reproducible results.

## Current packages

| Package | Implemented now |
|---|---|
| [`lux`](./app.go) | public `Model`/`Command` runtime, frames and metrics, backend port, idle event loop, adaptive pacing, latest-frame backpressure, lifecycle cleanup, runtime options |
| [`render`](./render) | cell buffers, grapheme-aware canvas, clipping/subviews, borders, styles, color values, resizing, diffing, double buffering, scrolling regions, caret placement |
| [`layout`](./layout) | responsive size classes and deterministic horizontal/vertical flex constraints |
| [`motion`](./motion) | absolute-time tweens, easing functions, and a critically damped spring |
| [`scene3d`](./scene3d) | dependency-free transforms, camera/light, meshes, clipping, z-buffered triangle rasterization, and half-block/ANSI/monochrome output |
| [`ui`](./ui) | presentation-bound focus/hit/modal routing, scoped shortcuts, buttons, EGC text input with bounded undo, drag selection and a hardware caret, small lists, virtual scroll viewports with wheel bubbling, modal panels, and an explicit clipboard port |
| [`event`](./event) | compact keyboard, text, paste, mouse, resize, focus, frame, quit, and error events |
| [`capability`](./capability) | immutable terminal feature snapshots and portable defaults |
| `internal/terminal` | VT lifecycle, streaming input parser, mouse/paste/focus decoding, resize handling, shortest-form ANSI diff encoding with scrolling regions, caret and cursor visibility, color downgrade, plain-output fallback, and a VT reference-grid conformance test |

The concrete terminal adapter stays internal; applications use `lux.Run` or
provide the public `lux.Backend` port for a PTY, SSH session, recorder, or test.

## Quick start

```sh
go get github.com/lux-tui/lux
```

```go
package main

import (
	"context"
	"fmt"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

type app struct{ width, height int }

func (a *app) Update(next event.Event) lux.Command {
	switch next.Kind {
	case event.Resize:
		a.width, a.height = next.Width, next.Height
	case event.Key:
		if next.Key.Code == event.KeyEscape || next.Key.Rune == 'q' {
			return lux.Quit()
		}
	}
	return nil
}

func (a *app) View(frame *lux.Frame) {
	style := render.Style{
		Foreground: render.RGBColor(139, 92, 246),
		Attrs:      render.AttrBold,
	}
	frame.Canvas.Border(frame.Canvas.Bounds(), render.RoundedBorder, style)
	frame.Canvas.Text(2, 2, "LUX TUI · 界面 · 👨‍👩‍👧‍👦", style)
}

func main() {
	if err := lux.Run(context.Background(), &app{}); err != nil {
		panic(fmt.Errorf("run LUX TUI: %w", err))
	}
}
```

`View` runs serially on the runtime goroutine. Persistent state belongs in the
model. Call `frame.RequestFrame()`, `RequestFrameAfter`, or `RequestFrameAt`
only while animation is active; an idle application has no redraw ticker.

Run the checks:

```sh
go test ./...
go vet ./...
```

Launch the adaptive animated 3D showcase:

```sh
go run ./examples/showcase
```

The module currently declares Go 1.23. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before extending the core.

## Native interaction

`ui.Context` is LUX TUI's own immediate-mode interaction layer. Applications
route an event through `Context.Handle` in `Update`, then call `Begin`, draw and
register controls, and `End` in `View`. Hit geometry becomes active only after
that exact frame is successfully presented; dropped frames cannot receive a
click. Buttons always have Enter/Space equivalents, Tab traverses focus, modal
scopes trap and restore focus, and lists support arrows/pages/wheel/click.

`ui.TextInput` edits extended grapheme clusters rather than runes. Its byte
offsets remain grapheme boundaries across typing, bounded paste, selection,
navigation, deletion, wide-cell mouse placement, drag selection, and horizontal
scrolling. The focused field also owns the real terminal cursor, so IME
candidate windows and screen readers follow the caret instead of a painted cell.
It includes byte-bounded undo/redo and an application-supplied clipboard port,
but remains intentionally single-line; password hardening, multiline editing,
RTL visual reordering, OS clipboard adapters, and IME preedit are not claimed.

Nested viewports need no widget tree: a scrollable region publishes whether it
sits at a boundary, and the router bubbles the wheel or a scroll key to the
enclosing region that can still move. Tab also reaches a control a container has
scrolled out of view, and the container scrolls it back into sight.

Scrolling is also cheap on the wire. The encoder recognises a vertical row shift
and lets the terminal move those rows, so a list that scrolls by one row costs
the same handful of bytes whether the screen is 80×24 or 120×40.

## Direction

Next slices are active capability negotiation, modern keyboard protocols,
variable-height virtualization, and semantic accessibility bridges.
Graphics protocols follow only after these paths are measurable.
The existing CPU `scene3d` package proves the cell-rendering path; it is not a
GPU engine.

- [Architecture](docs/ARCHITECTURE.md)
- [Compatibility](docs/COMPATIBILITY.md)
- [Interaction model](docs/INTERACTION.md)
- [Research snapshot](docs/RESEARCH.md)
- [Performance contract](docs/PERFORMANCE.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)

## License

Licensed at your option under either:

- [Apache License, Version 2.0](LICENSE-APACHE), or
- [MIT License](LICENSE-MIT).

Unless explicitly stated otherwise, contributions are offered under the same
dual license. Dependency notices are listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
