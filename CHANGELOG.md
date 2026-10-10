# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Versioning policy for v0.x

While the major version is 0, **minor releases may break**: a `0.x.0` minor
bump can change or remove API, and the changelog's Changed/Removed sections
list every break. Patch releases are strictly compatible — fixes and
performance only. From `v1.0.0` the project switches to conventional
semver: breaking changes land only in a new major.

## [Unreleased]

### Added

- Application-result seam (`event.Message` and `event.Event.Data`): a
  Command can now report application-level results back to
  `Model.Update` as an `event.Message` event whose `Data` payload is
  fully owned by the application — LUX only serializes delivery, the
  payload's type is opaque to the framework, and the field stays nil for
  every other kind. Unlike `event.Error`, a Message never terminates the
  run: recoverable failures (a failed load, a rejected request) travel
  as payloads for the UI to render, while fatal transport errors remain
  `event.Error` and end the run unchanged. Two tests pin the contract:
  a Message carrying a failed-list payload leaves the run alive and
  still serving input, an Error still terminates it.

- Process explorer reference app (`examples/09-procs`): the stage-2
  vertical for the async pattern on real data. A `processSource` seam
  isolates the OS — `tasklist` on Windows, `/proc` on Linux, `ps` on
  macOS — behind one context-aware `List(ctx, filter)` call that owns
  filtering; the Command goroutine captures everything by value and
  never touches the model, results return as `event.Message` payloads
  with a request identity so superseded answers are ignored, and at
  most one load runs at a time with keystrokes coalesced into a single
  deferred replay. The app renders loading, data, empty, and error
  phases honestly, retries on F5, cancels the in-flight load on quit,
  and pairs a virtualized table with a master-detail pane. The test
  suite drives the full journey end to end through a scripted source,
  feeds stale and foreign results directly to the model, proves quit
  unblocks without waiting on the worker, and smoke-tests the real
  OS adapters on every CI lane.

- Runtime discipline guards (`lux`, `internal/terminal`, `render`): the
  framework's steady-state discipline is now enforced by failing tests
  instead of benchmark observations. `TestRunReturnsToTheGoroutineBaseline`
  runs five complete lifecycles — interactive quit, present failure,
  cancellation inside a blocked write, model panic, and the plain
  non-interactive path — after a cancellation-based warm-up, and fails
  with a full goroutine stack dump if any framework goroutine outlives
  `Run`; the fake backend starts none of its own, so the guard attributes
  leaks to the framework alone. Four allocation guards pin the
  zero-allocation floors already measured by the benchmarks: frame diff
  encoding with a reused scratch buffer, the idle `Present` early exit,
  a full canvas repaint with mixed Latin/CJK/emoji content, and the
  unchanged `AppendDiff` sweep — `testing.AllocsPerRun` budgets of zero,
  verified to hold both with and without `-race`, so they gate the exact
  lanes the CI matrix runs.

- Scheduled verification workflow (`.github/workflows/scheduled.yml`,
  weekly Monday 03:30 UTC plus manual `workflow_dispatch`): single-pass
  CI verdicts miss interleavings that only repetition exposes, so the
  slow lanes now re-roll weekly. Four long-fuzz legs run the parser,
  TextInput, TextArea, and wrap targets for 10 minutes each
  (`-run '^$' -fuzz` — one target per test binary), uploading the full
  output and the minimized `testdata` reproducer as an artifact on
  failure. A terminal-lanes job runs the PTY e2e suite and the SSH
  example with `-race -count=5` on ubuntu and macOS. An interactive
  stress job runs the example suites with `-race -count=25` on ubuntu —
  the convergence-flake lane — with the same mirror-to-log plus
  artifact evidence handling as the push matrix, so a hang fires the
  goroutine dump into preserved output. The lane-to-evidence mapping is
  documented in `docs/COMPATIBILITY.md` under "Scheduled lanes".

- Race-instrumented e2e child (`e2e`): when the PTY suite itself runs under
  `go test -race`, `TestMain` now builds `childapp` with `-race` too
  (a build-tag const, `childRaceFlag`, mirrors the suite's own
  instrumentation). The application under the pty — keystrokes, resizes,
  paste, suspend/resume — is race-checked for real, not just the test code
  that drives it; a detected race makes the child exit 66 after printing
  the report to the pty, where the drain snapshots and wait-status
  assertions turn it into a RED failure instead of silence. Plain local
  runs without `-race` build the child without the flag and keep needing
  no C toolchain.

- CI failure evidence preservation (`.github/workflows/ci.yml`): the test
  matrix and SSH-example jobs now mirror the complete `test-output.txt`
  into the job log and upload it as an artifact whenever `go test` fails.
  The redirected output previously existed only as grep'd annotation
  lines (head -8, no dump frames), so a goroutine dump captured by the
  example tests' hang instrumentation was discarded with the runner. Now
  `gh run view --log-failed` and the artifact carry the full dump, which
  is the evidence the remaining CI-only convergence flake needs.

- Unix termination contract (`internal/terminal`, `lux`): `SIGINT` and
  `SIGTERM` are now trapped by the runtime's lifecycle watcher, which owns
  the cleanup half of the contract — the terminal is restored immediately
  (the same leave sequences `Close` writes, exactly once, idempotently
  latched) — and `Run` then returns `lux.ErrInterrupt` / `lux.ErrTerminated`
  through its ordinary error unwind, so a caller can map them to the
  conventional exit statuses 130/143 with `errors.Is`. The application owns
  the exit code; the model never sees the event. A Ctrl+C typed at the
  terminal is deliberately not this path: raw mode claims the byte and the
  model sees a Key event, which keeps interactive quit in the model's
  hands. `SIGHUP` stays terminal-loss-only (no restore attempt), `SIGTSTP`
  is untouched, and `SIGKILL` is promised nothing. The dispatcher loop was
  split out of the `signal.Notify` wrapper into `dispatchLifecycle` so the
  dispatch logic is unit-testable without racing a real signal against the
  registration; the pty e2e suite proves the whole chain against a real
  `kill -TERM` / `kill -INT`: the wait status must be a clean exit (an
  untrapped death is killed-by-signal), with exactly one leave set and the
  title pop as the final bytes.

- CI lanes for the nested Go modules (`.github/workflows/ci.yml`): the
  PTY end-to-end suite (`e2e/`) and the SSH example (`examples/07-ssh`)
  live behind their own `go.mod` files, so the root `go test ./...` job
  never compiled or ran them. Two new jobs run vet, build, and `go test
  -race` for both modules on `ubuntu-latest` and `macos-latest`, where the
  pty-based lanes are tagged to build; the SSH example job also builds the
  server binary. The `benchmarks/` module stays out for now: it requires
  Go 1.26, newer than the CI toolchain.

- Tour correctness suite (`examples/08-tour`): the guided tour now drives
  frame requests from `wantsFrame` — static tabs settle to at most two idle
  frames, the motion demos retarget with live position/velocity sampled at
  the press instant, page-local Space shortcuts stay reachable while the
  tab strip owns focus, and ReducedMotion freezes decoration while the
  determinate progress sideband (work state, not decoration) keeps
  ticking. Terminal sidebands (OSC 9;4 / OSC 9;777 / OSC 52) are issued
  from View on the presented frame, and the paste feed caps its preview
  instead of dumping whole pastes.

- Non-BMP input fixtures (`internal/terminal`): the parser now has pinned
  coverage for the UTF-8 shapes a non-BMP rune can arrive in — whole
  four-byte and three-byte runes, the same runes split across feed
  boundaries (held back until complete, never decoded as garbage), and a
  truncated rune at end of stream (replacement Text per undecodable
  byte). A measured Windows fixture backs the transport half of the
  story: injecting U+1D11E as its two surrogate KEY_EVENT halves through
  `WriteConsoleInputW` and reading the console back delivers exactly the
  combined UTF-8 bytes `f0 9d 84 9e`, in legacy raw mode and under
  `ENABLE_VIRTUAL_TERMINAL_INPUT` alike — the console driver pairs the
  halves, so the parser's byte stream never carries raw UTF-16. A
  dangling high surrogate delivers nothing (the console holds it pending
  until a low surrogate completes the pair), which is console behavior,
  not framework behavior, and is documented rather than worked around.

### Fixed

- Blind and uncancellable pipe reads on Windows (`internal/terminal`):
  `readInput` waited on the input handle with `WaitForSingleObject`
  before every read, but that wait is only contractual for console
  input handles. Go's `os.Pipe` creates synchronous pipe handles whose
  signaling is kernel-flavor dependent: on client Windows they never
  signal (bytes written to a pipe stdin never reached the backend —
  the probe, the multiplexer reply path, and replayed prefills were
  blind), while on Windows Server the handle reads as signaled when
  idle, so the subsequent `Read` parked on an empty pipe and no context
  could cancel it — the Windows CI lanes died in
  `TestReadLoopReplaysLargePrefillWithoutDeadlock` on both Go versions,
  twice, deterministically. `readInput` now branches once per call:
  console input keeps the exact wait-then-read it always had (sliced
  into 100ms waits so cancellation is noticed inside a slice), while
  pipes poll `PeekNamedPipe` for queued bytes and read only when the
  kernel guarantees data — `ERROR_BROKEN_PIPE` maps to `io.EOF` (a
  closed write end now ends the read loop), `ERROR_INVALID_FUNCTION`
  (a regular file) falls through to a direct read, and deadlines are
  honored by sleeping in 10ms slices. Five pipe-contract tests pin the
  behavior, and the negotiate, multiplexer, and read-loop-done tests
  that Windows used to skip (with exactly this wait limitation) now
  run and pass there too. A second defect fell out of the fix: a
  caller's one-shot wait spanning the whole timeout (probe timeouts,
  read waits with large values) ignored context cancellation for the
  full window — cancellation is now noticed within one wait slice.

- Data race in the SSH example pty bridge (`examples/07-ssh`): the resize
  loop called `pty.Setsize` on the master pty whenever a winch arrived,
  with nothing ever stopping it, so an in-flight `TIOCSWINSZ` ioctl could
  race the master's `os.File.Close` on the serve exit path (the ioctl and
  the close touch the same fd internals). `go test -race` flagged this on
  both Linux and macOS CI since the module got its own lanes, killing
  `TestServePtySizeTermAndExitStatus`, `TestServeResizeDrivesNewPtySize`,
  and `TestServeDisconnectKillsChild`. serve now quiesces the resize loop
  — close a stop channel once (`sync.Once`; both the disconnect killer and
  the normal exit path can trigger it) and wait for the loop to return —
  before any `ptmx.Close()`. The same change retires a second defect: the
  winch goroutine used to leak past the session forever because nothing
  closed the loop; it now terminates with serve.

- Chronic CI failures: the `Module and formatting` job failed because one
  `examples/08-tour` test file was never gofmt-formatted, and the
  `golangci-lint` job failed on nine standing findings — three embedded-field
  selectors, two redundant boolean shapes, two untagged switches, a
  side-effecting `||` test condition rewritten as explicit asserts, and one
  unused color variable. All are fixed with no behavior change; the lint,
  format, vet, and full root test suite now pass locally exactly as the CI
  jobs run them.

- Wheel-flake in the showcase end-to-end tests (`examples/06-showcase`):
  the form and display-widget tests fired three wheel notches at a fixed
  screen coordinate, but the first notch scrolls the panel nine rows and
  moves the nested virtual log under that coordinate — once a frame
  commits in between (legal: the runtime routes events against the last
  presented frame's hit screen, and the run-loop select may pick the
  timer before the queued wheels), the remaining notches scrolled the
  four-row log instead of the panel and the test hung to its timeout.
  The tests now re-aim each notch at a gap doc row no nested scrollable
  covers at any offset (and that stays bare even when the hit screen is
  one step stale), wait for each step's state to commit, and
  `waitStateWhere` reports the observed state count and last snapshot on
  timeout instead of a bare "matching state timeout". No library behavior
  changed.

- Width policy in the scene3d monochrome ramp (`scene3d`): the luminance
  ramp filtered its glyphs with the default width policy while painting
  through the canvas's, so on `AmbiguousWidth: 2` terminals a custom ramp
  built from ambiguous glyphs (°, ±, Greek) kept glyphs that render two
  cells wide there — each painted cell smeared into its neighbor and skewed
  the scene. The filter now measures with the canvas policy and the ramp
  cache is keyed by policy too. The default ASCII ramp is unchanged.

- Width policy in Tabs and Checkbox measurement (`ui`): two layout paths
  bypassed the canvas width policy — tab strips sized, hit-tested, and
  scrolled their labels with the default policy while clipping the drawn
  labels with the canvas's, so on `AmbiguousWidth: 2` terminals (CJK-locale
  xterm and friends) ambiguous labels (Greek, Cyrillic, box drawing) drew
  truncated and a click landed one tab off where the labels rendered; and a
  Checkbox with a custom marker measured the marker with the default policy
  while drawing it with the canvas's, letting an ambiguous marker push its
  label onto the marker's tail cell. `TabsState` now records the drawing
  policy (unexported, zero value = default) and every tabs measurement —
  slot layout, press mapping, visibility — runs through it; the checkbox
  label column follows the marker's canvas-policy width. Default-policy
  terminals (all ASCII labels and markers) draw byte-for-byte as before.

- Modal caret takeover (`ui`, `render`): while a modal scope was open, the
  editor beneath the dialog still painted the terminal cursor — the base
  repaints with its regained focus before `BeginModal` runs, so a focused
  `TextInput`/`TextArea` left a blinking hardware caret under the overlay
  even though its input was routed to the dialog alone. `BeginModal` now
  clears the frame's caret as its scope begins (new `Canvas.HideCaret`),
  so the covered editor stays caret-less until a control drawn inside the
  dialog sets its own; on dismiss the editor's caret returns on the first
  frame without the dialog, with focus restore unchanged.

- Table wheel-scroll continuity (`ui`): after a wheel notch the table viewport
  now stays where the wheel left it instead of snapping back to the selected
  row on the next frame. `TableState` gains a `FreeScroll` flag: wheel input
  sets it (viewport clamps only to the row range), the first selection-driven
  action — key press, click, or programmatic selection — clears it and
  `keepRowVisible` resumes. The flag defaults to false, so programmatic
  deep-selection snapping and wheel-at-boundary bubbling are unchanged.

- Table header and last-row press routing (`ui`): a press on the header row —
  or on the last visible body row — was dropped by the router because the
  table's region content height excluded the header, so header-click sorting
  and row-click selection were dead on arrival. The region now reports
  `header + visible body rows` as its content height, which makes the header
  pressable even when the table is empty (sorting an empty view is a no-op
  that still flips the indicator) and keeps the blank tail below the last row
  non-pressable, matching the List contract.

- Tour feature-table sorting (`examples/08-tour`): the widgets page fed the
  table actions but ignored the sort request, so clicking the CAPABILITY
  header flipped the state (indicator, sort direction) while the rows stayed
  in place. The tour now owns a copy of the feature rows, re-orders them
  with a stable sort when the widget reports `SortRequested`, and clears the
  flag once the request is served; the 04-table demo clears the flag the same
  way so both examples match the documented `TableState` contract.

- Compact layout of the widgets tour page (`examples/08-tour`): at 80×24 the
  fixed two-column layout cut off the checkbox, radio group, table, and
  virtual log because panel heights never scrolled and controls below the
  fold stopped registering. The page now switches to a stacked full-width
  column inside one page viewport (`wscroll`) whenever the terminal is
  compact or shorter than the natural display panel. Controls draw against
  their virtual rects so the canvas clips what is out of view while the
  focus system keeps every control reachable by wheel, Tab, and arrow keys;
  the footer row survives the scroll, and the nested virtual log composes
  the page offset so its rows stay glued to the content when its top slides
  above the viewport.

- Benchmark harness measurement honesty: lane paint/draw counters are now
  zeroed right after warmup (`adapter.resetCounts()`), so they cover the
  same measured phase as the wire bytes and CPU/allocation snapshots
  instead of silently including warmup traffic; and the settle waits are
  causal (`masterReader.waitQuietSince`): silence only proves an action
  finished when output from *after* that action was observed first. Drive
  units that produce no output at all are counted in a new
  `no_output` result field and surfaced in the report notes instead of
  entering the latency distribution as a fabricated 0 ms. Unit tests in
  `benchmarks/calibrate_test.go` pin both behaviors without a pty, and
  `benchmarks/results` was regenerated with the recalibrated counters.

- Hardened the OSC 8 output seam: a `render.Link` whose URI or ID carries
  control characters, malformed UTF-8, id field separators, or fields beyond
  the size caps now degrades to plain text at the encoder instead of
  traveling inside the control string, where a spliced string terminator
  could smuggle terminal commands. Glyphs and styles survive, a hostile
  link replacing a valid one closes the region, and legitimate Unicode
  destinations ride unchanged.

- CI convergence flake in the example end-to-end suites (tests): three CI
  failures — run #26 and run #32 in `04-table` (the second at 45.04s, on
  the already-widened guard) plus the scheduled stress lane in
  `05-widgets` — shared one goroutine-dump shape: the test parked in
  `waitStateWhere` right after its first keystroke, the run loop parked
  idle in its select, and the events channel was empty, meaning both
  keystrokes had been consumed and dropped. Run #26 was first misread as
  runner starvation and the guards were widened to 45s — which stood and
  later paid off when run #32 and the scheduled lane both captured full
  dumps through the evidence-preservation work above. Reading those
  dumps against the code proved the real cause: a synchronization hole in
  the tests, not a runner fault and not a framework bug. `ui.Context`
  routes terminal input against the last successfully presented snapshot
  — the writer runs a frame's focus commits only after `Present` returns
  — while the example tests observed the initial state from inside the
  first `View`, which reports before the first present is even offered.
  A keystroke sent in that window is routed against an empty focus table
  and is dropped silently: the model never sees it, nothing repaints, and
  the state wait hangs to its timeout. Only the first input after launch
  races this way — once a frame commits, geometry and focus are stable —
  which is why the same suites pass most pushes and later keystrokes
  never flake. The suites now synchronize before their first real input:
  `05-widgets` settles (its spinner keeps painting, and a later frame
  proves the earlier commit ran), while the quiet `03-textedit` and
  `04-table` suites inject an inert F5 press — bound nowhere, but every
  event dirties the model, so the forced repaint proves the first commit
  ran — and wait for the resulting frame.
  `TestKeystrokeBeforeFirstCommitIsDropped` (05-widgets) pins the drop
  itself deterministically with a gated backend that parks the writer
  inside its first `Present`: the pre-commit keystroke is consumed and
  must not move the selection after the gate opens, while a second
  post-commit keystroke must. The CI failure-annotation steps (root
  matrix and 07-ssh) additionally mirror the failing tests' own
  `t.Fatal`/`t.Errorf` message lines (the indented `file:line:` output),
  so a failure names the exact wait that fired, not just the FAIL
  summary. The duplicate `### Fixed` heading the previous slice
  introduced is merged back into one section.

- Double restore after a trapped termination signal (`internal/terminal`):
  `Close` wrote its leave sequences without consulting the suspend latch,
  so the exit path that follows the lifecycle watcher's restore (SIGINT,
  SIGTERM, and the Windows console-close events delivered as SIGTERM)
  restored the terminal a second time — the kitty and title stacks popped
  twice and the alternate-screen leave was repainted over a terminal the
  watcher had already cooked. The pty e2e lanes caught it on both
  platforms (the exactly-one leave assertion is the contract's uniqueness
  proof); the unit tests had asserted the watcher's restore but never
  called `Close` on top of it. The latch is now `Close`'s restore receipt:
  its own leave write is skipped, while the writer, termios, and VT
  teardown still run. The regression is pinned from three angles: the
  suspend unit tests' Close interplay (which had pinned the double-pop
  as "accepted") now demands exactly one leave, the `!windows`
  lifecycle tests close the backend after the dispatch before counting,
  and the e2e lanes assert exactly one leave set on a real pty.

_Unreleased notes: the v0.1.0 cut below is the first tagged release; per the
v0.x policy above, the next `0.2.0` may break API — watch this file._

## [0.1.0] - 2026-10-10

First tagged release. Module path: `github.com/GofMan5/lux` (the planned
`lux-tui` org was not created — organization creation is web-UI-only and
the release pipeline runs headless; see the repository README).

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
- `ui.Theme` and `Context.UseTheme`: one palette across the widget set —
  four roles (Text, Muted, Accent, Danger) resolved per Context, with no
  global mutable style table anywhere (`DefaultTheme()` is a function, so
  a palette value is passed, never mutated in place). The resolution rule
  is uniform: a zero-value style field reads its role, an explicit style
  always wins, a zero role falls back to the widget's own default.
  `DefaultTheme` is attributes-only (dim structure, bold accents,
  reverse-video disabled), so the built-in look survives monochrome
  terminals and redirected output; color themes are opt-in values. All
  nine styled widgets resolve through it: table body/headers/selected,
  tab selected/unselected, gauge fill/track, spinner, sparkline, select
  popup/options, checkbox (marker reads Accent, label reads Text), radio
  rows, tree rows. Default visuals therefore change where zero styles were
  painted: selected rows and tabs, gauge fills, spinners, sparklines,
  radio choices, tree selections, and checkbox markers render bold or dim
  where they previously rendered plain; headers render dim+underline.
  Explicitly styled widgets render exactly as before. Not claimed: a
  global mutable theme, per-instance theming inside one Context, theme
  files or runtime hot-swap, and any widget-specific role beyond the four.
- `lux.WithTitle` and `terminal.Title`: the terminal window title, written
  once through OSC 0 bracketed by the XTWINOPS title stack — the push
  (`CSI 22;0 t`) rides the enter write and the pop (`CSI 23;0 t`) the
  leave on every exit path, so the previous title returns exactly on
  stack-supporting terminals (xterm, kitty, VTE-family, tmux) and the
  syscall count is unchanged. Control runes in the title (C0, DEL, C1)
  are stripped before any write: a BEL or ESC would end the OSC string
  early and leak the remainder as commands. Plain and redirected output
  never writes title bytes, and an empty title opts out entirely. The
  enter/leave assembly for every latch is now built by one table
  (`sequences`) shared by Open and Close, so the restore a terminal
  receives is guaranteed to be the inverse of the enter it received.
  Not claimed: runtime retitling, per-frame titles, the icon-only OSC 1
  form, a capability probe for the title stack, or title support in
  custom adapters (the public `Backend` interface is unchanged).
- Job control (suspend/resume): `SIGTSTP` suspends the Unix backend by
  writing the same leave `sequences()` Close would write (modes, kitty
  pop, title pop), restoring cooked mode before re-raising the stop with
  the shell's disposition; `SIGCONT` rewrites the enter, drops the diff
  baseline, and the next frame is a full repaint — anything may have been
  printed while the process was stopped. Present during suspension is a
  clean no-op; the double stop is latched out; a plain, closed, or
  input-less backend refuses; a failing raw-mode resume keeps the
  suspension held until a later resume recovers. The resume restores the
  Open-time cooked state (the MakeRaw-returned state is discarded), so
  the final Close restores exactly what Open found. `event.Suspend` and
  `event.Resume` ride the ordinary update path and dirty the frame, so
  models pause animations and repaint without app-side wiring — models
  written before the kinds existed treat them as ordinary updates.
  `SIGHUP` is delivered as an Error event and exits through the normal
  restoration path. Windows: no job control — CTRL_CLOSE/LOGOFF/SHUTDOWN
  surface as SIGTERM, the console is released immediately, and an Error
  event is reported before the OS terminates the process (best-effort
  cleanup). Not claimed: `SIGTTOU` background-write stops, or a Windows
  resume path.
- `lux.WithSuspend(enabled bool)` opts the whole job-control behavior out
  (default on). With it off, the run loop never claims the Ctrl+Z byte —
  raw-mode ISIG stays disabled, so the byte arrives as an ordinary key
  event a custom binding can own, and the `SIGTSTP`/`SIGCONT` handshake is
  never entered.
- `event.Kind.String()` names every event kind (lowercase: "key",
  "suspend", "resume", "frame", …), so logs and test failures read as
  words instead of integers.
- A pseudo-terminal end-to-end suite (`e2e/`, a separate Go module
  restricted to `linux` and `darwin`) pins job control against the real
  operating system instead of fakes: a scripted child runs under `lux.Run`
  inside a `creack/pty` terminal with an interactive busybox shell as the
  session leader. The suite proves the shell's own `Stopped` line — which
  only a waitpid on a genuinely stopped job produces — after the raw 0x1A
  byte is typed; that `fg` resumes it with the Suspend/Resume counts, a
  full repaint, and the title re-pushed; that a second `SIGTSTP` raised
  straight at the stopped job is discarded by `fg`'s SIGCONT (SIGCONT
  flushes pending stop signals, so the double stop is safe); and that a
  keystrokes-racing-the-probe replay survives a real kernel input buffer.
  The direct-lane test pins byte-level exit truth: exactly one leave
  sequence, the title pop as the final bytes, and the cursor shown.
- The editor undo family gains a pure alt alias: alt+u undoes in
  `ui.TextInput` and `ui.TextArea` — for terminals where the runtime's
  suspend handshake claims the ctrl+z chord, undo keeps a modifier
  combination that job control can never eat. The alias must be pure alt:
  ctrl+alt+u, alt+shift+u, and hyper/meta chords are not the alias and
  pass through to the application.
- Verification artifacts under `.verification/` at every stage close.
- A terminal validation matrix (`.verification/matrix/`, twelve lanes
  with self-asserting scripts and captured evidence): real `tmux 3.7c`
  and GNU `Screen 5.0.2` multiplexer ptys, `TERM=dumb` and
  redirected-output plain profiles on Linux and Windows,
  grapheme-heavy and `NO_COLOR` panes, a real sshd/ssh pair over
  `-tt`, the same transport shaped by `tc netem` (200 ms delay,
  512 kbit) with an strace-measured probe budget, an interactive
  scripted Windows Terminal 1.24 window with screenshots, a headless
  classic-conhost lifecycle transcript, the Windows redirected plain
  profile, and the examples/ssh server driven by a real OpenSSH
  client inside tmux. Three integration findings came out of it and
  are documented in the matrix README: `ssh -tt` from a piped client
  requests a zero-row/zero-col pty (a 0-cell canvas paints nothing —
  size the remote pty with `stty` first); keystrokes sent before raw
  mode is established can be eaten by the cooked line discipline as
  XON/XOFF flow control; and an interactive busybox ash on a pty
  emits a cursor-position query (`ESC[6n`) and blocks until the reply
  — anything driving an interactive shell through a pty must answer
  DSR like a terminal does.
- `examples/ssh`: a nested Go module serving LUX TUI over SSH
  (gliderlabs/ssh + creack/pty). Each session is bridged onto a real
  pty and the requested program — by default the binary itself
  re-exec'd with `-child` — runs there as the session leader, so the
  engine sees a genuine terminal (raw mode, probes, SIGWINCH) with
  zero library changes. The server is also a generic pty forwarder:
  any argv can be served (`-- /bin/sh`, `-- ../showcase/showcase`).
  Resize window-change requests become `TIOCSWINSZ`; the child's exit
  status travels back as the SSH exit status; a canceled connection
  kills the child's process group (nothing leaks). Unit tests fake
  the gliderlabs session seam — including its newline munging and the
  DSR reply a real terminal gives — and drive `sh` on a real pty for
  size/TERM propagation, exit codes, resize, and disconnect-kill.
  Unix-only (`//go:build !windows`, creack/pty has no Windows port).
  README documents the design and its caveats (demo password auth,
  no signal forwarding — disconnect already kills the child).
- Sideband channels riding the frame, all opt-in: `WithProgress`
  (taskbar progress, OSC 9;4), `WithNotifications` (desktop
  notifications, OSC 9;777), and `WithClipboard` (write-only clipboard,
  OSC 52), armed per frame by `frame.SetProgress`, `frame.Notify`, and
  `frame.CopyToClipboard`. The queued bytes ride the same terminal
  write as the frame that queued them — after the diff, before the
  `OnPresented` callbacks — so a dropped or failed frame drops its
  annexes with it and the newest frame always wins. Progress is a
  reported state (the View re-states it every frame the bar should
  exist), so suspend/resume restores it for free, and every leave path
  ends with the `9;4;0` reset after the title pop — no taskbar bar
  outlives the program. OSC 52 is write-only (the read direction is
  never built; payloads over 32 KiB are dropped rather than clipped);
  notify fields are sanitized of control bytes. Consent is layered:
  every channel defaults off, a zero `&Frame{}` queues nothing,
  backends without the one-method `AnnexWriter` extension ignore
  annexes entirely, and a default build is byte-identical to a
  sideband-unaware program. Not claimed: an OSC 52 read path, kitty
  clipboard-name extensions, notify actions, or a capability probe
  (the sequences are undetectable by reply — the terminal matrix is
  the honest cost, pinned by the raw-wire annex validation lane with a
  behaviorally decoded OSC 52 proof through tmux `set-clipboard`).
- `examples/annex`: the live sideband demo — keys walk the five
  progress states, bump the percent, queue a toast, and queue a
  clipboard write, all from the frame.

### Changed

- The Unix backend no longer `Notify`s `SIGTSTP` to itself: a signal that
  was never registered keeps the kernel's default disposition, so the
  re-raised stop truly stops the process group (the Go 1.21+ runtime
  swallows kill-sent `TSTP` after a `Notify`→`Reset` cycle, which made
  suspend a no-op on a direct terminal lane). The stop chain is now: raw
  mode disables `ISIG` so Ctrl+Z arrives as byte 0x1A, the run loop
  intercepts it, writes the leave sequences, restores cooked mode, and
  only then re-raises `SIGTSTP` — the shell's disposition takes it from
  there. An external `kill -TSTP` on the running process is now a plain
  kernel-default stop with no leave bytes (the handshake runs only for
  the intercepted byte); the stopped process still resumes cleanly
  through the normal resume path.
- `ui.TextInput` and `ui.TextArea` treat hyper and meta modifier chords
  exactly like alt and super: the editor never eats them as edits, so
  the application stays free to bind them — the same family boundary the
  undo bindings already honor. Previously a hyper+u keystroke inserted
  `u` as text.
- The encoder emits the shortest VT encoding of a frame and lets the terminal
  move rows it already has through scroll regions instead of repainting them.
- Resizes are delivered through a last-wins slot, so a resize storm cannot
  displace the newest size.
- Windows console sizes resolve through the screen buffer instead of the legacy
  console API.
- Compatibility and README documentation now describe the negotiated capability
  behavior instead of the pre-negotiation state.

### Fixed

- Scoped shortcuts no longer compare keyboard lock state: the caps-lock and
  num-lock modifier bits now drop out of shortcut matching on both the
  binding and the incoming event. The win32-input parser faithfully reports
  lock state in its modifier byte — every F1 arriving under Windows Terminal
  with num-lock on carried `ModNumLock`, while the binding registered plain
  F1, and the exact `Mod` comparison made the shortcut unmatchable (F1 help
  never opened; the same failure shape hits kitty's modifier set, since it
  also reports lock bits). Bindings with intent modifiers (Ctrl+F1, …) keep
  requiring them exactly; only the lock bits are exempt. A regression test
  covers all three cases: plain binding matched under lock bits, locked
  binding matched by a plain key, and Ctrl+F1 not matched by num-lock-only.
  Found and verified end-to-end in a real Windows Terminal 1.24 lane
  (F1 → modal screenshot → Esc → Tab → rune → Ctrl+Q → exit 0), not just in
  unit tests. The showcase footer now honestly advertises that plain `q`
  also quits.
- Windows raw mode is entered through a hardened `makeRawTerm` that also
  sets the `ENABLE_VIRTUAL_TERMIN_INPUT` console mode bit before the raw
  flags land (best-effort: a failure degrades to the x/term raw state).
  Defense-in-depth found during the same investigation — under ConPTY,
  `x/term`'s `MakeRaw` already sets the bit (verified
  `0x1f7 → 0x3f0`), so this is not the fix for the shortcut bug above,
  only an explicit, tested contract that `Open` and `resume` share one
  make-raw path.
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
