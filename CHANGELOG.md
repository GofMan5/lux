# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No version has been tagged yet: everything below is unreleased pre-`v0.1.0`
work and the API may still change.

## [Unreleased]

### Added

- Core runtime (`lux`): `Model`/`Command` application loop, frames and metrics,
  public backend port, idle event loop, adaptive pacing, latest-frame
  backpressure, and lifecycle cleanup.
- Render package: grapheme-aware cell buffers, canvas with clipping and
  subviews, borders, styles, color values, deterministic diffing, double
  buffering, scrolling regions, and caret placement.
- Responsive layout: size classes and deterministic flex constraints.
- Motion: absolute-time tweens, easing functions, and a critically damped
  spring.
- `scene3d`: dependency-free transforms, camera and lighting, meshes, clipping,
  z-buffered triangle rasterization, and half-block/ANSI/monochrome output.
- Immediate-mode interaction layer (`ui`): focus/hit/modal routing, scoped
  shortcuts, buttons, EGC-correct single-line text input with bounded
  undo/redo, drag selection with a hardware caret, small lists, virtual scroll
  viewports with wheel bubbling, modal panels, and an explicit clipboard port.
- Runtime options: `WithSizeLimit`, `WithFallbackSize`, `WithIO`, `WithBackend`.
- Plain-output fallback: monochrome newline-delimited snapshots for redirected
  output and `TERM=dumb`.
- Capability negotiation before the first frame: one bounded DA1/DECRQM probe
  (150 ms default) that can only veto `ScrollRegions` and
  `SynchronizedOutput`; `LUX_TUI_PROBE_TIMEOUT` adjusts the budget or disables
  negotiation. Keystrokes that race the probe are replayed, never dropped.
- Kitty keyboard protocol (disambiguate + event-type flags): the startup
  probe asks `CSI ? u` and waits a 25 ms grace behind the classic reports; a
  flags reply is the one enable-only verdict and turns `KeyboardEnhanced`
  on. The push (`CSI > 3 u`) rides the enter write and the pop (`CSI < u`)
  leads the restore in the same write on every exit path, including panics
  funneled through `Run`'s deferred close. Enhanced mode decodes kitty
  `CSI u` keys: Esc is immediate (no escape delay), Ctrl+letter splits from
  its C0 alias, multi-modifier combos arrive, Shift+Tab normalizes to
  BackTab, and every modifier bit — super, hyper, meta, caps-lock,
  num-lock included — decodes. Key events carry a press/repeat/release
  action; releases are filtered by default and delivered with
  `WithKeyReleases(true)`, while widgets treat them as inert. Kitty's
  private-use functional-key codes (F13-F35, media and volume keys, keypad)
  decode onto real keys even in legacy mode, keypad keys normalizing to
  their non-keypad equivalents. The legacy path follows kitty where
  emulators diverge: `0x08` is Ctrl+Backspace, `CSI 29 ~` is Menu, a
  doubled escape prefixes Alt-modified arrows, and `~` codes 25-34 map to
  F13-F20. Silence keeps the legacy key path untouched.
- All-motion mouse tracking (`1003` with SGR `1006`): hover states update on
  plain pointer motion. xterm's no-button motion report (SGR button 35)
  decodes as a move instead of a phantom release — only the `m` final byte
  means release — and SGR releases carry the real button instead of
  collapsing to `MouseNone`.
- Opt-in pixel-precision mouse: `WithPixelMouse(true)` asks `CSI ? 1016 $ p`
  during the startup probe and swaps SGR `1006` for kitty's pixel-position
  mode on a recognized answer. Mouse events carry 0-based `PixelX`/`PixelY`
  (and cell `X`/`Y` via floor division once the `CSI 16 t` cell-size reply
  lands — re-queried on resize); back and forward buttons decode as
  `MouseBack`/`MouseForward`; kitty's pointer-leave code decodes as a move
  with `MouseNone` and every coordinate -1, which clears hover. A rejected
  or silent answer keeps the cell encoding, so the option stays inert.
  Malformed reports (buttons past 511, negative coordinates, the reserved
  130/131 codes) fall through to the unknown-key escape hatch.
- Styled underlines and opt-in hyperlinks: `render.Style` gains
  `UnderlineStyle` (single/double/curly/dotted/dashed — SGR `4:1`-`4:5`,
  shapes transmitted at every color depth) and `UnderlineColor` (the
  `58`/`59` grammar of `38`/`48`, riding the color ladder). Cells carry a
  `Link` that the encoder opens and closes as OSC 8 — both cells of a wide
  grapheme, close before reset/erase, and an endFrame close so no region
  leaks across frames. `Canvas.PutCell` is the public way to write a full
  cell. `WithHyperlinks(true)` opts in because the protocol defines no
  detection mechanism; the default keeps frames byte-identical and a
  link-only change costs zero bytes.
- Synchronized output enabled by the probe: a DECRPM 2026 answer that
  recognizes the mode (set, reset, or permanently set) turns on the
  `\x1b[?2026h`/`\x1b[?2026l` frame wrapper, a rejection (unrecognized, or
  permanently reset, where the wrapper would be a permanent no-op) turns it
  off, and silence leaves it off — an unconfirmed terminal never receives
  the sequence. `LUX_TUI_SYNC_OUTPUT` is now a two-way override
  (`1`/`true`/`yes`/`on` force on, `0`/`false`/`no`/`off` force off, empty
  or `auto` leaves the probe to decide; an invalid value degrades to auto
  after a one-line stderr notice). With negotiation disabled, only a
  forced-on setting claims the wrapper.
- Verification artifacts under `.verification/` at every stage close.

### Changed

- The encoder emits the shortest VT encoding of a frame and lets the terminal
  move rows it already has through scroll regions instead of repainting them.
- Resizes are delivered through a last-wins slot, so a resize storm cannot
  displace the newest size.
- Windows console sizes resolve through the screen buffer instead of the legacy
  console API.
- Compatibility and README documentation now describe the negotiated capability
  behavior instead of the pre-negotiation state.

### Fixed

- Blocked terminal writes are interrupted instead of wedging shutdown, and
  restoration and panic cleanup errors are preserved.
- Presentation callback failures are contained; clipped interaction coordinates
  are preserved; callback cancellation causes survive wrapping.
- Equal-row diff guards and post-scroll re-marks keep an idle screen unchanged
  row by row.

### Performance

- Word navigation no longer allocates.
- The editor window recomputes in a single pass.
