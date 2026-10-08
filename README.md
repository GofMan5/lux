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

- Unicode grapheme correctness and current-width truth (18.0.0 overlay over
  uniseg segmentation) from layout through diffing;
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
| [`render`](./render) | cell buffers, grapheme-aware canvas, clipping/subviews, borders, styles, color values, resizing, diffing, double buffering, scrolling regions, caret placement, Unicode 18.0.0 terminal-width truth over uniseg segmentation, a width policy for terminals that disagree, and single-pass word wrap that never splits a grapheme |
| [`layout`](./layout) | responsive size classes and deterministic horizontal/vertical flex constraints |
| [`motion`](./motion) | absolute-time tweens, easing functions, and a critically damped spring |
| [`scene3d`](./scene3d) | dependency-free transforms, camera/light, meshes, clipping, z-buffered triangle rasterization, and half-block/ANSI/monochrome output |
| [`ui`](./ui) | presentation-bound focus/hit/modal routing, scoped shortcuts, buttons, EGC text input with bounded undo, drag selection and a hardware caret, small lists, virtual scroll viewports with wheel bubbling, modal panels, and an explicit clipboard port |
| [`event`](./event) | compact keyboard, text, paste, mouse, resize, focus, frame, quit, and error events |
| [`capability`](./capability) | immutable terminal feature snapshots and portable defaults |
| `internal/terminal` | VT lifecycle, startup capability negotiation (DA1/DECRQM/kitty query, bounded, veto-only with four enable-only verdicts), streaming input parser with kitty CSI u and win32-input-mode record decoding, mouse (SGR cell plus opt-in kitty pixel 1016), paste, and focus decoding, resize handling, shortest-form ANSI diff encoding with scrolling regions, styled underlines and opt-in OSC 8 hyperlinks, Unicode core mode 2027, caret and cursor visibility, color downgrade, plain-output fallback, and a VT reference-grid conformance test |

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

Or the multiline editor, which exercises wrap, grapheme caret moves, bounded
undo, and the clipboard port:

```sh
go run ./examples/textedit
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
but remains intentionally single-line; password hardening, RTL visual
reordering, OS clipboard adapters, and IME preedit are not claimed.

`ui.TextArea` extends the same grapheme contract to multiline documents:
logical lines, soft wrap in word or character mode, a vertical viewport with
wheel and key scrolling, selection painting, bounded undo/redo, and the same
clipboard port. Layout is cached as a dirty range, so editing near the caret
re-wraps only the touched lines, and ASCII boundary probes take a constant-time
fast path before falling back to the grapheme scanner. What is not claimed:
per-row incremental rewrap of a single huge line, hard tabs beyond fixed
4-column stops, RTL visual reordering, and IME preedit.

`ui.Table` renders virtualized tabular data: fixed-width and rubber columns
(Weight shares the leftover width), a header row with click-to-sort, keyboard
and wheel navigation, and row selection with viewport-follow. The widget asks
the application for one row at a time through `rowAt`, so a million-row source
paints only the visible window — the 120×40 million-row benchmark costs
~1.8 ms per repaint with 41 allocations. Sorting stays a request: the table
reports `SortRequested` and the app re-slices its own data, so the widget never
mutates the source. Not claimed: horizontal scrolling, row height above one
cell, per-cell content styling, and IME preedit. `examples/table` shows the
full pattern with 200k rows.

Some terminals measure differently from the model: a CJK-locale xterm doubles
East Asian Ambiguous codepoints, legacy consoles squeeze emoji into one cell,
and some stacks ignore emoji presentation selectors. `lux.WithWidthPolicy`
describes such a terminal to lux — drawing, measuring, clipping, the caret,
and mouse mapping then all follow the described cells coherently, while the
defaults stays byte-identical to a width-agnostic program. Terminals that
answer the probe's mode 2027 query positively (the contour-led Unicode
core proposal, ALPHA) additionally get that mode entered, holding the
terminal to the same grapheme-cluster contract lux measures by itself —
and an explicit width policy suppresses it, since one stream follows one
width authority.

Nested viewports need no widget tree: a scrollable region publishes whether it
sits at a boundary, and the router bubbles the wheel or a scroll key to the
enclosing region that can still move. Tab also reaches a control a container has
scrolled out of view, and the container scrolls it back into sight.

Scrolling is also cheap on the wire. The encoder recognises a vertical row shift
and lets the terminal move those rows, so a list that scrolls by one row costs
the same handful of bytes whether the screen is 80×24 or 120×40.

## Capabilities

Defaults come from the environment (`TERM`, `COLORTERM`, `NO_COLOR`) and the
platform. Then a single bounded probe at startup asks the terminal itself
(primary and secondary device attributes, mode reports for scroll regions,
synchronized output, pixel mouse, win32 input, and Unicode core, and the
kitty keyboard query). The scroll-mode
probe can only veto, never
invent; a silent or slow terminal keeps the conservative defaults, and
redirected output skips the probe entirely for the plain-text profile. The
answers carry provenance: the device reports and the environment together
reveal when a multiplexer answered on behalf of the host, and inside a
detected tmux or GNU screen the input-family verdicts (kitty keyboard,
pixel mouse, win32 input) are downgraded — a pane never runs an input
encoding the multiplexer does not re-emit, while output-family verdicts
(scroll regions, synchronized output, Unicode core) stay, because tmux
implements those for its panes itself. The
other queries are enable-only — a kitty flags reply turns on the
enhanced keyboard, where Esc decodes immediately and Ctrl+letter splits from
its C0 alias; on Windows, a mode 9001 answer that recognizes win32 input
mode turns on the ConPTY record encoding (press/repeat/release actions,
console modifier truth, and AltGr as a plain character) with the same
guaranteed reset on every exit path, and kitty wins when both answer;
a mode 2026 answer that recognizes the mode turns on the
frame-synchronization wrapper (silence never does), and
`LUX_TUI_SYNC_OUTPUT` overrides that verdict in either direction; a mode
2027 answer that recognizes the mode enters Unicode core mode with the
same guaranteed reset on every exit path — silence, rejection, and an
explicit width policy all keep it off. Mouse
tracking is all-motion (1003 with SGR 1006): hover states
follow plain pointer motion, and SGR releases carry the real button.
`WithPixelMouse` opts into kitty's pixel-position mode (1016) where the probe
confirms it, carrying `PixelX`/`PixelY` and back/forward buttons. Styles
carry the kitty underline shapes (`4:1`-`4:5`) and underline color
(`58`/`59`) at every color depth, and `WithHyperlinks(true)` opts into OSC 8
link regions — undetectable by design, so the default stays byte-identical
to a links-unaware program.
Keystrokes that race the probe are replayed in order, never
dropped. `LUX_TUI_PROBE_TIMEOUT` raises the 150 ms budget or turns
negotiation off for deterministic runs. Features degrade in this order:

```text
Kitty/Sixel/iTerm image → half-block/braille cells → ASCII
24-bit RGB             → ANSI 256              → ANSI 16 → monochrome
Unicode border         → ASCII border
modern keyboard        → legacy VT key events
interactive UI         → deterministic plain snapshot
```

See [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) for the full behavior and
the open requirements.

## Direction

Capability negotiation landed, and the keyboard-truth stage is underway:
the kitty protocol is queried at startup and pushed (disambiguate +
event-type flags) only on a positive answer, with a guaranteed pop on every
exit path. Key events carry press/repeat/release actions, modifier bits up
through caps/num lock, and kitty's private-use functional-key table
(F13-F35, media, keypad). Mouse truth has landed: all-motion SGR tracking
with the pixel-position mode (1016) as an opt-in that maps pixel reports to
cells via the queried cell size, plus back/forward buttons and kitty's
pointer-leave sentinel. Styled underlines and hyperlinks have landed: the
kitty underline shapes and underline color ride the style diff at every
depth, and OSC 8 regions are opt-in with a frame contract that never leaks
an open region. Synchronized output has landed the same way: the probe
enables mode 2026 wrapping only on a recognized answer, with
`LUX_TUI_SYNC_OUTPUT` as the force-on/force-off escape hatch. The Windows
counterpart has landed too: win32-input-mode (9001) is probed and enabled on
a recognizing answer, decoding the ConPTY key records with both ctrl keys
into the ctrl bit, AltGr as a plain rune, console repeat counts, and
modifier bookends as honest unknown-key releases. Multiplexer provenance has
landed too: the probe latches DA1 and DA2 (first answerer wins) and reads
`$TMUX`/`$STY`/`$TERM`, so a pane that answered through tmux or GNU screen
keeps the legacy input path even when a mis-routed or forged reply claims
kitty support — that closes the keyboard-truth stage. The text-truth stage
has landed: the 18.0.0 width overlay and the width policy, Unicode core
mode 2027 following the probe verdicts with a guaranteed
reset on every exit path (an explicit width policy suppresses it, since
one stream follows one width authority), word wrap (`WrapLines` and
`Canvas.TextWrap` measure every grapheme through the width policy, never
split a cluster, and charge control graphemes zero cells exactly as the
canvas draws them), and the multiline `TextArea` on the same boundary
authority — that closes the text-truth stage. Next: the widget-truth
stage, variable-height virtualization and semantic
accessibility bridges. Graphics protocols follow only after these paths are
measurable.
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
