# TUI landscape research

Research snapshot: **2026-08-08**; document refreshed **2026-08-09**. Sources
are upstream repositories, documentation, and implementation files. Star counts
were verified from the GitHub REST API on the snapshot date and are only a
popularity signal. Performance descriptions below identify architecture and
mechanisms, not independently reproduced benchmark wins.

## Executive finding

The mature libraries specialize:

- Bubble Tea excels at Elm-style Go application flow and ecosystem;
- tview/tcell provides mature Go widgets over a capable cell backend;
- Ratatui offers an especially clear immediate-mode buffer/diff model;
- Textual leads high-level responsive styling and animation ergonomics;
- prompt_toolkit excels at editable command-line applications;
- Ink brings React/Yoga composition to Node;
- OpenTUI is the closest modern visual competitor, with a Zig core, Yoga,
  graphics protocols, and optional WebGPU/Three.js;
- Notcurses and libvaxis demonstrate modern capability and Unicode handling.

The opportunity for LUX TUI is not “more widgets.” It is a cohesive Go-native
path from events through responsive layout and elapsed-time motion into a
Unicode-correct, capability-aware renderer, with measurable behavior under lag.

## Verified popularity and licenses

| Project | Language | Stars | License |
|---|---:|---:|---|
| Rich | Python | 57,037 | MIT |
| Bubble Tea | Go | 44,243 | MIT |
| Ink | TypeScript | 39,593 | MIT |
| Textual | Python | 36,888 | MIT |
| Ratatui | Rust | 22,134 | MIT |
| tview | Go | 14,025 | MIT |
| termui | Go | 13,581 | MIT |
| OpenTUI | TypeScript/Zig | 12,925 | MIT |
| blessed | JavaScript | 11,877 | MIT |
| Lip Gloss | Go | 11,681 | MIT |
| gocui | Go | 10,596 | BSD-3-Clause |
| prompt_toolkit | Python | 10,549 | BSD-3-Clause |
| tcell | Go | 5,211 | Apache-2.0 |
| Cursive | Rust | 4,838 | MIT |
| Notcurses | C | 4,654 | Apache-2.0 |
| asciimatics | Python | 4,299 | Apache-2.0 |
| Crossterm | Rust | 4,174 | MIT |
| terminal-kit | JavaScript | 3,373 | MIT |
| Urwid | Python | 3,012 | LGPL-2.1-or-later |
| libvaxis | Zig | 1,945 | MIT |
| tui-realm | Rust | 979 | MIT |
| termbox2 | C | 742 | MIT |

## Architecture comparison

| Stack | Architecture and rendering | Layout / animation | Principal trade-off |
|---|---|---|---|
| Bubble Tea v2 + Lip Gloss v2 | Elm Model/Update/View/Cmd; cell renderer, diffing, synchronized output support | string composition; ticks or separate motion packages | excellent Go DX, but responsive widget/layout and animation remain application work |
| tview + tcell v3 | retained widgets over logical/physical cell screen; efficient `Show` | Flex, Grid, Pages; no animation system | mature widgets, less suited to composited cinematic scenes |
| Ratatui | immediate frame rebuilt each draw; zero-allocation buffer diff; swappable backends | constraint/flex solver; app-owned timing | deterministic and testable, but runtime/input/state scheduling are deliberately external |
| Textual | retained DOM, async messages, reactive invalidation, dirty-region compositor | CSS-like grid/flex units; built-in easing animation | best high-level ergonomics with Python/runtime overhead |
| prompt_toolkit | compositional controls/windows; previous/current screen diff | application containers; invalidation throttling | exceptional editor/input foundation, not a graphics framework |
| Ink 7 | React reconciler + Yoga; optional changed-line incremental rendering | Flexbox; shared absolute/delta animation hook | familiar composition with Node/React/Yoga cost and line-oriented output |
| OpenTUI | retained Yoga tree, Zig buffers/diff, optional render thread, React/Solid adapters | Flex; native buffers; Three.js/WebGPU extension | closest feature competitor, but adds JS, native binaries, Zig, and GPU/readback complexity |
| Notcurses | planes/sprites, Unicode EGC cells, rendered-mode optimization, Kitty/Sixel/framebuffer | visual primitives rather than app framework | rich modern terminal graphics with native/terminfo complexity and a modern-terminal bias |
| libvaxis | low-level cell API plus Flutter-like `vxfw`; terminal queries and double buffering | retained stateful widgets; explicit redraw | modern protocol model, smaller ecosystem and Zig toolchain churn |

## Important implementation lessons

### Rendering

Ratatui, tcell, prompt_toolkit, blessed, asciimatics, OpenTUI, libvaxis, and
ncurses all reinforce the value of a logical screen distinct from physical
terminal state. LUX TUI follows that direction. Correct diffs must account for
wide-glyph trailing cells, resize invalidation, cursor state, output errors, and
the cost of moving the cursor—not merely compare strings.

### Unicode

Modern implementations increasingly store extended grapheme clusters rather
than runes. Width remains terminal-dependent. A sound core needs UAX #29
segmentation, configurable width policy, safe last-column behavior, and
eventual support for Unicode Core Mode 2027/explicit-width negotiation.

### Input

Legacy VT input is ambiguous: for example, Ctrl-I and Tab share a code. Kitty
and xterm keyboard protocols improve this, but must be negotiated and disabled
cleanly. Paste and control strings need strict bounds. Mouse support should
progress from cell SGR to pixel coordinates only when positively supported.

### Responsive layout

Observed patterns include Bubble Tea's explicit resize message, Ratatui's
frame-relative constraints, tview's root-sized Flex/Grid, Textual's relative
units and overflow, Ink/OpenTUI's Yoga recalculation, and prompt_toolkit's
resize invalidation. LUX TUI should keep native cell constraints and a few
coarse size classes before considering a stylesheet language.

### Animation under lag

Fixed “advance one frame” loops slow down when the machine or connection is
late. Absolute elapsed time, coalesced invalidations, a single pending frame,
and adaptive cadence preserve interaction latency. OpenTUI and Ink expose
useful timing designs. LUX TUI now combines absolute-time motion with explicit
frame requests, one queued latest frame, and adaptive 60→30→20→10 pacing;
real-terminal calibration remains pending.

### 3D and terminal graphics

OpenTUI's Three package renders WebGPU via Three.js, reads pixels back, and
maps them into terminal buffers. It proves the experience is possible, while
also exposing the cost of GPU setup and readback. A Go design should begin with
a CPU projection/raster API that targets subcells, then add Kitty/Sixel images
and an optional GPU adapter only when measurements justify it.

## Requirements derived for LUX TUI

1. Keep `render` independent from OS and runtime concerns.
2. Preserve full graphemes and continuation-cell invariants everywhere.
3. Separate capabilities from terminal brand guesses and record provenance.
4. Offer plain deterministic output when interactivity is absent.
5. Give each responsive component a minimum useful size and fallback policy.
6. Coalesce render requests and sample motion using elapsed time.
7. Bound parser, queue, and output memory.
8. Test resize, Unicode, slow transports, output failures, and lifecycle
   restoration as first-class behavior.
9. Publish raw benchmarks before comparative marketing.
10. Keep the default distribution a normal Go binary.

## Sources

All URLs were accessed on 2026-08-08 or 2026-08-09.

### Go

- <https://github.com/charmbracelet/bubbletea>
- <https://github.com/charmbracelet/bubbletea/blob/main/cursed_renderer.go>
- <https://github.com/charmbracelet/lipgloss>
- <https://github.com/rivo/tview>
- <https://github.com/rivo/tview/blob/master/application.go>
- <https://github.com/gdamore/tcell>
- <https://github.com/gdamore/tcell/blob/main/README-windows.md>
- <https://github.com/gizak/termui>
- <https://github.com/jroimartin/gocui>

### Rust

- <https://github.com/ratatui/ratatui>
- <https://github.com/ratatui/ratatui/blob/main/ratatui-core/src/terminal/render.rs>
- <https://github.com/ratatui/ratatui/blob/main/ratatui-core/src/buffer/diff.rs>
- <https://github.com/crossterm-rs/crossterm>
- <https://github.com/gyscos/cursive>
- <https://github.com/veeso/tui-realm>

### Python

- <https://github.com/Textualize/textual>
- <https://textual.textualize.io/guide/reactivity/>
- <https://textual.textualize.io/guide/layout/>
- <https://textual.textualize.io/guide/animation/>
- <https://github.com/Textualize/textual/blob/main/src/textual/_compositor.py>
- <https://github.com/Textualize/rich>
- <https://github.com/prompt-toolkit/python-prompt-toolkit>
- <https://github.com/prompt-toolkit/python-prompt-toolkit/blob/main/src/prompt_toolkit/renderer.py>
- <https://github.com/urwid/urwid>
- <https://github.com/peterbrittain/asciimatics>

### JavaScript, TypeScript, and rendering engines

- <https://github.com/vadimdemedes/ink>
- <https://github.com/chjj/blessed>
- <https://github.com/cronvel/terminal-kit>
- <https://github.com/anomalyco/opentui>
- <https://github.com/anomalyco/opentui/blob/main/packages/core/src/zig/renderer.zig>
- <https://github.com/anomalyco/opentui/blob/main/packages/core/src/specs/terminal-startup.md>
- <https://github.com/anomalyco/opentui/blob/main/packages/three/src/WGPURenderer.ts>
- <https://github.com/dankamongmen/notcurses>
- <https://github.com/dankamongmen/notcurses/blob/master/TERMINALS.md>
- <https://github.com/termbox/termbox2>
- <https://github.com/rockorager/libvaxis>

### Standards and platforms

- <https://learn.microsoft.com/windows/console/console-virtual-terminal-sequences>
- <https://learn.microsoft.com/windows/console/classic-vs-vt>
- <https://invisible-island.net/ncurses/man/curs_refresh.3x.html>
- <https://www.unicode.org/reports/tr29/>
- <https://www.gnu.org/software/screen/manual/screen.html>
