# Go TUI Library Landscape — Late 2026

Competitive research for **lux-tui** (immediate-mode Go TUI). All facts below were verified on **2026-10-06/07** against primary sources: the GitHub REST API (repo metadata, releases, issues, PRs), raw README/CHANGES files on `raw.githubusercontent.com`, vendor blog/docs, and the HN Algolia API. Claims that could not be confirmed are explicitly marked **UNVERIFIED**.

> Method note: the session's web_search backend was unavailable; research used direct HTTP fetches of the sources above. Star counts and dates come from `api.github.com` snapshots on the dates above.

---

## 1. gdamore/tcell — retained-cell workhorse, mid-v3 transition

**Maintenance:** 5,224 stars; last push 2026-10-06; not archived; Apache-2.0; offers Tidelift and paid support (Staysail Systems). Latest release **v3.5.0 (2026-09-11)**; the v3 line began with **v3.0.0 (2025-12-01)**. ([repo](https://api.github.com/repos/gdamore/tcell), [v3.5.0](https://github.com/gdamore/tcell/releases/tag/v3.5.0), [v3.0.0](https://github.com/gdamore/tcell/releases/tag/v3.0.0))

**Architecture:** classic retained cell grid (SetCell-style draw + `Show()` flush with internal damage tracking).

**v3 is a major break** ([CHANGESv3.md](https://raw.githubusercontent.com/gdamore/tcell/master/CHANGESv3.md), [release notes](https://github.com/gdamore/tcell/releases/tag/v3.0.0)):
- **Terminfo subsystem removed entirely.** No terminfo database, no `tput` exec; a small set of built-in terminal classes; unknown `$TERM` is assumed xterm/ECMA-48. Pure Go, no CGO.
- **Termbox compatibility package removed**; `NewConsoleScreen` removed → **legacy Windows console-mode support dropped**; requires Windows 10 1703+; uses the modern Windows VT API, win32-input-mode, and `ReadConsoleInput` for resize; **24-bit color is the default on Windows**. ([README-windows.md](https://raw.githubusercontent.com/gdamore/tcell/master/README-windows.md))
- `EventKey` now carries a string (`Str` vs `Rune`) — enables **multi-rune grapheme-cluster keystrokes**; kitty "associated text" for shifted keys landed in v3.5.0.
- Event loop: channel exposed via `EventQ` (PollEvent/PostEvent removed); `Put`/`Get` cell APIs; 32-bit Color / 16-bit attr / 8-bit underline style (styled & colored underlines; `AttrUnderline` removed).
- v3.0.0 highlights: "improvements in event handling, richer support for more keys and modifier reports … desktop notification support, simpler APIs for emitting strings directly to the screen."
- `SimulationScreen` (public test double) removed; replaced by internal `MockScreen`/`MockTerm`, not public API.

**Input:** mouse incl. modern protocols (README-windows: Alacritty rated good for "modern key reporting, mouse, and bracketed paste"), extended keys/modifier reports, kitty keyboard support (partial, incl. associated text). **Output:** damage-tracked diffing, 24-bit color (with downgrade path), styled underlines. **Platforms:** POSIX, Windows (VT API), WASM, Plan 9 (best-effort). ([README](https://github.com/gdamore/tcell))

**Windows testing matrix (from README-windows):** Windows Terminal is the main target; Alacritty good (but limited Unicode); WezTerm/Termius/Putty/MobaXterm OK for remote use; **ConEmu and mintty not recommended**; SSH *from* Windows degrades keyboard handling to legacy VT encodings.

**Weaknesses:** ecosystem fragmentation — v1 unmaintained, v2 line still maintained (tview pins v2.8.1), v3 adoption is slow because the break is large; public test-double API removed in v3; SSH-from-Windows key degradation.

**Relevance to lux-tui:** tcell is the *engine* under tview and many apps. Its v3 direction (drop terminfo, drop legacy console, kitty/associated-text, win32-input-mode) defines the modern baseline a new library must match — and its v2/v3 split is a compatibility opening for newcomers.

---

## 2. Charm v2 ecosystem (bubbletea · lipgloss · bubbles · x · colorprofile) — the state of the art

**Bubble Tea** ([repo](https://api.github.com/repos/charmbracelet/bubbletea)): 45,307 stars; last push 2026-10-02; latest **v2.0.10 (2026-09-24)**; v2 GA **2026-02-24**. The Charm blog states the ecosystem "powers more than 25,000 open-source applications", with teams at NVIDIA, GitHub, Slack, Microsoft Azure; the v2 line has powered **Crush** (Charm's AI coding agent) in production since the beta. ([charm.land/blog/v2](https://charm.land/blog/v2/), [v2.0.0 notes](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0))

**Architecture:** Elm-architecture message loop (Model/Update/View). v2's headline API change: **`View()` returns a `tea.View` struct** — declarative fields replace imperative commands and startup options: `Content`, `AltScreen`, `MouseMode` (None/CellMotion/AllMotion), `ReportFocus`, `DisableBracketedPasteMode`, `WindowTitle`, `Cursor` (position/shape/color/blink), `ForegroundColor`/`BackgroundColor`, `ProgressBar` (native terminal progress bar), `KeyboardEnhancement`. `tea.KeyPressMsg` replaces `tea.KeyMsg`; `case "space"` replaces `case " "`. ([UPGRADE_GUIDE_V2.md](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md))

**Rendering — the "Cursed Renderer":** modeled on the ncurses rendering algorithm; Charm claims "rendering is faster and more efficient by orders of magnitude", with SSH bandwidth savings described as "monetarily quantifiable". **No published benchmark numbers were found** in any fetched document — quantitative claims UNVERIFIED. ([blog](https://charm.land/blog/v2/))

**Synchronized output (DECSET 2026):** on program start bubbletea v2 *queries* the terminal for synchronized-output support and, if present, uses mode 2026 instead of hide/show-cursor during updates; the cursed renderer owns altscreen/cursor state selection between "mode 2026 or hide/show cursor". ([PR #1542](https://github.com/charmbracelet/bubbletea/pull/1542))

**Keyboard:** kitty keyboard protocol with progressive enhancements (shift+enter, super+space, **key release events**), easy support detection and automatic fallbacks. ([v2.0.0 notes](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0))

**Other I/O:** clipboard transfer over SSH (OSC-52-style), mouse modes, automatic color downsampling, inline images, focus reporting (`View.ReportFocus`), bracketed paste on by default (`View.DisableBracketedPasteMode` to opt out). ([blog](https://charm.land/blog/v2/), [UPGRADE_GUIDE_V2](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md))

**Testing:** Elm model separation makes `Update` unit-testable; program options `tea.WithColorProfile`, `tea.WithWindowSize` "great for testing"; experimental `teatest` and `golden` packages live in charmbracelet/x. ([UPGRADE_GUIDE_V2](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md), [x README](https://github.com/charmbracelet/x))

**Lip Gloss** ([repo](https://api.github.com/repos/charmbracelet/lipgloss)): 11,902 stars; pushed 2026-10-01; latest **v2.0.6 (2026-08-11)**: fixed Devanagari (Hindi) rendering; **auto-switches to Unicode core mode 2027** when the terminal supports it, "measures widths the same way the terminal does" (the **Ultraviolet** width/grapheme engine). ([v2.0.6 notes](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6))

**Bubbles** ([repo](https://api.github.com/repos/charmbracelet/bubbles)): 8,972 stars; pushed 2026-10-04; latest **v2.2.1 (2026-08-24)** (textarea word-left boundary freeze fix, #1036). ([release](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1))

**charmbracelet/x** ([repo](https://api.github.com/repos/charmbracelet/x)): 320 stars; pushed 2026-10-04; very active experimental monorepo: `ansi` parser, `cellbuf`, `input`, `term`, `termios`, **`conpty` (Windows PTY)**, **`xpty` (cross-platform PTY)**, **`vt` (terminal emulator)**, `wcwidth`, `colors`, **`mosaic` (image→terminal rendering)**, **`pony` (declarative TUI markup language)**, `nerdfont`, `exp/teatest`, `exp/golden`. ([README](https://github.com/charmbracelet/x))

**colorprofile** ([repo](https://api.github.com/repos/charmbracelet/colorprofile)): created 2024-09; 133 stars; pushed 2026-10-01 — the in-stack successor to termenv for color-profile detection/degradation.

**termenv** ([repo](https://api.github.com/repos/muesli/termenv)): 2,027 stars; last push **2025-11-21** (~10.5 months dormant); not archived; no deprecation notice in README; detects color range, TrueColor→256→16→Ascii degradation, light/dark detection, respects NO_COLOR and CLICOLOR_FORCE. Effectively frozen; superseded inside the Charm stack by colorprofile. ([README](https://github.com/muesli/termenv))

**Weaknesses / complaints (verified):**
- **Breaking v2 migration**: new `charm.land` module path, `View()` signature change, `KeyMsg`→`KeyPressMsg` — a real cost for the huge v1 app base (45k stars, "never pushed a breaking change" before v2). ([UPGRADE_GUIDE_V2](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md))
- **IME is a known hole**: bubbles #361 "textinput does not work well with pinyin(Chinese) input method" — open since 2023-03, 16 comments, never fixed (4 IME-related issues total). ([#361](https://github.com/charmbracelet/bubbles/issues/361))
- Flicker-adjacent issues persist post-v2: 11 issues match "flicker", e.g. window-title flicker on exit (#1797, Sept 2026, unmerged fix), exec-subprocess view clearing (#1762). ([#1797](https://github.com/charmbracelet/bubbletea/pull/1797), [#1762](https://github.com/charmbracelet/bubbletea/pull/1762))
- a11y: #780 "Does Bubble Tea have anything on the roadmap for screen reader accessibility?" — open since 2023-07, no implementation; requester's workaround is shipping a separate `--web` app. ([#780](https://github.com/charmbracelet/bubbletea/issues/780))

**Relevance to lux-tui:** the Charm v2 stack is the feature benchmark for 2026. lux-tui's immediate-mode architecture is a genuine differentiator — nothing in the mainstream Go ecosystem works that way — but feature parity against the checklist below (esp. kitty keys, mode 2026, mode 2027/Ultraviolet-class widths, PTY/ConPTY story, golden-file testing) is the bar.

---

## 3. rivo/tview — rich widget toolkit, maintenance-mode

**Maintenance:** 14,121 stars; last push **2026-08-11**; MIT; **no GitHub Releases objects** — versioned via git tags only (issue #1169 references v0.42.0; install = module version or @master); deps: tcell **v2.8.1** (not v3), fatih/color, go-colorful, go-runewidth, rivo/uniseg; `go 1.25.0`. ([repo](https://api.github.com/repos/rivo/tview), [go.mod](https://github.com/rivo/tview/blob/master/go.mod), [#1169](https://github.com/rivo/tview/issues/1169))

**Architecture:** retained-widget toolkit over tcell: Forms, TextView, TextArea, Table, TreeView, List, Pages/Grid/Flex, modals, **Images**, focus management, Box-based composition. Recent commits are small fixes ("Select method for InputField. Resolves #892", Aug 2026; "Syncing after resuming. Might be a fix for #244"). ([commits](https://api.github.com/repos/rivo/tview/commits))

**Input/output:** inherits tcell v2 (mouse, bracketed paste); no kitty keyboard protocol (tcell v2 line), no sync output/mode 2026/2027 integration, no IME handling, no a11y mode. **Images widget** exists in the README feature list; implementation details (sixel vs block graphics) **UNVERIFIED** — no merged PR found in issue search. ([README](https://github.com/rivo/tview))

**Weaknesses / complaints (verified):** 839 open issues; Table horizontal-scroll jumps back to first column in edge cases (#1169, open, Oct 2026); Form lacks field enable/disable with proper focus/color behavior (#1161, open); no universal widget-parenting API for cascading shortcut processing (#728, closed Aug 2026); README itself warns that tview "may break" on tcell changes and that some public interfaces are de-facto internal. Widely-cited `tview.Styles`/`tview.Borders` **global mutable state** complaint: **UNVERIFIED** in this pass. ([#1169](https://github.com/rivo/tview/issues/1169), [#1161](https://github.com/rivo/tview/issues/1161), [#728](https://github.com/rivo/tview/issues/728))
- a11y: #1047 "Add accessibility mode" (proposed `DrawCLI`/`AccessiblePrimitive` dual-render) — open since 2024-10, no implementation. ([#1047](https://github.com/rivo/tview/issues/1047))

**Ecosystem still alive:** e.g. *tiki* (Markdown viewer with images, SVG, mermaid rendering, built entirely on tview) was added to the README project list in Aug 2026. ([#1152](https://github.com/rivo/tview/pull/1152))

**Relevance to lux-tui:** tview owns the "batteries-included widget toolkit" niche but is single-maintainer, slow-moving, stuck on tcell v2, and has no releases. A new library with composable, testable widgets + modern protocols would attack it directly.

---

## 4. gizak/termui — dashboard widgets, dormant

**Maintenance:** 13,592 stars; last push **2025-07-10** (~15 months); MIT; no GitHub releases (module `github.com/gizak/termui/v3` via tags/branches); README states maintainer availability "fluctuates" and the project is **looking for maintainers**. ([repo](https://api.github.com/repos/gizak/termui), [README](https://github.com/gizak/termui))

**Architecture:** grid-based dashboard widget set built on **termbox-go** (never migrated): BarChart, braille Canvas, Gauge, Image, List, Tree, Paragraph, PieChart, Plot, Sparkline, StackedBarChart, Table, Tabs; relative-grid or absolute positioning.

**Relevance to lux-tui:** the chart/dashboard niche is stagnant and still on termbox — a grapheme-aware, protocol-modern chart/widget layer is uncontested.

---

## 5. gocui — quiet, no successor

**Maintenance (jroimartin/gocui):** 10,611 stars, 643 forks; last push 2025-05-01 (README-only commit removing the projects list; last real code ~Sept 2024); BSD-3; **no deprecation note**, but effectively inactive. ([repo](https://api.github.com/repos/jroimartin/gocui))

**Architecture:** view manager: views as `io.ReadWriter`, overlapping views, concurrent-safe runtime modification, global + per-view keybindings, mouse, colored text, customizable edit mode. ([README](https://github.com/jroimartin/gocui))

**Forks:** the `awesome/gocui` fork is **gone (HTTP 404)**; a GitHub repo search for "gocui" (92 results) shows jroimartin/gocui as the top hit — **no prominent maintained fork identified**. ([search](https://api.github.com/search/repositories?q=gocui))

---

## 6. nsf/termbox-go — legacy

4,787 stars; last push 2026-08-23 but only drive-by PRs (e.g. 2026-06-23 #256, Windows light colors); MIT; README says plainly: **"This library is somewhat not maintained anymore"** and recommends tcell. Windows support is the old winapi console path; no SGR-era features (kitty keys, bracketed paste, sync output). ([repo](https://api.github.com/repos/nsf/termbox-go), [README](https://github.com/nsf/termbox-go)) — status of the separate `termbox2` C library: **UNVERIFIED**.

---

## 7. Emerging players 2024–2026

- **"gonotebook": NOT FOUND.** GitHub search returns only a 2014-era Go book source repo (`feyeleanor/GoNotebook`) and 0-star repos — no TUI library by that name exists. ([search](https://api.github.com/search/repositories?q=gonotebook))
- **No notable immediate-mode Go TUI library found.** The `topic:tui language:go` space (4,089 repos, star-sorted) is headed by *apps* (wagoodman/dive, etc.), not new frameworks. ([search](https://api.github.com/search/repositories?q=topic:tui+language:go&sort=stars)) — lux-tui's immediate-mode model remains an open lane.
- **pterm** ([repo](https://api.github.com/repos/pterm/pterm)): 5,548 stars; pushed 2026-07-11; "beautify console output" framework (charts, progressbars, tables, trees, text input, select menus) — pretty-printing with some interactive pieces, not a full TUI app framework.
- **Gaurav-Gosain/tuios** ([repo](https://github.com/Gaurav-Gosain/tuios), created 2025): "terminal window manager that knows what your agents are doing" — tiling panes, workspaces, restart-surviving sessions, agent inbox. An *app*, not a library, but a signal of the agent-era demand that Charm explicitly names as the reason for v2. ([blog](https://charm.land/blog/v2/))
- **Charm experimental surface** counts as the real "emerging" story: `x/pony` (declarative TUI markup), `x/mosaic` (image→terminal), `x/conpty`/`x/xpty` (PTY), `x/vt` (embedded terminal emulator). ([x README](https://github.com/charmbracelet/x))
- Ecosystem evidence for agent-era TUI demand: Show HN (Jan 2026) "Plural — parallel Claude Code sessions", built with Go and Bubble Tea v2. ([HN story](https://www.zhubert.com/plural))

---

## 8. Terminal protocol adoption (late 2026)

| Protocol / feature | Go-library support (verified) | Per-terminal matrix |
|---|---|---|
| Kitty keyboard protocol | bubbletea v2 (progressive enhancements: shift+enter, super+space, key releases, detection+fallback — [v2.0.0 notes](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0)); tcell v3 (incl. kitty associated text, [v3.5.0](https://github.com/gdamore/tcell/releases/tag/v3.5.0)) | **UNVERIFIED** (kitty spec page fetch was truncated) |
| Synchronized output, DECSET **2026** | bubbletea v2 queries terminal and uses it instead of cursor hide/show ([PR #1542](https://github.com/charmbracelet/bubbletea/pull/1542)) | **UNVERIFIED** per-terminal |
| Unicode core, DECSET **2027** | lipgloss v2 auto-enables when supported ([v2.0.6 notes](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6)); spec = contour-terminal/terminal-unicode-core (created 2021, **alpha stage**, last push 2026-06-23; goals: grapheme clusters, widths, ligatures+emoji "without sacrifice" — [repo](https://api.github.com/repos/contour-terminal/terminal-unicode-core), [README](https://github.com/contour-terminal/terminal-unicode-core)) | **UNVERIFIED** per-terminal |
| Bracketed paste | bubbletea (on by default, `View.DisableBracketedPasteMode` — [upgrade guide](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md)); tcell (README-windows rates Alacritty good incl. bracketed paste — [README-windows](https://raw.githubusercontent.com/gdamore/tcell/master/README-windows.md)) | partial |
| Focus reporting (1004) | bubbletea `View.ReportFocus` ([upgrade guide](https://raw.githubusercontent.com/charmbracelet/bubbletea/main/UPGRADE_GUIDE_V2.md)) | **UNVERIFIED** |
| SGR mouse (1006) / all-motion | bubbletea MouseMode enum; tcell v3 modern mouse | partial |
| Grapheme clustering / width | rivo/uniseg under tview; tcell v3 EventKey grapheme strings; lipgloss/Ultraviolet aligned with terminal widths via mode 2027 | partial |
| Windows input | tcell v3: win32-input-mode, VT API, Win10 1703+ floor ([CHANGESv3](https://raw.githubusercontent.com/gdamore/tcell/master/CHANGESv3.md)); Charm: `x/conpty`/`x/xpty` ([x](https://github.com/charmbracelet/x)) | Windows Terminal = primary target for both tcell and Charm |
| Clipboard over SSH | bubbletea v2 ([blog](https://charm.land/blog/v2/)) | — |

**Benchmarks:** no independent, published Go TUI benchmark suite was found in this pass — **UNVERIFIED**. The only quantified-adjacent claim is Charm's qualitative "orders of magnitude" for the Cursed Renderer ([blog](https://charm.land/blog/v2/)).

**Accessibility across the ecosystem:** no Go TUI library ships screen-reader support or a11y hooks. Both flagship trackers are open feature *requests* with zero implementation: bubbletea [#780](https://github.com/charmbracelet/bubbletea/issues/780) (2023) and tview [#1047](https://github.com/rivo/tview/issues/1047) (2024, proposes a `DrawCLI` dual-render). This is the single clearest white space in the whole landscape.

---

## 9. State-of-the-art checklist (union of best-in-class, late 2026)

A competitive Go TUI library in late 2026 needs, at minimum, everything below; ✅ = who demonstrably has it today.

**Input**
- ✅ kitty keyboard protocol with *capability query + progressive enhancement + automatic fallback* (bubbletea v2; tcell v3 partial)
- ✅ key **release** events; super/shift-modified Enter/Space (bubbletea v2)
- ✅ SGR mouse incl. all-motion mode, opt-in per view (bubbletea; tcell)
- ✅ bracketed paste, on by default with opt-out (bubbletea; tcell)
- ✅ focus/blur reporting (bubbletea)
- ✅ multi-rune grapheme keystroke delivery (tcell v3 EventKey strings)
- ✅ win32-input-mode + modern Windows VT input (tcell v3; Charm x)
- ✅ desktop notifications (tcell v3)
- ❌ IME / composition-state handling (nobody — bubbles #361 open since 2023)

**Rendering & output**
- ✅ ncurses-style damage-tracked renderer with double buffering (bubbletea v2 Cursed Renderer; tcell)
- ✅ synchronized output mode 2026, queried, used instead of cursor flicker (bubbletea v2)
- ✅ Unicode core mode 2027, queried; grapheme-cluster widths matching the terminal (lipgloss v2 + Ultraviolet)
- ✅ wide/emoji cells; UAX #29 via uniseg (tview, lipgloss)
- ✅ 24-bit color with query-aware downsampling to 256/16; NO_COLOR / CLICOLOR_FORCE (termenv→colorprofile; bubbletea; tcell)
- ✅ styled/colored underlines (tcell v3)
- ✅ clipboard transfer incl. over SSH (bubbletea v2)
- ✅ window-title management with restore-on-exit semantics (bubbletea — still being fixed, #1797)
- ✅ native terminal progress bar (bubbletea v2 `View.ProgressBar`)
- ✅ inline images (Charm x/mosaic; tview Images — details UNVERIFIED)
- ✅ alt screen *and* inline rendering as first-class modes (bubbletea v2)

**Architecture & testing**
- ✅ declarative view state (bubbletea v2 `tea.View`)
- ✅ side-effect-free render layer for pure unit tests (lipgloss)
- ✅ test harness: teatest + golden files (Charm x, experimental); mock screen (tcell internal-only)
- ❌ public screen-mock/test renderer in a stable API (regression: tcell v3 removed SimulationScreen)

**Platform & packaging**
- ✅ pure Go, no CGO, no `tput` exec (tcell v3; Charm)
- ✅ POSIX + Windows 10 1703+ VT (legacy console dropped), WASM; Plan 9 best-effort (tcell)
- ✅ embedded PTY story: ConPTY + cross-platform xpty (Charm x)
- ✅ SSH bandwidth awareness as a design goal (bubbletea v2)

**Open white space for a new entrant (lux-tui)**
1. **Accessibility / screen-reader mode** — literally nobody has it; two open requests with no movement.
2. **IME (CJK) input** — bubbletea/bubbles has a 3-year-old open pinyin bug; tview/tcell nothing.
3. **Immediate-mode architecture** — no notable Go library works this way (retained grid and Elm dominate).
4. **tcell-v3-class features without the tview debt** — tview is on tcell v2, single-maintainer, releaseless; termui is on termbox; gocui is quiet.
5. **Published, reproducible benchmarks** — the entire ecosystem argues performance without public numbers.
