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
- Capability negotiation before the first frame: one bounded
  DA1/DA2/DECRQM probe (150 ms default) that can only veto
  `ScrollRegions` and
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
- Win32-input-mode keyboard (DEC private mode 9001) — the Windows
  counterpart of kitty disambiguation, from Microsoft's improved ConPTY
  keyboard handling: the startup probe asks `CSI ? 9001 $ p`, and a
  recognizing DECRPM answer turns the mode on (status 0/4 rejects it,
  silence keeps the legacy encodings, and kitty wins when both answer, so
  only one enhanced encoding is ever live). Enter sends `CSI ? 9001 h` and
  every exit path leads its restore with `CSI ? 9001 l`, riding the same
  write composition as the kitty stack. Enhanced mode decodes the
  `CSI Vk;Sc;Uc;Kd;Cs;Rc _` records: press, repeat, and release actions
  with console repeat counts; both ctrl keys (RIGHT_CTRL 0x4 and
  LEFT_CTRL 0x8) and both alt keys map to the ctrl/alt bits; caps-lock
  and num-lock state decode; AltGr (LEFT_CTRL|RIGHT_ALT) on a printable
  translation is a plain rune, not Ctrl+Alt; a printable Uc is the event
  rune; Shift+Tab normalizes to BackTab; and modifier keys or other
  unmapped virtual keys surface as unknown-key events with their honest
  action and modifier state — a Ctrl-up bookend reads as a release,
  never a phantom press. The scan code is parsed and ignored;
  SCROLL_LOCK_ON and ENHANCED_KEY are dropped; malformed records (more
  than six parameters, non-numeric fields, a key-down flag outside 0/1,
  a repeat past 0xFFFF) fall through to the unknown-key escape hatch.
  Supported by Windows Terminal, ConPTY's ConHost, and WezTerm
  (default-on since 20220624); mintty and foot implement kitty instead,
  which the probe's silence handles honestly.
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
- Multiplexer provenance for the negotiated capabilities — the probe now
  also sends the secondary device attributes query (`CSI > c`) and latches
  both DA1 and DA2 bodies with first-answerer-wins semantics, so a forged
  second report cannot overwrite the first. DA2's manufacturer code
  identifies who answered: tmux reports 84 — a value no real host claims
  (Windows Terminal 0, xterm 41, VTE 65, screen 83, WezTerm 123) — and
  tmux's DA1 is the VT100 shape `?1;2c` where modern hosts answer VT220-class
  attribute sets. The environment corroborates where reports cannot:
  `$TMUX`, `$STY`, and the propagated `$TERM` (`tmux*`/`screen*`), which
  survives an ssh hop that `$TMUX` itself does not. Any single witness is
  enough, and the verdict it drives is asymmetric by family: output
  protocols (DECOM/DECSTBM, 2026) keep the multiplexer's answers, because
  tmux implements those for its panes itself and the app-to-mux path is the
  one that matters; input protocols (kitty keyboard, pixel mouse, win32
  input) have a "supported" verdict downgraded to unknown, because the bytes
  a pane receives are whatever the multiplexer re-encodes and DCS
  passthrough is output-only — there is no byte-level way to confirm the
  host answered directly. Rejections and silence stand. kitty therefore
  stays off inside a detected tmux or screen until the multiplexer
  implements the protocol natively — a deliberate cost paid back by never
  running an enhanced encoding a pane cannot actually receive; today's tmux
  honestly rejects 1016 and 9001 (3.7 and later) or never answers them (3.6
  and earlier), and drops the kitty query in every version anyway, so the
  gate is defense in depth against the next "helpful" multiplexer, a
  mis-routed passthrough reply, or a pane whose `$TERM` was forced to a
  host name. Screen is environment-detected only: its DA1 is byte-identical
  to tmux's, and its DA2 code 83 was not audited for exclusivity among real
  terminals (the version parameter behind it drifts between builds), so
  `$STY` and a `screen*` `$TERM` are the trusted witnesses there.
- Terminal-width truth: `uniseg` v0.4.7 remains the grapheme segmentation
  engine, but every grapheme's cell width now passes a Unicode 18.0.0
  overlay (range tables derived mechanically from the 18.0.0 UCD
  EastAsianWidth, emoji-data and GraphemeBreakProperty files). Codepoints
  uniseg's 15.0-era data still measures as one cell render at their 18.0.0
  width: the 16.0 East Asian Width promotions (I Ching trigrams and
  hexagrams, Tangut supplement and Monogram blocks, new emoji such as the
  Tomobiki symbol and the harp), and new Extend/Control assignments
  (Tulu-Tigalari marks, Latin Extended-D marks, musical-symbol
  reclassifications) measure zero cells and ride the existing combining-mark
  path. Clusters with no overlay codepoint keep the exact raw `uniseg`
  width, so regional-indicator pairs, Hangul composition, VS15/VS16
  presentation switches and the 3/4-cell dashes are untouched. Segmentation
  itself is not upgraded: UAX #29 conformance is 602/602 against the 15.0.0
  GraphemeBreakTest, and the drift against 18.0.0 (26 failing lines, all new
  GB9c-style Indic conjunct rules uniseg lacks, plus ZWJ sequences built
  from post-15.0 codepoints) is documented in COMPATIBILITY.md.
- Width policy for disagreeing terminals: `lux.WithWidthPolicy(render.
  WidthPolicy{...})` retells the measuring for terminals whose cells differ
  from the 18.0.0 model. `AmbiguousWidth: 2` doubles East Asian Ambiguous
  codepoints (179 ranges / 138,739 codepoints from the 18.0.0
  EastAsianWidth.txt class A — Greek, Cyrillic, box drawing, unit signs,
  enclosed alphanumerics, the private-use planes), matching a CJK-locale
  xterm, mlterm, or legacy conhost; only the exact value 2 activates the
  bump, anything else (including the zero value) behaves as 1.
  `VS16Blind: true` strips one trailing `U+FE0F` before measuring, so
  `❤️`/`⚠️`/`ℹ️` collapse to their single-cell text forms on terminals that
  ignore emoji presentation selectors, while `U+FE0E` text presentation is
  always respected. `NarrowEmoji: true` squeezes emoji-presentation
  graphemes into one cell (80 Emoji_Presentation ranges / 1,228 codepoints
  from 18.0.0 emoji-data.txt: `😀`, `⚽`, flag pairs, ZWJ families, and any
  cluster carrying VS16) for legacy console stacks, never narrowing CJK
  ideographs. The policy is per-application and advisory — no
  auto-detection — and flows coherently from `Run` through every canvas the
  frame owns (`Canvas`/`Subview`/`Clip`), `Canvas.Text` advance, `TextWidth`,
  `Button`/`Modal`/`ListBox` truncation, `TextInput` caret placement and
  mouse mapping, so clicks land on the cells actually painted. The zero
  policy is byte-identical to the previous behavior.
- Unicode core mode (DEC private mode 2027, the contour-led
  terminal-unicode-core proposal, ALPHA): the startup probe now also asks
  `CSI ? 2027 $ p`, and a recognizing DECRPM answer (status 1, 2, or 3 —
  a permanently-reset 4 or an unrecognized 0 rejects, silence leaves it
  off) enters the mode with `CSI ? 2027 h` and resets it with
  `CSI ? 2027 l` first on every exit path, the LIFO opposite of the enter
  order. An enabled terminal clusters printed text into extended grapheme
  clusters per UAX #29 itself; lux's own uniseg-plus-18.0.0-overlay
  measurement already agrees, so no measuring code changes — the mode
  converts the terminal from coincidentally agreeing into being specified
  to agree, and a leaked cluster-aware mode cannot misalign the next
  program on that terminal (the spec leaves the default after DECSTR and
  RIS undefined, and Ultraviolet, the proposal's other consumer, does not
  restore the mode on exit — lux does, unconditionally). The capability
  snapshot exposes the verdict as `capability.Set.UnicodeCoreMode`, and
  `terminal.UnicodeCore(true/false)` overrides the probe verdict both ways
  for a terminal whose report lies. An explicit width policy suppresses
  the mode even on a positive answer (Run passes
  `terminal.UnicodeCore(false)` automatically): one stream follows one
  width authority, and the policy owns it. Inside a detected multiplexer
  the 2027 verdict stands with the other output-family answers, unlike
  the input-family verdicts the gate downgrades.
- Word wrap: `render.WrapLines`/`WrapLinesWith` wrap text to a cell limit
  in word mode (the zero value) or char mode, `MeasureWrap`/
  `MeasureWrapWith` return the resulting `TextMetrics` without drawing,
  and `Canvas.TextWrap(rect, text, opts, style)` wraps to the rect's
  width under the canvas's own width policy, draws the rows that fit,
  and reports the full height, so `Height > rect.H` is visible clipped
  overflow. Every grapheme is measured through the same policy-aware
  terminal-width authority as `Canvas.Text` and is never split: a wide
  grapheme that would straddle the limit moves down and leaves the last
  cell of its line empty, and a one-cell limit under wide content
  overflows by exactly that wide width, pinned in tests. Hard breaks
  (`\n`, `\r\n`, a lone `\r`) always split; whitespace at a soft break is
  consumed, while leading indentation is content that overflows as its
  own line when wider than the limit; tabs keep their four-column stops
  relative to the wrapped line's start; a trailing hard newline yields a
  final empty line the way an editor counts the caret's line. Control
  graphemes occupy no cells and stay in the output — exactly what
  `Canvas.Text` draws — so a wrap can never delete a grapheme and let
  neighboring ZWJ clusters merge into a different measurement (found by
  fuzzing, pinned by a corpus seed that fails without the fix).
  Hyphenation stays off by design: a soft hyphen is a zero-width
  codepoint terminals treat differently, and breaking on it would measure
  one line with two width authorities. The wrap is a single linear pass
  with one re-scan only for unbreakable runs, and the fuzz target holds
  the invariants: no panic, metrics agree with the cells the canvas
  actually draws, and the output graphemes are an in-order subsequence of
  the input that drops only whitespace consumed at soft breaks and hard
  breaks.
- Multiline editor (`ui.TextArea`): logical lines with the same EGC contract
  as `TextInput` — caret and selection stay on grapheme boundaries, wide
  cells and combining marks are never split. Soft wrap in word or character
  mode (`Wrap render.WrapMode`), a vertical viewport with wheel and key
  scrolling (`Scroll`), selection painting, word/line/paragraph navigation
  and edits, bounded undo/redo (`UndoBytes`), paste sanitization and
  `MaxBytes` bound, `Copy`/`Cut`/`Paste` through the clipboard port, and a
  dirty-range layout cache so a keystroke re-wraps only the edited lines.
  `examples/textedit` demonstrates the widget end to end. Not claimed:
  per-row incremental rewrap of a single huge line, hard tabs beyond the
  wrap's four-column stops, RTL visual reordering, IME preedit.
- `ui.Table`: virtualized tabular data with fixed-width and rubber columns
  (`Width` fixed, `Weight` shares the leftover), a header row with
  click-to-sort reporting (the app re-slices its own data; the widget never
  mutates the source), keyboard and wheel navigation, row selection with
  viewport-follow, wheel-decoupled viewport scrolling, EGC-correct cell
  clipping with a policy-aware ellipsis, and a zero-value ready
  `TableState`. `rowAt` supplies only the visible window, so a million-row
  table paints 39 rows. `examples/table` demonstrates the full pattern with
  200k rows, numeric and lexical sorts, and selection. `examples/widgets` is
  the stage-8 widget catalog whose Table slice is live end to end (arrow
  keys, body-click selection, Escape quits), and the showcase's Interaction
  Lab embeds a second live Table in its scrolling panel while a godoc
  `ExampleContext_Table` pins the header/body/cell layout. Not claimed:
  horizontal scrolling, row height above one cell, per-cell content
  styling, empty-table header sorting (the router drops below-content
  clicks), IME preedit.
- `ui.Checkbox`: zero-value `CheckboxState`, toggles on Enter/Space or a
  completed click (press focuses, release activates), ASCII `[x]`/`[ ]`
  markers restyleable, honest `changed` reporting; key releases are inert.
- `ui.RadioGroup`: zero-value `RadioState` over an app-owned label slice;
  arrows, Home/End, PageUp/PageDown (half the group), wheel steps, and row
  presses all move the choice — a radio selects on navigation, release
  reports `activated` while keeping the choice. Not claimed: vertical key
  wraparound, two-column layout.
- `ui.Select`: zero-value `SelectState` dropdown over an app-owned option
  slice. The closed box (value plus `▾` marker, clipped with `…`) opens on
  activation or any navigation key; the open popup is intentionally
  borderless so its drawn cells and press rows are the same rectangle, with
  popup row 0 the inert value anchor, `1..n` guarded to `Offset + Y - 1`.
  Opening reports `changed`, a committed pick reports `activated`, wheel
  scrolls the popup viewport without moving the choice (viewportOnly
  decoupling), and the open region is `ContentHeight: 1+rows` so presses past
  the options are dropped by the router. Registration order is the popup's
  z-order: register the select after its neighbours so the grown region wins
  pointer hits. `examples/widgets` shows all three controls end to end
  (checkbox toggle, radio pick, select open/pick/commit with Escape quit),
  the showcase's Interaction Lab drives a live form (checkbox halves the spin,
  radio recolors the scene border, select retunes rotation speed) below the
  fold of its scrolling panel, and godoc `ExampleContext_Checkbox`,
  `ExampleContext_RadioGroup`, `ExampleContext_Select` pin the painted
  output. Not claimed: fuzzy filtering, auto-flip above the anchor when the
  popup would clip the screen bottom, multi-select, app-side click-outside
  close, app-side Escape close (plain Escape never routes to regions).
- `ui.Tabs`: zero-value `TabsState` strip over an app-owned label slice.
  Each tab is its label plus one padding cell per side, and the same layout
  math drives both drawing and pointer hits, so a press on slot x selects
  that tab directly. Navigation selects like a radio group: Left/Right,
  Home/End, and wheel steps move the choice, and the closing release
  reports `activated` while keeping it — a strip has nothing to commit. The
  strip virtualizes: `Offset` is the first drawn tab, and `keepTabVisible`
  advances it minimally so the selected tab's whole label stays readable (a
  tab wider than the strip clips from the left edge instead of vanishing).
  Vertical
  and page keys are inert, key releases are inert. `examples/widgets` (page
  tabs over a four-page control panel: inventory, form, motion, tree) and
  the showcase's Interaction Lab embed
  live strips end to end — the showcase one nested inside the panel's
  vertical scroll viewport, which is the case the horizontal-arrow routing
  fix below exists for. Not claimed: multi-row strips, underline accent
  rows, overflow markers, drag reordering, per-tab hover.
- `ui.Gauge`: display-only determinate progress. `GaugeState{Value, Max}`
  clamps Max to at least 1 and `Fraction` to [0, 1]; the bar is full block
  cells, a half-block cell when the remainder is at least half a cell, then
  light-shade track to the end, repeated on every row of a taller bounds.
  No region, no routing, no focus: "seek" would need its own press contract
  and is not claimed.
- `ui.Spinner`: display-only spinning glyph driven by absolute time.
  `SpinnerState{Started}` plus `Age(now)` pick the frame — the phase is a
  pure function of wall clock, so 10 FPS and 240 FPS presenters draw the
  same glyph at the same instant. The app passes `now`, which makes drawing
  deterministic for tests and replay. Presets: `SpinnerASCII` (`|/-\`),
  `SpinnerBraille` (ten-phase), `SpinnerArc` (four-phase disc). A zero
  `Started` paints the resting first frame. No region, no routing, no
  focus, and frame scheduling stays the runtime pacer's job. Not claimed:
  multi-cell frames, an indeterminate marquee.
- `ui.Sparkline`: display-only trend line over an app-owned float series.
  `SparklineState{Values, Min, Max}` paints the newest values right-aligned
  (a window, so the series can be unbounded) as eight-level block glyphs
  filling each column bottom-up; pin `Min`/`Max` for a stable axis across
  frames, or leave both zero for per-frame auto-range. A degenerate range
  (flat series or `Min >= Max`) collapses to the mid glyph, NaN clamps to
  the floor, and empty or blank bounds draw nothing. No region, no
  routing, no focus. Not claimed: thresholds, labels, axis drawing,
  down-sampling beyond the window, negative bounds painting below a
  baseline.
- `ui.Tree`: list-style hierarchy widget over an app-flattened row source.
  The app owns the expansion set (`TreeState.Expanded` by row ID) and
  yields one `TreeRow{ID, Label, Depth}` per visible node through `rowAt`,
  so a million-node tree virtualizes exactly like a table. Navigation is
  the list contract — arrows, Home/End, pages, wheel move the selection —
  and activation (Enter, Space, or a completed click on the row) toggles
  the row's `Expanded` entry: `activated` is reported always, `changed`
  only when the entry actually flips, and a leaf row reports activation
  without claiming a change. A collapsed entry stays in the map as false,
  and a zero `Expanded` map allocates on first toggle. Drawing pools
  two-space indents, draws `style.Collapsed`/`Expanded` markers (`+`/`-`
  defaults) for rows with children, and clips the label to the bounds. Not
  claimed: inline node filtering, per-node styling, drag-and-drop
  reordering, lazy async children, horizontal tree scrolling.
- `docs/CUSTOM-CONTROL.md`: the "your own control in an evening" recipe —
  state, style, `Apply`, region registration, and the two wiring lines —
  walked through a stepper `[− 42 +]` whose exact code is compiled and
  tested as `ui/stepper_recipe_test.go`, so the document cannot drift from
  the truth it teaches. Includes the testing ladder (Apply table, paint,
  routing rungs), the free-vs-owned split the framework hands every
  control, and the pitfalls list (duplicate-ID panics, press-as-Scroll on
  Scrollable-only regions, release-commit timing for Activatable ones).
- First-frame caret: the first focusable control now paints its caret on
  the very first frame it registers in, because a static app may never
  repaint afterwards. Focus is normally committed only when a frame is
  presented, so before this a static app's first paint showed no caret
  until the first edit.
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
- Pasting an empty string (or newline-only text) into a `ui.TextArea` with an
  active selection replaced the selection with nothing but left the deleted
  text behind — an empty replacement now clears the selection like any other
  paste does.
- Horizontal arrows now route to the focused region: `KeyLeft`/`KeyRight`
  were missing from the router's scroll-key set, so a focused horizontal
  widget (a tab strip, later a horizontal list) dropped them on the floor
  instead of scrolling, and the unhandled key never bubbled anywhere. They
  join `scrollKey` while the vertical clamp test (`scrollKeyUp`) stays
  up-only, so horizontal arrows never bubble through an enclosing vertical
  viewport — the focused region always receives them as Scroll actions. A
  consequence: non-priority `ScopeApp` shortcuts bound to bare Left/Right
  are now shadowed by any focused scrollable region (no shipped example
  binds one).
- `ui.Tabs`' `keepTabVisible` measured visibility against the clipped slot,
  and a slot truncated to one cell by the strip's right edge always
  "fit": selecting a tab whose label did not fully fit never advanced the
  offset, so the selected label could sit clipped to nothing while the
  strip scrolled to it. Visibility is now measured against the whole label
  width — an advance happens whenever the selected label would not fit
  fully — and the scan sums label widths directly instead of building a
  layout per step, which also drops one allocation and about 16% of
  `BenchmarkTabsDraw`.

### Performance

- Word navigation no longer allocates.
- The editor window recomputes in a single pass.
- Boundary probes test the bytes at the caret first: an ASCII neighborhood
  answers in constant time instead of walking the grapheme scanner, which
  turns a 128 KiB single-line edit from ~142 ms per keystroke into ~0.72 ms.
- `ui.TextArea` holds its wrap layout as a dirty-range cache, so a keystroke
  in a 400-line document re-wraps one line (~78 us, 7 allocs) instead of the
  whole document.
- `ui.Sparkline` computes the value range once per frame, not per column:
  a 60×3 sparkline over 256 samples draws in ~15 µs with 1 alloc (the
  glyph table is an eight-string pool, so each cell is a slice, not a
  rune conversion).
- `ui.Tree` pools its indents (one two-space string per depth) and clips
  labels in place: 40 visible rows draw in ~197 µs with 11 allocs, with
  the row source consulted exactly once per visible row.
- `ui.Tabs`' `keepTabVisible` rewrite (see Fixed) also drops
  `BenchmarkTabsDraw` from ~98 µs / 3 allocs to ~82 µs / 2 allocs.
