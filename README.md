# LUX TUI

**A small Go foundation for fast, Unicode-correct terminal interfaces.**

[![License: MIT OR Apache-2.0](https://img.shields.io/badge/license-MIT%20OR%20Apache--2.0-blue.svg)](#license)

> **v0.1.0:** the first tagged release. The public runtime, renderer,
> responsive layout, immediate-mode interaction context, core controls,
> motion, terminal adapter, and CPU 3D rasterizer are in. Semver rules for
> `0.x` are stated in [CHANGELOG.md](CHANGELOG.md) — minor releases may
> break, patch releases never do.

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

Measured cross-framework numbers are now published. The committed full run
([benchmarks/results/report.md](benchmarks/results/report.md): 168 runs, 0
failed, one pinned host, one docker command to reproduce) shows lux
allocating 1–2 orders of magnitude fewer bytes per frame than the
bubbletea and tview lanes in every scene — full repaint 240x80: 55,504
B/frame for lux vs 5,464,384 for tea and 2,723,542 for tview — with 0.0 ms
p50 input-to-output latency in the input-burst scene (tview: 282.3 ms) and
8,581 us CPU/frame in the heaviest scene (tea: 88,920). The report also
records where lux loses: tview's wall-clock ms/frame is lower in the light
scenes (6.18 vs 12.67 static 120x40) at roughly 150x the allocations, and
tea paints nothing in some scenes, so per-frame cells for it are honestly
"—". The measurement contract is documented in
[docs/PERFORMANCE.md](docs/PERFORMANCE.md); every comparison above is
reproducible from `benchmarks/`.

## Current packages

| Package | Implemented now |
|---|---|
| [`lux`](./app.go) | public `Model`/`Command` runtime, frames and metrics, backend port, idle event loop, adaptive pacing, latest-frame backpressure, lifecycle cleanup, runtime options, and opt-in sideband channels (taskbar progress, notifications, clipboard) |
| [`render`](./render) | cell buffers, grapheme-aware canvas, clipping/subviews, borders, styles, color values, resizing, diffing, double buffering, scrolling regions, caret placement, Unicode 18.0.0 terminal-width truth over uniseg segmentation, a width policy for terminals that disagree, and single-pass word wrap that never splits a grapheme |
| [`layout`](./layout) | responsive size classes and deterministic horizontal/vertical flex constraints |
| [`motion`](./motion) | absolute-time tweens, easing functions, and a critically damped spring |
| [`scene3d`](./scene3d) | dependency-free transforms, camera/light, meshes, clipping, z-buffered triangle rasterization, and half-block/ANSI/monochrome output |
| [`ui`](./ui) | presentation-bound focus/hit/modal routing, scoped shortcuts, buttons, EGC text input with bounded undo, drag selection and a hardware caret, small lists, virtual scroll viewports with wheel bubbling, modal panels, and an explicit clipboard port |
| [`event`](./event) | compact keyboard, text, paste, mouse, resize, focus, frame, quit, and error events |
| [`capability`](./capability) | immutable terminal feature snapshots and portable defaults |
| `internal/terminal` | VT lifecycle, startup capability negotiation (DA1/DECRQM/kitty query, bounded, veto-only with four enable-only verdicts), streaming input parser with kitty CSI u and win32-input-mode record decoding, mouse (SGR cell plus opt-in kitty pixel 1016), paste, and focus decoding, resize handling, suspend/resume job control (`SIGTSTP`/`SIGCONT`, Windows console-close), shortest-form ANSI diff encoding with scrolling regions, styled underlines and opt-in OSC 8 hyperlinks, Unicode core mode 2027, caret and cursor visibility, color downgrade, plain-output fallback, and a VT reference-grid conformance test |

The concrete terminal adapter stays internal; applications use `lux.Run` or
provide the public `lux.Backend` port for a PTY, SSH session, recorder, or test.

## Quick start

```sh
go get github.com/GofMan5/lux
```

```go
package main

import (
	"context"
	"fmt"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
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

The examples are numbered as a curriculum:

| Example | What it shows |
| --- | --- |
| `01-counter` | The smallest complete program — model, keys, centered view ([QUICKSTART.md](docs/QUICKSTART.md) walks it line by line) |
| `02-animation` | The motion loop — tween and spring, retarget-from-momentum, `RequestFrame` ([ANIMATION.md](docs/ANIMATION.md)) |
| `03-textedit` | Multiline editing — wrap, grapheme caret moves, bounded undo, the clipboard port |
| `04-table` | Virtualized tabular data — 200k rows, sorting, selection |
| `05-widgets` | The widget catalog — every control end to end ([CUSTOM-CONTROL.md](docs/CUSTOM-CONTROL.md)) |
| `06-showcase` | The adaptive animated 3D showcase with a live Interaction Lab |
| `07-ssh` | A LUX TUI served over SSH — the engine in a child on a real pty |
| `08-tour` | The full-capability tour — nine tabs; Widgets / F2 opens the keyboard-first Tree, Select, and TextArea stand ([tour guide](examples/08-tour/README.md)) |
| `09-procs` | The async data app — a real process explorer: type-to-search filtering, loading/empty/error/retry phases, master-detail, cancellation, and a confirm-then-kill action with auto-refresh |
| `annex` | Sideband channels — taskbar progress, a toast, a clipboard write |

```sh
go run ./examples/01-counter
go run ./examples/02-animation
go run ./examples/06-showcase   # and so on
```

The SSH server is a nested Go module with its own build:

```sh
cd examples/07-ssh && go run .   # then: ssh -t -p 2222 localhost
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
cell, per-cell content styling, and IME preedit. `examples/04-table` shows the
full pattern with 200k rows, `examples/05-widgets` is the stage-8 widget
catalog (arrow keys, click selection, Escape quits), and the showcase's
Interaction Lab embeds a second live Table in its scrolling panel.

The form trio completes the same contract for picks and toggles:
`ui.Checkbox` (zero-value `CheckboxState`, toggles on Enter/Space or a
completed click, restyleable ASCII `[x]`/`[ ]` markers), `ui.RadioGroup`
(zero-value `RadioState`, navigation selects — arrows, Home/End, pages,
wheel, and row presses all move the choice), and `ui.Select` (zero-value
`SelectState` dropdown: the closed box opens on activation or any navigation
key, the borderless popup maps row `1..n` to `Offset + Y - 1` with row 0 the
inert anchor, wheel scrolls the popup viewport without moving the choice,
and the open region publishes `ContentHeight: 1+rows` so presses past the
options are dropped). Both the catalog and the Interaction Lab run them end
to end; the lab's form is live — the checkbox halves the cube's spin, the
radio recolors the scene border, the select retunes the rotation. Not
claimed: fuzzy filtering, popup auto-flip, multi-select, app-side
click-outside and Escape close (plain Escape never routes to regions).

Navigation strips and honest busy indicators follow the same rules.
`ui.Tabs` (zero-value `TabsState`) lays each tab out as its label plus one
padding cell per side with one layout math for both drawing and pointer
hits; navigation selects like a radio — Left/Right, Home/End, wheel — and
the closing press on a slot selects it directly. The strip virtualizes its
offset so a wide label set keeps the selected tab's slot whole. Horizontal
arrows are navigation keys in the router: a focused strip owns them even
inside a vertical viewport (vertical arrows still bubble to the enclosing
scroll, horizontal ones never do). `ui.Gauge` and `ui.Spinner` are
display-only by design — no region, no routing, no focus: the gauge clamps
Value/Max and paints full/half-block fill plus light-shade track, the
spinner's phase is a pure function of absolute time so 10 FPS and 240 FPS
presenters draw the same glyph at the same wall-clock instant. Not claimed:
multi-row tab strips, gauge seek, multi-cell spinner frames, a marquee.

Trend and hierarchy data close the set. `ui.Sparkline` (zero-value
`SparklineState{Values, Min, Max}`) paints the newest-right window as
eight-level block glyphs: pin Min/Max for a stable axis across frames or
leave both zero for per-frame auto-range; a degenerate range collapses to
the mid glyph and NaN draws as the floor. `ui.Tree` renders an
app-flattened row list — the application owns the expansion set and
contributes one `TreeRow{ID, Label, Depth}` per visible node, so
virtualizing a tree is just a windowed `rowAt`. The list contract holds:
arrows, Home/End, pages, and wheel move the selection, a press selects the
row under the pointer, and Enter, Space, or a completed click toggles the
row's expansion entry (`activated` always, `changed` only when the entry
actually flips). Both the widget catalog — the live sparkline rides the
motion page, the file tree its own — and the showcase's frame-time history
run them end to end. Not claimed: inline node filtering, per-node styling,
drag-and-drop reordering, lazy async children.

The control set is also a recipe, not a wall: building your own widget —
state, style, `Apply`, region registration, two wiring lines — is a
[docs/CUSTOM-CONTROL.md](docs/CUSTOM-CONTROL.md) evening walk, and the
document's worked stepper compiles and runs as a test in the `ui` package,
so the recipe cannot rot.

One palette runs across the whole set. `ui.Theme` is five roles — Text
(body), Muted (structure), Accent (selection and markers), Focus
(keyboard focus), Danger (disabled) — and `Context.UseTheme` installs it
per Context; there is no global mutable style table to collide with
another application's. The resolution rule is one line: a zero-value
style field reads its theme role, an explicit style always wins, and a
zero role falls back to the widget's own default. `DefaultTheme` is
attributes-only (dim structure, bold accents, bold+underlined focus,
reverse disabled), so the built-in look survives monochrome terminals and
redirected output while color themes stay opt-in, and focus stays visible
next to a selection without any hue — `ui.DefaultTheme()` is a function,
so restyling means passing your own value, never mutating a shared one.
`examples/09-procs` and `examples/05-widgets` run the recipe for real on
two screens — and since both converged on the same colors, that palette
now lives in the library: `ui.ReferencePalette()` returns the shared
`ui.Palette`, `Palette.Theme()` maps it onto the roles, and each
screen's constructor installs it with `UseTheme` while its local color
names alias the same value, so one edit restyles the theme roles and
the explicit chrome of both screens. Every selection widget (the table,
tabs, checkbox, radio group, select, tree) is drawn with a zero style
struct, and a test pins that swapping the palette restyles all of them
with no style block to edit. Two deliberate boundaries: layout chrome
(borders, labels, status lines) keeps explicit styles, because those
are facts of the layout rather than themeable control states; and the
glyph widgets — gauge, spinner, sparkline — keep explicit styles too,
because their colors are data painted as fg-colored glyphs, and a role
would invert the fg/bg semantics (an Accent gauge fill would draw dark
blocks on a violet background).

The window title is a lifecycle claim, not a per-frame one. `lux.WithTitle`
writes the title through OSC 0 bracketed by the XTWINOPS title stack — the
push (CSI 22;0 t) rides the same terminal write as the enter sequences and
the pop (CSI 23;0 t) the same write as the restore on every exit path, so
the syscall count does not change and the previous title comes back exactly
on terminals with the stack (xterm, kitty, VTE-family, tmux), while
terminals without it keep the program's title on exit — the documented
matrix. Control runes in the title are stripped before it is written: a BEL
or ESC would end the OSC string early and leak the rest as commands. Plain
and redirected output never writes title bytes, and the option only steers
the terminal backend Run constructs for itself — a custom adapter owns its
title policy. Not claimed: runtime retitling, a per-frame title, the
terminal's icon-only OSC 1 form.

The frame can also carry sideband channels, all opt-in. With
`WithProgress`, `WithNotifications`, and `WithClipboard` armed, a `View`
may call `frame.SetProgress(state, percent)` (taskbar progress through
OSC 9;4, five states plus a clamped percent), `frame.Notify(title, body)`
(desktop notification through OSC 9;777), and `frame.CopyToClipboard(text)`
(write-only OSC 52 — the read direction is never built, and payloads over
32 KiB are dropped rather than clipped). Each rides the same write as the
frame that queued it, after the diff and before the `OnPresented`
callbacks, and a dropped or failed frame drops its annexes with it — a
notification never outlives the frame that asked for it. Progress is a
reported state, not an event: the View re-states it every frame while the
bar is meant to exist, so suspend/resume restores it for free, and the
leave sequence always closes with the `9;4;0` reset so no taskbar bar
outlives the program. Consent is layered: all three channels default off,
OSC 52 is write-only, and the terminal's own prompt or `set-clipboard`
gate stays the terminal's decision — the library never reads the
clipboard and never pushes a notification unasked. The byte truth of all
three sequences is pinned by a real-terminal lane
(`.verification/matrix/annex-lane.sh`, raw pipe capture). Not claimed: an
OSC 52 read path, kitty's `OSC 52` clipboard-name extensions, and a
"notify with actions" protocol. `examples/annex` is the live demo.

Ctrl+Z works the way a shell user expects it to. `SIGTSTP` makes the
backend write the same leave sequences Close would write — modes, kitty
pop, title pop — and restore cooked mode before re-raising the stop, so
the shell reads clean input while the process is stopped. `SIGCONT`
(through `fg`) rewrites the enter sequences, drops the diff baseline, and
the next frame is a full repaint, because anything may have been printed
while the process was stopped. Models hear both ends as ordinary
`event.Suspend` and `event.Resume` updates and each dirties the frame, so
pausing an animation is a one-line `Update` case. The double stop is
latched out, a plain or closed backend refuses suspension, and a failing
raw-mode resume keeps the suspension held until a later resume recovers.
`lux.WithSuspend(false)` turns the whole behavior off — raw-mode ISIG stays
disabled, so a custom binding can claim the Ctrl+Z byte instead. Windows
has no job control: a console-close event releases the console immediately
and reports an Error event before the OS terminates the process —
best-effort cleanup, not a resume path.

External termination has an explicit contract on Unix. `SIGINT` and
`SIGTERM` are trapped by the runtime, which owns the cleanup half: the
terminal is restored immediately — the same leave sequences Close writes,
exactly once — and `Run` returns `lux.ErrInterrupt` or `lux.ErrTerminated`
after an ordinary unwind through the deferred Close, so a caller can map
them to the conventional exit statuses (130 and 143) with `errors.Is`.
The application owns the exit code, not the terminal. A Ctrl+C typed at
the terminal is deliberately not this path: raw mode claims the byte and
the model sees a Key event, which keeps interactive quit in the model's
hands. `SIGHUP` stays what it always was — terminal loss, an error exit
with no restore attempt, because the terminal is gone. `SIGKILL` is
promised nothing: no runtime can clean up after it, and this one does not
claim to.

The whole cycle is verified against real operating-system truth, not
fakes: an end-to-end suite (`e2e/`, Linux and macOS) runs the runtime
inside a pseudo-terminal under a live interactive shell, types the 0x1A
byte, and asserts the shell itself reports `Stopped` — a line only a
waitpid on a genuinely stopped job produces — before `fg` resumes it.
A second `SIGTSTP` raised straight at the stopped job is discarded by
`fg`'s SIGCONT, exactly one suspension is delivered, and keystrokes that
race the capability probe are replayed against the real kernel input
buffer. The termination contract is proven the same way: a real
`kill -TERM`/`kill -INT` against the pty session must leave the wait
status a clean exit — not a process killed by the default disposition —
with the restore bytes already on the wire.

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
authority — that closes the text-truth stage. The widget-truth stage has
landed whole: the virtualized `Table` with fixed and rubber columns and
hook-driven sorting, `Select` with a rofi-like type-to-filter, checkbox
and radio, `Tabs`, `Gauge`, `Spinner`, `Sparkline`, and the
app-virtualized `Tree`; the widget recipe (CUSTOM-CONTROL.md) whose
worked stepper compiles as a `ui` test, so the recipe cannot rot; the
immutable `Theme` (four roles, per-Context, no global mutable style
table) resolving across all nine styled widgets; and the window title
(`WithTitle`) riding the enter and leave writes with the XTWINOPS stack
restore — that closes the widget-truth stage. The real-terminal stage is
closed: job control (Ctrl+Z suspend / `fg`
resume with full repaint, Suspend/Resume events, SIGHUP as an error exit,
Windows console-close cleanup) is in, pinned by a pseudo-terminal
end-to-end suite that proves the real kernel stop and the real shell's
`Stopped` line, the double-stop discard, and probe-racing keystroke
replay; the Unix termination contract (SIGINT/SIGTERM trapped, terminal
restored before a clean error return, SIGHUP and SIGKILL outside the
promise) is pinned by the same lane's real-signal kills; a twelve-lane
terminal validation matrix (tmux and Screen ptys,
ssh over a real sshd/ssh pair, netem-shaped ssh, plain and dumb
profiles on Linux and Windows, CJK and NO_COLOR panes, an interactive
scripted Windows Terminal window, a headless conhost lifecycle, the
examples/07-ssh server, and the sideband raw-wire lane) runs as
self-asserting scripts with committed evidence; the SSH example serves
the engine over the wire by bridging each session onto a real pty; and
the frame's sideband channels — taskbar progress, notifications, and a
write-only clipboard — are pinned by byte truth and a decoded tmux
buffer. The prove-it stage is closed: the
[benchmarks module](benchmarks/README.md) ran its full comparative
campaign (168 runs, 0 failed; committed raw data, benchstat, and a
facts-only [report](benchmarks/results/report.md) reproduced by one
docker command), the README above quotes only those measured numbers,
and v0.1.0 is tagged on this repository. The planned
`docs/assets/demo-counter.gif` screencast is deferred: rendering the
committed [tape](docs/assets/demo-counter.tape) needs ffmpeg/ttyd from
package mirrors that the release sandbox cannot reach.
Graphics protocols and semantic accessibility bridges follow
only on measured demand, per the roadmap's post-1.0 gate.
The existing CPU `scene3d` package proves the cell-rendering path; it is not a
GPU engine.

- [Quick start](docs/QUICKSTART.md)
- [Animation](docs/ANIMATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Compatibility](docs/COMPATIBILITY.md)
- [Interaction model](docs/INTERACTION.md)
- [Your own control in an evening](docs/CUSTOM-CONTROL.md)
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
