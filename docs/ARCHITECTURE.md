# Architecture

Status snapshot: **2026-08-11**. This document distinguishes code present in
the repository from intended boundaries; roadmap items are explicitly labeled.

## Design constraints

1. Preserve correctness at terminal-cell boundaries, especially extended
   grapheme clusters and two-column glyphs.
2. Keep the hot path deterministic, bounded, and testable without a real TTY.
3. Depend inward: application behavior must not import a concrete terminal.
4. Add abstractions only when at least one complete user-facing slice needs
   them.
5. Prefer elapsed-time motion and frame coalescing over replaying late work.

## Hexagonal core

The repository follows a small ports-and-adapters shape:

```text
                         outside
          keyboard / mouse / resize / time
                           │
                    input adapter
                           │ event.Event
                           ▼
  ┌──────────────── application/use case ────────────────┐
  │ state transition → responsive layout → draw canvas  │
  └─────────────────────────┬────────────────────────────┘
                            │ render.Buffer
                            ▼
                     presentation port
                            │
                  VT / plain-output adapter
                            │
                         terminal
```

The public `lux.Run` runtime drives the use case. `lux.Model` receives events
and returns optional asynchronous `lux.Command` work; `View(*lux.Frame)` draws
immediately. `lux.Backend` is the transport port. Domain-facing packages do not
perform terminal I/O:

- `event` is the input vocabulary;
- `layout` maps available cells to rectangles;
- `motion` maps monotonic elapsed time to values;
- `render` owns cell state and drawing rules;
- `scene3d` projects CPU-rasterized triangle scenes into a `render.Canvas`;
- `ui` owns presentation-bound focus, hit routing, and composed controls;
- `capability` describes a backend's feature snapshot.

`internal/terminal` is the default outbound/inbound adapter. It owns raw mode,
escape parsing, resize observation, capability-derived encoding, output, and
restoration. Keeping it internal makes `lux.Backend`, rather than VT details,
the substitution boundary.

## Vertical slices

Hexagonal architecture controls dependency direction; vertical slices control
delivery. A feature is complete only when it traverses the real path and leaves
one focused check behind.

Example slice: **resize**

```text
SIGWINCH or Windows poll
  → event.Resize
  → classify available area
  → recompute flex rectangles
  → resize buffers safely
  → redraw
  → emit one coherent terminal update
```

Example slice: **animated progress**

```text
frame time
  → motion.Tween/Spring sampled at now
  → state-derived geometry
  → canvas cells
  → buffer diff
  → terminal bytes or plain snapshot
```

Slices should live close to their behavior rather than in horizontal
`controllers/services/repositories` scaffolding. Introduce a port when a slice
needs substitution for production and tests, not as speculation.

## Implemented boundaries

### Cell model and canvas

`render.Cell` stores one grapheme in its leading cell and marks a wide
grapheme's trailing cell as a continuation. Canvas operations clip atomically:
they do not leave half a wide glyph. Buffers support cloning, resizing,
comparison, changed-cell extraction, and front/back lifecycle.

A buffer also carries the caret the terminal cursor should occupy, because the
cursor is part of the frame being presented rather than a second channel to the
adapter. It is presentation state: cell equality and diffs ignore it, `Reset`
hides it so every frame restates it, a resize invalidates it, and a caret on a
wide grapheme's continuation cell snaps to the leading cell. `Canvas.SetCaret`
converts local coordinates and drops a caret outside the active clip.

### Responsive layout

`layout.Classify` combines width and height into `Tiny`, `Compact`, `Medium`, or
`Wide`. `layout.Flex` distributes a caller-owned destination across horizontal
or vertical constraints with basis, minimum, maximum, grow, shrink, and gap.
No stylesheet parser or retained widget tree exists today.

The intended adaptive sequence is:

1. preserve required content and focus;
2. remove optional decoration;
3. shorten labels;
4. stack columns into rows;
5. scroll or clip as the final fallback.

Each future widget must define its minimum useful size and fallback behavior.

### Native interaction

`ui.Context` is app-owned and immediate-mode: `Begin` starts a registry,
controls paint and register clipped terminal-space regions in final paint
order, and `End` attaches the immutable snapshot through `Frame.OnPresented`.
Only a successfully written frame commits its regions. Resize invalidates
coordinates immediately, and generation checks reject stale callbacks.

The context centralizes reverse-order hit testing, left-button capture,
Tab/BackTab traversal, Enter/Space activation, scroll routing, and one modal
focus scope with restoration. Disabled top regions occlude controls below.
There is deliberately no retained DOM, event bus, callback tree, CSS parser,
or spatial index; composite widgets can justify parent routing later.

Built-in vertical slices are Button, selectable List, Modal, and a single-line
TextInput. Text offsets are UTF-8 bytes at UAX #29 extended-grapheme boundaries,
using the same width policy as `render`.

Shortcuts are value registrations committed with the same presented snapshot:
focused bindings precede native control behavior, modal bindings suppress app
bindings, and explicit priority app bindings are limited to lifecycle commands.
`ScrollViewport` keeps only a row offset and returns a half-open visible range;
one-million-row models therefore remain O(visible) at draw time. A viewport
publishes whether it sits at a scroll boundary, which lets the router bubble the
wheel or a scroll key to an enclosing viewport without a retained parent tree,
and every region records the canvas clip it was drawn through so a container can
recognise and reveal its own scrolled-out children. Text history
clones snapshots under a byte budget, and clipboard access is a hexagonal app
port rather than a terminal escape sequence.

### Motion

Tweens and springs are sampled from `time.Time`. If rendering pauses, the next
sample reflects elapsed time; it does not enqueue every missed animation step.
There is no global animation scheduler or timeline yet.

### CPU 3D scene

`scene3d` currently provides vectors, matrices, camera/light defaults, indexed
triangles, cube geometry, near-plane clipping, back-face culling, a z-buffer,
half-block output, ANSI 16 quantization, and a monochrome ramp. Its reusable
renderer targets a `render.Canvas`, so it participates in the ordinary cell
diff path and has no GPU or native dependency.

### Terminal adapter

The current adapter provides:

- POSIX raw mode and `SIGWINCH` observation;
- Windows VT enablement and terminal-size polling;
- streaming UTF-8, legacy-key, SGR-mouse, focus, and bracketed-paste parsing;
- bounded escape and paste buffers;
- alternate-screen lifecycle and terminal restoration;
- changed-run ANSI encoding and RGB → 256 → 16 → monochrome downgrade;
- shortest-form output: a run is addressed relatively when that is fewer bytes
  than absolute addressing, a style is transmitted as a delta when that beats
  restating it, and a row whose tail is default-styled blanks is erased rather
  than transmitted;
- a scrolling region for vertical row shifts, so moving a list costs the rows
  it exposes rather than the rows it moves;
- caret positioning with `DECTCEM` visibility transitions, emitted after the
  cell runs that move the cursor;
- opt-in synchronized output through `LUX_TUI_SYNC_OUTPUT`;
- newline-delimited plain snapshots for redirected output or `TERM=dumb`.

Encoder correctness is proved rather than asserted: a test-only VT interpreter
covering exactly the sequences the encoder can emit replays real output into a
grid seeded from the previous frame, then requires that grid to equal the source
buffer under the active color depth, including the caret. It runs across the
four color depths with synchronized output on and off, over successive diffs,
and across a resize repaint. Every byte-saving shortcut is therefore paid for in
the interpreter first: an encoding the reference terminal cannot model is
rejected outright rather than trusted.

The one shortcut the encoder deliberately refuses is erasing a styled tail.
Erase-to-end-of-line fills with the active background on terminals with
background-color erase and with the default background on those without, so it
is only used for default-styled blanks, after the style has been reset.

Scrolling is the one operation where the encoder moves cells it never
transmits. It compares row hashes to find a vertical shift, emits a scrolling
region with insert- or delete-line, and applies the same shift to its model of
the terminal before diffing the rest. Only full-width row ranges qualify,
because a scrolling region has no portable horizontal margins, and a hash
collision can only cost bytes because the diff that follows compares real
cells. Because the model now describes an operation the encoder did not
transmit cell by cell, a failed write discards it: the next frame repaints.

Capability detection is deliberately conservative but currently environment
based. Active, bounded feature negotiation is roadmap work.

### Application runtime

`lux.Run` owns backend open/close, initial resize, serialized model updates and
views, commands, frames, size limits, and cancellation. Three reusable surfaces
bound the presentation path: one can be drawn, one queued, and one in flight.
When output falls behind, the queued surface is replaced by the latest state.

Animation is explicit. `Frame.RequestFrame`, `RequestFrameAfter`, and
`RequestFrameAt` coalesce to the earliest deadline; without a request or event,
the timer is stopped. Presentation cost lowers the target cadence through
60→30→20→10 FPS steps (bounded by `WithMaxFPS`) and recovers with hysteresis.
`Frame` also exposes elapsed `Delta`, capabilities, presentation metrics, and
the `WithReducedMotion` preference.

## Dependency rule

Allowed direction:

```text
application/example → lux runtime → event/layout/motion/render/capability ← adapter
```

Disallowed direction:

- `render` importing terminal or event-loop code;
- domain packages reading environment variables or file descriptors;
- widgets emitting raw escape sequences;
- application state depending on a concrete terminal brand.

## Roadmap, in dependency order

1. Harden the existing responsive CPU-3D showcase as the reference application
   slice.
2. Strengthen writer-failure/full-repaint recovery and test-terminal
   conformance.
3. Add shortcut scopes, virtualized viewports, clipboard and bounded undo.
4. Capability provenance, modern keyboard protocols, and multiplexer-aware
   negotiation.
5. Optional Kitty/Sixel/iTerm graphics adapters and richer subcell modes.
6. Extend the existing CPU 3D package only from measured needs; add GPU
   integration only after profiles prove it useful.

This order is intentional. A broad widget catalog before measured runtime and
showcase behavior would create APIs without a proven flow.
