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
- Kitty keyboard protocol (disambiguate flag): the startup probe asks
  `CSI ? u` and waits a 25 ms grace behind the classic reports; a flags reply
  is the one enable-only verdict and turns `KeyboardEnhanced` on. The push
  (`CSI > 1 u`) rides the enter write and the pop (`CSI < u`) leads the
  restore in the same write on every exit path, including panics funneled
  through `Run`'s deferred close. Enhanced mode decodes kitty `CSI u` keys:
  Esc is immediate (no escape delay), Ctrl+letter splits from its C0 alias,
  multi-modifier combos arrive, Shift+Tab normalizes to BackTab, repeat
  events coalesce, release events and private-use functional codes surface
  as unknown keys, and lock-state bits are dropped until the event model
  grows them. Silence keeps the legacy key path untouched.
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
