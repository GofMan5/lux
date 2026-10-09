# Compatibility

Snapshot: **2026-10-08**. Entries describe the code currently present, not a
release certification. Automated tests cover rendering, encoder, parser, and
negotiation logic; the real-terminal matrix below remains to be executed and
published.

Legend: **implemented** = code path exists; **degraded** = conservative/basic
behavior; **planned validation** = plausible path without current evidence.

| Environment | Current status | Expected behavior / limit |
|---|---|---|
| Windows Terminal + ConPTY | implemented, validation pending | VT output, raw input through `x/term`, win32-input-mode keyboard when the mode 9001 probe answers, 100 ms resize polling |
| Windows 10+ ConHost in cmd/PowerShell | degraded, validation pending | attempts `ENABLE_VIRTUAL_TERMINAL_PROCESSING`; answers the mode 9001 probe with full win32-input support, so the keyboard path matches Windows Terminal there; modern mouse behavior depends on console support |
| Windows 7/8 legacy console | not supported | no published fallback to classic screen-buffer rendering |
| Unix PTYs on Linux/macOS/BSD | implemented, e2e-verified | termios raw mode, VT output, `SIGWINCH` resize, `SIGTSTP`/`SIGCONT` suspend-resume round trip |
| tmux | planned validation | legacy VT keyboard and mouse; tmux's own DECRPM answers for the output family (DECOM, 2026) are trusted because tmux implements those for its panes; input-family verdicts (kitty, pixel mouse, win32 input) are downgraded by the multiplexer gate so a pane never runs an input encoding tmux did not re-emit — see "Multiplexer provenance" below |
| GNU screen | planned validation | basic VT/ANSI path only; detected through `$STY` or a `screen*` `$TERM` and gated the same way as tmux; screen itself implements neither 2026 nor kitty for panes |
| SSH/mosh | planned validation | negotiation runs before the first frame over the same transport; slow links can spend the whole 150 ms budget (raise `LUX_TUI_PROBE_TIMEOUT`), and impaired-link validation remains |
| CI, redirected stdout, `TERM=dumb` | implemented | monochrome newline-delimited full snapshots; no cursor addressing/input |

Interactive controls require an interactive terminal profile. Every pointer
action in the built-in controls has a keyboard equivalent because mouse support
is environment-derived rather than actively probed. Generic VT transports
provide committed UTF-8 text, not a portable IME preedit/candidate lifecycle.
PowerShell and cmd remain shells; their host terminal determines behavior.

TextInput is EGC-correct under the currently bundled uniseg Unicode 15.0
segmentation; cell widths are corrected to Unicode 18.0.0 by the overlay
described in "Terminal width truth" below. Terminal/font width, RTL visual
editing, locale-tailored words, IME preedit, password hardening, and
multiline are outside the current claim. Paste is
bracketed/bounded by the adapter and then sanitized/bounded again by the field.
LUX TUI does not read/write OSC 52 automatically; clipboard mutation requires
an explicit application-supplied `Clipboard` implementation. No OS adapter is
bundled; remote OSC 52 remains outside core and needs capability/user policy.

Scoped shortcuts compare exact key code, rune, and intent modifiers while
ignoring repeat count and the press/repeat/release action: a shortcut fires on
the activation, once. The caps-lock and num-lock bits report keyboard lock
state, not intent, so they never participate in the comparison — a user with
num-lock on gets the same F1 shortcut as one without. Legacy VT aliases
(Tab/Ctrl-I, Enter/Ctrl-M) remain indistinguishable on the legacy path; once
the kitty protocol is positively negotiated, a terminal that reports the pairs
as CSI u decodes them into distinct events. Do not rely on the split until the
terminal has answered the probe.

`cmd.exe`, PowerShell, and shells running under SSH are command interpreters,
not terminal emulators. Compatibility decisions must be based on the actual TTY
and capabilities, never the shell name alone.

## Current capability behavior

The adapter records interactive mode, Unicode, color depth, alternate screen,
cursor addressing, mouse, focus, bracketed paste, enhanced keyboard,
synchronized output, hyperlinks, Unicode core mode, and graphics. Today:

- redirected output or `TERM=dumb` selects the portable monochrome profile;
- the enhanced (kitty) keyboard stays off until the probe's kitty query is
  positively answered — nothing about the legacy key path changes before
  that;
- on Windows the win32-input mode (DEC private mode 9001) takes the
  disambiguation role the kitty protocol plays elsewhere: the probe asks
  `CSI ? 9001 $ p`, a recognizing answer turns it on unless kitty answered
  too — kitty wins when both do, because its encoding is the richer one —
  and without that answer the keyboard stays on the legacy encodings;
- `NO_COLOR` disables color;
- `COLORTERM=truecolor` or `24bit` selects true color;
- a `TERM` containing `256color` selects the 256-color palette;
- otherwise an interactive VT uses ANSI 16 colors;
- graphics remain cell-based;
- the terminal cursor stays hidden unless a frame requests a caret, and the
  plain profile ignores caret requests entirely;
- synchronized output turns itself on when the terminal's mode 2026 report
  recognizes the mode; `LUX_TUI_SYNC_OUTPUT` overrides the report either way
  (`1`/`true`/`yes`/`on` force the wrapper on, `0`/`false`/`no`/`off` force
  it off, empty or `auto` leaves the decision to the probe) — the same
  direction Bubble Tea v2 took;
- styled underlines (SGR `4:1`-`4:5` shapes and the `58`/`59` underline
  color) are part of the render style — shapes transmit at every color
  depth, the color follows the depth ladder;
- hyperlinks (OSC 8) are opt-in with `WithHyperlinks(true)`: the protocol
  defines no detection mechanism, so it cannot be probed, and the default
  emits frames byte-identical to a links-unaware program;
- Unicode core mode (DEC private mode 2027) turns itself on when the
  terminal's mode 2027 report recognizes the mode — the proposal is an
  ALPHA draft with one implementing terminal, so lux never assumes it —
  and an explicit `lux.WithWidthPolicy` suppresses it, because the
  application then owns the width contract for the stream.

## Startup negotiation

Open probes an interactive terminal once, in raw mode, before the alternate
screen is entered, so the probe meets a quiet stream: the only reports in
flight are the ones it asked for. One write — primary and secondary device
attributes (`CSI c`, `CSI > c`) plus DECRQM for DEC private mode 6 (DECOM, the
proxy for DECSTBM scroll regions), mode 2026 (synchronized output), mode 1016
(pixel mouse), mode 9001 (win32 input), and mode 2027 (Unicode core), plus
the kitty keyboard query (`CSI ? u`) — followed by a bounded read:

- the default budget is 150 ms; terminals that answer usually do so in
  single-digit milliseconds, and a terminal that answers DA1 but not DECRQM
  keeps the probe alive until the deadline, which startup pays once;
- once the classic trio (DA1 and the DECOM and 2026 reports) has answered, a
  25 ms grace replaces the rest of the budget for the kitty, pixel-mouse,
  win32-input, and Unicode-core replies: fragmented answers still land, while
  a protocol-less terminal pays one extra 25 ms instead of the full window;
- `LUX_TUI_PROBE_TIMEOUT` sets the budget in milliseconds; `0`, a negative
  number, `off`, `false`, or `no` disables negotiation entirely (deterministic
  runs, terminals that answer nothing — including the kitty query, so it
  doubles as the escape hatch for a misbehaving multiplexer); an invalid value
  falls back to the default after a one-line notice on stderr;
- the DECRQM answers are asymmetric by design. The DECOM probe can only
  veto: only a report that positively rejects the mode removes scroll
  regions from the snapshot — a silent terminal, a lost reply, or a partial
  report keeps the defaults, so a dead link can never corrupt output. The
  mode 2026 probe drives synchronized output in both directions: a report
  that recognizes the mode (set, reset, or permanently set — the backend
  wraps each frame explicitly, so the reported state does not matter) turns
  the wrapper on, a rejection — the mode unrecognized, or "permanently
  reset", where the set-mode wrapper would be a permanent no-op and the
  frames would stream unsynchronized anyway — removes it, and silence
  leaves it off, so an unconfirmed terminal never receives sequences it may
  not parse. The `LUX_TUI_SYNC_OUTPUT` override stands above this verdict
  on purpose: it is the escape hatch for a terminal whose report lies;
- the kitty query is the other enable-only verdict: a flags reply
  (`CSI ? flags u`) turns the enhanced keyboard on; silence or junk keeps the
  legacy path;
- the mode 9001 query is the Windows counterpart and another enable-only
  verdict: a DECRPM answer that recognizes the mode — set, reset, or
  permanently set — turns the win32 input mode on; a status of 0
  (unrecognized) or 4 (permanently reset, where the enable write would be a
  no-op) rejects it; silence leaves the legacy key path. When kitty answered
  too, the win32 mode is not entered, so only one enhanced encoding is ever
  active on the wire;
- the mode 2027 query is the newest enable-only verdict: a DECRPM answer
  that recognizes the mode — set, reset, or permanently set — enters
  Unicode core mode; a status of 0 (unrecognized) or 4 (permanently reset,
  where the enable write would be a no-op) rejects it; silence leaves the
  mode off, so this backend's own grapheme measurement stays the single
  width authority. An explicit width policy suppresses the mode even on a
  positive answer, and `terminal.UnicodeCore` (the backend-level option
  behind it) forces the decision either way for a terminal whose report
  lies;
- redirected output and `TERM=dumb` skip the probe and use the portable
  plain-text profile.

The probe is honest about what it cannot know. Keystrokes that race it are
replayed into the read loop in order — never dropped. A report that arrives
after the deadline is not filtered either (filtering report shapes would eat
pasted input that merely looks like one); it surfaces as a single unknown-key
event, the price on links slower than the budget. A late kitty flags reply
takes the same exit. A resize landing inside the probe window is replayed as
the first event, so the first layout uses the real terminal size. Inspection
is capped at 4096 bytes, and the cap shortens classification only: bytes
already read are always classified or replayed.

### Multiplexer provenance

The reports above are answered by whichever parser receives them, and inside
a multiplexer that parser is the multiplexer, not the host terminal. The
probe therefore records who answered before trusting the answers:

- DA1 and DA2 bodies are latched with first-answerer-wins semantics, so a
  forged second report cannot overwrite the first;
- DA2's first parameter is the manufacturer code. tmux answers 84 — a value
  no real host claims (Windows Terminal 0, xterm 41, VTE 65, screen 83,
  WezTerm 123, kitty 1) — which makes it a near-collision-free fingerprint
  even where the environment is silent;
- tmux's DA1 is the VT100 shape `?1;2c` (sixel builds append `;4`) while
  every modern host reports a VT220-class attribute set. GNU screen answers
  DA1 byte-identically, so that witness alone cannot tell tmux from screen —
  harmless, because the gate treats every multiplexer the same and the DA2
  code or the environment separates them for bookkeeping;
- the environment corroborates where the reports cannot: `$TMUX`, `$STY`,
  and the propagated `$TERM` (`tmux*`, `screen*`). `$TERM` survives an ssh
  hop that `$TMUX` does not — the local-tmux-into-remote-ssh case — which
  is exactly why the report witnesses exist. Screen's DA2 code is
  deliberately not a fingerprint: the version parameter behind the
  manufacturer code drifts between builds, and 83 was never audited for
  exclusivity among real terminals, so screen stays environment-detected
  only.

Any single witness is enough, and the verdict it drives is asymmetric by
family:

- output protocols — DECOM/DECSTBM, 2026, and the Unicode core mode 2027 —
  keep the multiplexer's answers. They describe the app-to-multiplexer
  path, and tmux implements DECSTBM and 2026 for its panes itself, so its
  supports and rejections are both honest. Mode 2027 joins this family
  even though today's tmux never answers it: a multiplexer that adopts
  the proposal would answer for the pane path it renders, and a
  multiplexer that lies merely receives a set-mode sequence it ignores —
  output modes cannot corrupt decoding, so the input gate has nothing to
  protect against here;
- input protocols — kitty keyboard, pixel mouse, win32 input — have their
  verdicts downgraded to unknown. The bytes a pane receives are whatever the
  multiplexer re-encodes, so a "supported" answer attributes nothing about
  the input path; rejections and silence stand (they come from the same
  parser that feeds the pane). Legacy decoding is what the pane actually
  speaks, and stays.

DSC passthrough cannot rescue the input verdicts: passthrough is output-only,
host replies are consumed by the multiplexer's own input parser and never
forwarded back, and there is no byte-level way to observe "the host answered
me directly" from inside a pane. kitty therefore stays off inside a detected
multiplexer until the multiplexer implements the protocol natively — at
which point its answers describe the pane path and become honest exactly the
way 2026's already are. That is a deliberate cost: a future tmux with native
kitty would be gated off until detection is taught about it, paid back by
never running an enhanced encoding a pane cannot actually receive.

The gate is defense in depth rather than a live-bug fix: today's tmux
honestly rejects 1016 and 9001 (3.7 and later) or never answers them (3.6
and earlier), and drops the kitty query in every version, so its panes
already negotiate to legacy input. What the gate buys is survival of the
next "helpful" multiplexer, a mis-routed passthrough reply, or a pane whose
`$TERM` was forced to a host name — input stays decodable in all of them.

## Enhanced keyboard (kitty protocol)

When the kitty query is answered, the backend pushes the protocol's
disambiguate and event-type flags (`CSI > 3 u`) — no key-only text
reporting. What that buys:

- the flags ride the enter sequence in the same Write, and the pop
  (`CSI < u`) leads the legacy restore in the same Write on every exit path
  — normal Close, the enter-write failure branch, and anything the app
  layer's deferred Close covers. A session that started cannot exit with the
  kitty stack still pushed;
- Esc arrives as `CSI 27 u` and decodes immediately: the 35 ms escape delay
  that disambiguates a lone `0x1b` on legacy paths never applies in enhanced
  mode;
- ambiguous pairs split when the terminal reports them as CSI u: Esc vs
  Alt+key, Ctrl+letter vs its C0 alias (Ctrl+I vs Tab, Ctrl+M vs Enter), and
  multi-modifier combos that legacy encodings cannot express;
- Shift+Tab normalizes to BackTab to match what the legacy `\x1b[Z` path
  delivers, so apps see one event shape either way;
- key events carry an action — press, auto-repeat, release — instead of
  coalescing everything into presses. Repeats arrive as their own
  activations; releases are decoded in full but filtered by default, and
  `WithKeyReleases(true)` delivers them for apps that track held keys.
  Focus-following widgets (text input, lists, scrolling) treat releases as
  inert either way. Enter, Tab, and Backspace never report releases at
  these flag levels (the spec's shell-safety exception: a shell reading the
  same stream must not see Enter eaten by a release);
- every modifier the protocol can name — shift, alt, ctrl, super, hyper,
  meta, caps-lock, num-lock — decodes into modifier bits, lock-state
  included;
- functional and media keys decode: F13-F35, caps/scroll/num lock,
  print-screen, pause, menu, the media and volume keys, and the keypad.
  Kitty numbers these inside the Unicode private-use block and sends CSI u
  for them even in otherwise legacy modes, so the mapping is not optional
  for enhanced terminals. Keypad keys normalize to their non-keypad
  equivalents (digits and punctuation as runes, keypad-Enter as Enter,
  keypad arrows as arrows); modifier keys themselves and codepoints kitty
  leaves unused stay unknown rather than injecting private-use runes into
  text.

What does not change: text-producing keys, unmodified Enter/Tab/Backspace
keep their legacy encodings. Enhanced mode never changes rendering or the
mouse path.

The legacy path follows kitty's truth where emulators diverge: C0 splits —
`0x7f` is Backspace, `0x08` is Ctrl+Backspace (a terminal spelling Ctrl+H
the same byte is indistinguishable on that path); `CSI 29 ~` is Menu for
kitty and F16 for xterm, and Menu wins; Alt-modified arrow-family keys
arrive with a doubled escape (`ESC ESC [ A` is Alt+Up) and a bare doubled
escape surfaces as two Escapes one 35 ms timeout apart. Legacy `~` codes
25-34 map to F13-F20 with the Menu hole, matching what enhanced terminals
re-send.

The verdict trusts the first answerer, and the multiplexer gate above
decides whether that answerer is trustworthy for input: inside a detected
tmux or screen the kitty flags reply is downgraded and the legacy path
stands, whatever the reply claimed. Disabling negotiation with
`LUX_TUI_PROBE_TIMEOUT=off` restores the pure legacy path.

## Windows keyboard (win32 input mode)

The kitty protocol has no Windows footprint — mintty and foot, the
kitty-mode terminals of the POSIX world, are not the Windows story — so the
Windows equivalent of keyboard disambiguation is Microsoft's own
win32-input-mode (DEC private mode 9001, the "improved keyboard handling in
ConPTY" spec). Windows Terminal and ConPTY's ConHost answer for it; WezTerm
implements it on every platform and defaults it on for Windows since the
20220624 build; mintty and foot implement kitty instead and ignore 9001,
which the probe treats as silence and honest legacy degradation.

When the mode 9001 probe is answered and kitty did not answer, the backend
sends `CSI ? 9001 h` on enter and `CSI ? 9001 l` on every exit path — the
same write composition and the same snapshot discipline as the kitty stack.
ConPTY itself enables 9001 unconditionally when it spawns, so the mode may
already be set; the enable is idempotent and the leave matches what the
session set. What the mode buys:

- every key event arrives as a structured record
  `CSI Vk;Sc;Uc;Kd;Cs;Rc _` — virtual key, scan code, Unicode translation,
  key-down flag, control key state, repeat count — so press/repeat/release
  truth exists before any C0 collision matters;
- releases are decoded in full and follow the same default filtering and
  `WithKeyReleases(true)` delivery as the kitty path;
- ambiguous pairs split: Ctrl+letter vs its C0 alias, Esc vs Alt+key, and
  multi-modifier combos the legacy encodings cannot express, all from the
  control-key-state field rather than byte guessing;
- modifier truth follows the console: the ctrl bit reads both RIGHT_CTRL
  (0x4) and LEFT_CTRL (0x8), the alt bit both alt keys (0x1/0x2), shift
  0x10, caps-lock (0x80) and num-lock (0x20) as lock state, riding along
  exactly as the kitty arm reports them;
- AltGr is honest: the real AltGr chord is LEFT_CTRL|RIGHT_ALT (0x9), so
  that state on a printable translation is a plain character (a Polish `ą`
  on AltGr+A, for instance), not Ctrl+Alt spam — while genuine
  Ctrl+Alt (0xA, left of both) still decodes as a combo;
- a printable Unicode translation (Uc) becomes the rune of the event, so
  text keys are native and IME/VK_PACKET input that bypasses the records
  still arrives as plain characters on the legacy path;
- the repeat count from the console drives the repeat action, so OS-side
  auto-repeat arrives as `Repeat`-action events rather than synthesized
  presses.

The limits are deliberate. Modifier keys themselves — the Ctrl down/up
bookends that bracket every composition — surface as unknown-key events
carrying their own action and modifier state rather than standalone
modifier presses: the API expresses modifiers as bits on the keys they
modify. The scan code parameter is parsed and then ignored (the virtual key
plus translation already identify every key lux maps). SCROLL_LOCK_ON
(0x40) and ENHANCED_KEY (0x100) control-key-state bits are dropped —
scroll-lock state does not alter decoding and the enhanced-key bit only
marks an extended keyboard, neither is a modifier. Records with more than
six parameters, a non-numeric parameter, a key-down flag outside {0, 1}, or
a repeat count past 0xFFFF are malformed and fall through to the unknown-key
escape hatch, never a panic.

Two deviations from the only other major implementation (charm's lib xt)
are stricter on purpose: a record may omit trailing parameters up to
six total (charm requires exactly six; the spec's own truncated examples
permit fewer), and the key-down flag must be 0 or 1 (charm treats anything
non-1 as a release). Wire truth follows the Microsoft spec: the final byte
is `_`, omitted fields take their defaults (all zero, repeat count 1), and
`;;` skips a field.

What does not change: text-producing keys, Enter/Tab/Backspace semantics,
rendering, the mouse path, and every terminal that never answers the probe —
mintty and foot keep kitty and legacy encodings respectively, and Windows
Terminal without an answer would keep the legacy path too. Spec:
<https://github.com/microsoft/terminal/blob/main/doc/specs/%234999%20-%20Improved%20keyboard%20handling%20in%20Conpty.md>.

## Mouse

The adapter enables SGR-encoded all-motion tracking (`CSI ? 1003 h` with
`CSI ? 1006 h` on enter, and the matching resets on every exit path) instead
of 1002's motion-only-while-pressed, so hover states update without a held
button:

- plain pointer motion arrives as a move with `MouseNone`: xterm encodes it
  as button code 35 (the motion bit 32 over the no-button code 3), and the
  decoder maps that shape to a move instead of mistaking it for a release —
  only the lowercase `m` final byte ever means release;
- press, drag, wheel, and modifier semantics are unchanged: drags are moves
  with the held button, wheels are bit 64 plus a direction, and
  shift/alt/ctrl ride bits 4/8/16;
- releases keep the real button: SGR closes a press with the same button code
  and the `m` final byte, so `Button` reports `Left`/`Middle`/`Right` on
  release rather than collapsing to `MouseNone` (the no-button code 3 on a
  release still decodes as `MouseNone`). The built-in widgets treat releases
  as inert either way;
- out-of-range or malformed mouse reports fall through to the unknown-key
  escape hatch, never to a panic or a stuck parser.

A terminal that ignores the 1003 enable degrades to whatever tracking it
implements on its own; the decoder accepts any SGR report the stream
carries. Legacy X10-encoded mouse (`ESC [ M` plus three raw bytes) is not
decoded because SGR is enabled on every path that has a mouse at all, and
the ancient byte encoding cannot survive raw mode's binary transparency.

### Pixel-precision mouse (kitty 1016)

`WithPixelMouse(true)` opts the program into pixel-coordinate tracking on
terminals that answer the `CSI ? 1016 $ p` mode query — the kitty
pixel-position extension over SGR. The opt-in is deliberate: turning it on
blind would trade proven cell coordinates for unproven pixel ones on every
terminal with a mouse.

The runtime behavior on terminals that answer the query:

- enter swaps the SGR cell encoding (`?1006`) for pixel mode (`?1016`) while
  keeping all-motion `?1003`, and appends the cell-size query `CSI 16 t`;
- the terminal answers `CSI 6 ; height ; width t` (height first — the xterm
  and kitty report shape), and the parser absorbs that reply as
  bookkeeping, mapping `PixelX`/`PixelY` onto `X`/`Y` by floor division;
- mouse events then carry `PixelX` and `PixelY` (0-based, measured from the
  viewport's top left), while cell `X`/`Y` remain available as before;
- back and forward buttons (kitty codes 128-129) decode as `MouseBack`/
  `MouseForward` in both cell and pixel mode;
- a pointer leaving the window arrives as kitty's leave code
  (`256 | 32 | modifiers`) and decodes as a move with `MouseNone` and every
  coordinate set to -1 — the built-in widgets' hit tests simply miss, which
  clears hover;
- on resize the adapter re-queries the cell size, so the mapping stays
  truthful after font-size or window changes;
- while the cell size is still unknown — from enter until the `CSI 16 t`
  reply lands, typically milliseconds — pixel moves carry real pixel
  coordinates but `X`/`Y` of -1, which transiently clears hover. A resize
  that re-queries has the same gap.

A terminal whose 1016 reply is status 0 ("not recognized") or 4
("permanently reset"), or that never answers the query, keeps the default
cell encoding; the option stays inert rather than degrading to a guess.
Malformed pixel reports — button codes past 511, negative coordinates, the
reserved 130/131 codes — fall through to the unknown-key escape hatch, and
the decoder never trusts a zero cell size for mapping.

## Terminal width truth

Cell widths follow a two-layer model: `uniseg` v0.4.7 computes every
grapheme cluster's width, and a Unicode 18.0.0 overlay corrects the
codepoints where uniseg's 15.0-era tables disagree with current data. The
overlay lives in `render/widthoverlay.go` as two range tables derived
mechanically from the 18.0.0 UCD files (`EastAsianWidth.txt`,
`emoji/emoji-data.txt`, `auxiliary/GraphemeBreakProperty.txt`) by measuring
`uniseg.StringWidth` of every codepoint against a 18.0.0 rune-width model:

- 33 wide ranges (1357 codepoints) render two cells: the 16.0 East Asian
  Width promotions (I Ching trigrams `U+2630..U+2637` and hexagrams
  `U+4DC0..U+4DFF`, Tangut supplement and Monogram blocks, CJK Extension
  I), 15.1 additions uniseg missed, and new emoji;
- 53 zero ranges (145 codepoints) render no cells and ride the combining
  mark path: new Extend assignments (Tulu-Tigalari, Latin Extended-D,
  Vietnamese alternate reading marks) and the 16.0 reclassifications from
  SpacingMark into Extend (`U+1D166`, `U+1D16D`);
- no codepoint gets narrower and Extended_Pictographic is stable after
  15.0, so no VS16 presentation table is needed.

A cluster containing an overlay codepoint is measured by translating each
new codepoint to a legacy stand-in with the same break class and width
(`Extend -> U+0300`, `Wide -> U+4E00`) and asking uniseg — that keeps
uniseg's composition rules (regional-indicator pairs, Hangul, VS15/VS16
presentation switches) engaged on the corrected widths. Clusters without
overlay codepoints take the raw uniseg width unchanged, including the
3-cell `U+2E3A` and 4-cell `U+2E3B` (clamped to 2 by the cell model).
`Canvas.Text`, `TextWidth` and `TextInput` all resolve through this single
path.

Segmentation is **not** upgraded: boundaries stay uniseg. Conformance
against the UAX #29 `GraphemeBreakTest`:

- Unicode 15.0.0: 602/602 lines pass — the engine conforms to the data it
  ships;
- Unicode 18.0.0: 827/853 lines pass; the 26 failures are all the new
  GB9c-style Indic conjunct rules (ConjunctLinker/LinkingConsonant classes)
  introduced after uniseg's data was cut, plus the test file's new
  ZWJ-sequence lines built from post-15.0 codepoints.

Practical drift while segmentation stays 15.0: a ZWJ emoji whose
codepoints were all assigned after 15.0 can split into separate clusters
(each still measured with the correct width, so the total advance only
differs where the sequence should have glued), and GB9c Indic conjuncts do
not fuse. A uniseg upgrade will absorb both; the overlay is width-only and
does not touch boundaries.

Width data sources (Unicode License, © Unicode®, Inc.; see
THIRD_PARTY_NOTICES.md):

- https://www.unicode.org/Public/18.0.0/ucd/EastAsianWidth.txt
- https://www.unicode.org/Public/18.0.0/ucd/emoji/emoji-data.txt
- https://www.unicode.org/Public/18.0.0/ucd/auxiliary/GraphemeBreakProperty.txt
- https://www.unicode.org/Public/18.0.0/ucd/auxiliary/GraphemeBreakTest.txt

### Width policy: terminal quirks on top of the truth

The two-layer model above is what a modern terminal renders, and it is the
default. Some terminals disagree, and a `render.WidthPolicy` passed via
`lux.WithWidthPolicy` retells the measuring to match them:

| Knob | Terminals it models | Effect |
| --- | --- | --- |
| `AmbiguousWidth: 2` | xterm under a CJK locale (`ja_JP.UTF-8`), mlterm, older konsole, legacy conhost fonts | East Asian Ambiguous codepoints (Greek, Cyrillic, box drawing, unit signs, enclosed alphanumerics, private-use planes) advance two cells. Only `2` activates the bump; any other value behaves as 1. |
| `VS16Blind: true` | terminals that ignore emoji presentation selectors | one trailing `U+FE0F` is stripped before measuring, so `❤️` and `❤` occupy the same single cell. VS15 (`U+FE0E`) text presentation is always respected. |
| `NarrowEmoji: true` | legacy console stacks that squeeze every emoji into one cell | emoji-presentation graphemes (`😀`, `⚽`, flag pairs, ZWJ families, clusters carrying VS16) advance one cell. CJK ideographs are never narrowed. |

The tables behind the knobs (179 ambiguous ranges / 138,739 codepoints, 80
Emoji_Presentation ranges / 1,228 codepoints) come from the same 18.0.0 UCD
files as the overlay and live in `render/widthpolicy.go`. Policies are
advisory only — they change what lux draws and measures, never the Unicode
data underneath.

Coherence is the contract: the policy flows from `Run` into every canvas the
frame owns (`Canvas`, `Subview`, `Clip`), so `Canvas.Text` advance,
`Canvas.TextWidth`, `Button`/`Modal` truncation and `TextInput` caret
placement all use the same cells, and mouse clicks map onto the cells that
are actually painted. The caveat: the policy is per-application, chosen by
the caller for its target terminal, not auto-detected — drawing truthfully
for a terminal you did not describe is not possible, and misdescribed
terminals will still misalign (misalignment is then local and explainable,
not a lux bug).

The zero policy is byte-identical to the pre-policy behavior: ambiguous
stays 1 cell, VS16 is respected, emoji take 2. `scene3d`'s ASCII luminance
ramp ignores the policy on purpose — its glyphs are width-1 by construction.

### Unicode core mode (DEC private mode 2027)

The contour-led terminal-unicode-core proposal defines one DEC private mode
that moves grapheme-cluster truth into the terminal itself: an enabled
terminal clusters printed text into extended grapheme clusters per UAX #29,
measures emoji as two cells, may widen VS16-carrying clusters to two cells,
and erases-then-overwrites when text is printed at a cluster cell. The
proposal is an **ALPHA draft** with exactly one implementing terminal
(contour) and one more consumer known to adopt it (Charm's Ultraviolet) —
lux-tui treats it accordingly: the mode is entered only after the terminal
positively answered the probe's `CSI ? 2027 $ p` query, and silence or a
rejection keeps it off.

For lux the mode is an alignment device, not a feature dependency. This
library already measures every cluster itself — `uniseg` segmentation with
the Unicode 18.0.0 width overlay (and optionally a width policy) — and a
2027-enabled terminal is held to the same contract, so enabling the mode
changes no measurement code: it converts the terminal from coincidentally
agreeing with lux's cells into being specified to agree. That is also why
an explicit `lux.WithWidthPolicy` suppresses the mode: a policy means the
application owns the width contract for the stream, and one stream cannot
follow two width authorities. The suppression is automatic — `Run` passes
`terminal.UnicodeCore(false)` whenever a policy is set — while
`terminal.UnicodeCore` on the backend overrides the probe verdict in both
directions (`true` forces the mode on despite silence, `false` off despite
a positive answer) as the escape hatch for a terminal whose report lies.

Lifecycle follows the same discipline as every other mode lux enters. The
probe's DECRPM verdicts map as usual — status 1/2/3 recognize the mode,
status 0 (unrecognized) and 4 (permanently reset, where the set-mode write
would be a no-op) reject it. A recognized answer enters the mode with
`CSI ? 2027 h` after the alternate-screen setup, and every exit path resets
it with `CSI ? 2027 l` first, before the win32, kitty, and base restores
(LIFO with the enter order). The reset is unconditional on the latched
path: the proposal leaves 2027's default undefined after DECSTR and RIS,
and the one other consumer known to adopt the mode (Ultraviolet) does not
restore it on exit — a gap lux refuses to inherit, because a leaked
cluster-aware mode would misalign the next program on that terminal.
Inside a detected multiplexer the 2027 verdict stands like the other
output-family answers (see "Multiplexer provenance").

Honesty about adoption: contour is the only terminal known to implement
the mode today, so on every other terminal the probe answer is a
rejection or silence and nothing changes. The capability snapshot exposes
the verdict as `capability.Set.UnicodeCoreMode`; no live-terminal matrix
has run against contour yet (the validation-matrix lanes stay the
prerequisite for any README claim beyond "probe-driven, guaranteed
restore").

Spec sources (the proposal draft and its second consumer):

- https://raw.githubusercontent.com/contour-terminal/terminal-unicode-core/master/spec/terminal-unicode-core.tex
- https://github.com/charmbracelet/ultraviolet

## Styled underlines

`render.Style` carries an underline shape (`UnderlineStyle`: single, double,
curly, dotted, dashed) and an underline color (`UnderlineColor`). The encoder
follows the kitty underline extension — the SGR underline parameter accepts
`4:0`-`4:5` next to the plain `4` (plain single for backcompat, `24` the
off-switch), and the underline color follows `58`/`59` with the same grammar
as `38`/`48` (`58;5;n` or `58;2;r;g;b`, `59` for the default). The shape
tables come from kitty's underline documentation and the Wikipedia ANSI
escape code list.

- shapes transmit at every color depth: a legacy terminal that ignores the
  colon form still underlines, because its parser reads the bare `4` — the
  shape degrades to single, it is never dropped;
- the underline color rides the color ladder: truecolor sends RGB, the
  256-color depth quantizes to the palette, and ANSI 16/monochrome drop the
  color while keeping the shape;
- diffs stay minimal: `4` is re-emitted before a shape change only when the
  underline bit itself was off, `59` returns to the terminal default, and
  the curly-to-single transition resets the shape rather than trusting the
  stale one.

The known hazard is legacy misparse: a terminal that splits colon
sub-parameters reads `4:3;58;2;1;2;3` as separate SGR parameters, and kitty's
page documents the stray `58` family landing on unrelated attributes (italic
on some terminals). The escape sequences are therefore "opt-in by style":
programs that stay with a plain underline or none emit nothing new.

## Hyperlinks (OSC 8)

`render.Link{URI, ID}` stamps a cell; `Canvas.PutCell` is the public way to
write one. The encoder opens a region with `OSC 8 ; params ; URI ST`, closes
with `OSC 8 ; ; ST`, and both cells of a wide grapheme carry the link because
the terminal attributes a printed range as a whole. The `id=` key is the only
defined parameter and rides the params field; switching targets directly —
one open after another — is legal, and an empty URI means closed. The
wire grammar follows the hyperlink gist by Egmont Koblinger.

There is no detection mechanism in the protocol, so nothing is probed:
`WithHyperlinks(true)` opts in, and a terminal either renders the links or
ignores the sequences. When the option is off — the default — frames are
byte-identical to a links-unaware program, and a link-only cell change costs
zero bytes, because the diff never consults links it will not transmit.

The frame contract keeps regions honest on every path:

- each frame starts from an unknown terminal state, so the first run of a
  frame pays the full style right after its link open, and a frame-start
  erase needs no close (no region is open at that point);
- a region opened mid-frame closes before the next style reset or erase
  (`\x1b]8;;\x1b\\\x1b[0m\x1b[K`), so erased cells never inherit the link;
- endFrame closes a region left open at the last written cell, so no link
  leaks across frames;
- blank runs close an active region before printing spaces, and the erasable
  tail stops at the first linked cell, so a trailing link is never trimmed
  into ambiguity;
- the diff compares both URI and ID, so a changed target repaints the cell.

## Window title (OSC 0 and the XTWINOPS stack)

`lux.WithTitle` sets the terminal window and tab title as a lifecycle
claim: the title is written once, in the same Write as the enter
sequences, and restored once, in the same Write as the leave — never per
frame, never on the plain path. The set is OSC 0 (`ESC ] 0 ; title BEL`,
the icon-and-window form) and the restore is bracketed by the XTWINOPS
title stack: `CSI 22;0 t` pushes both titles before the set, `CSI 23;0 t`
pops them after the modes reset on every exit path — the enter-failure
branch, Present-write failure, Close, and the panic cleanup all ride the
same leave assembly.

The stack is what makes the restore exact on a compliant terminal, and it
is the one part of the program that cannot be detected: there is no
report for the stack depth a reply could be parsed from without ambiguity
(`CSI 21 t` answers with an OSC string that parses as keystrokes when
misrouted), so no probe is sent and the honest matrix is the cost:

| Terminal class | OSC 0 | CSI 22;0/23;0 | Exit behavior |
|---|---|---|---|
| xterm, VTE (GNOME and friends), kitty, Alacritty, foot, WezTerm | yes | yes | previous title restored exactly |
| tmux (outer window) | yes | yes | previous title restored exactly; pane titles are a separate mechanism |
| Windows Terminal / ConPTY | yes (tab title) | no | program title persists on exit — ConPTY has no title stack |
| ConHost legacy console | yes (window title) | no | program title persists on exit |
| plain pipe, `TERM=dumb` | n/a | n/a | no title bytes written at any point |

The title string itself is sanitized before any write: C0 controls, DEL,
and C1 runes are stripped, because a BEL or ESC inside the OSC string
would terminate it early and leak the rest of the title as commands;
graphic runes from any script pass through unchanged, and invalid UTF-8
is passed as-is (it cannot inject a control sequence). An empty title
after sanitization is an opt-out: no title bytes are written at all.

Not claimed: runtime retitling after Open, per-frame titles, the
icon-only OSC 1 form, title handling in a custom `Backend` (the interface
is unchanged; an adapter owns its own title policy).

## Sideband channels (OSC 9;4, OSC 9;777, OSC 52)

Three frame-riding sideband channels, each opt-in through a run option:
`WithProgress` (taskbar progress), `WithNotifications` (desktop
notifications), `WithClipboard` (clipboard writes). A `View` arms them
per frame with `frame.SetProgress`, `frame.Notify`, and
`frame.CopyToClipboard`; the queued bytes ride the same terminal write
as the frame that queued them — after the diff, before the
`OnPresented` callbacks — and a dropped or failed frame drops its
annexes with it.

- **OSC 9;4 (progress)**: states none/indeterminate/normal/error/paused
  with a percent clamped to 0–100; state none carries no percent field.
  Progress is a reported state, not an event — the View re-states it
  every frame while the bar should exist — so a suspend/resume cycle
  restores the bar when the next frame re-states it. Every leave path
  ends with `ESC]9;4;0 BEL` after the title pop, so no taskbar bar
  outlives the program.
- **OSC 9;777 (notify)**: title and body are sanitized (control runes
  stripped; `;` stripped from the title only, the body may carry it);
  both-empty queues nothing.
- **OSC 52 (clipboard)**: write-only. The read direction is never
  built — a program never reads the user's clipboard through the
  terminal. Payloads are base64 (std encoding) over the empty selection
  (the terminal chooses); payloads over 32 KiB are dropped rather than
  clipped. The terminal's own consent layer (kitty's prompt, tmux's
  `set-clipboard`) stays the terminal's decision.

Consent is layered by design: every channel defaults off, a zero
`&Frame{}` queues nothing, backends without the one-method `AnnexWriter`
extension ignore annexes entirely, and a default build is byte-identical
to a sideband-unaware program.

Terminal matrix:

| Terminal class | 9;4 | 9;777 | 52 write |
|---|---|---|---|
| Windows Terminal, iTerm2, kitty, VTE-family | varies (WT: yes) | varies | yes (prompted in some) |
| tmux | dropped (pane) | dropped | decoded with `set-clipboard on` |
| plain pipe, `TERM=dumb` | no bytes | no bytes | no bytes |

The sequences are undetectable by reply, so no probe is sent — the
matrix is the honest cost, and the raw wire truth is pinned by the
annex validation lane. Not claimed: an OSC 52 read path, kitty's
clipboard-name extensions, notify actions, per-terminal progress
implementations behind one state API.

## Suspend and resume (job control)

`SIGTSTP` (Ctrl+Z on a foreground job) and the shell's continue (`SIGCONT`,
via `fg`) are a terminal-mode round trip: the process hands the terminal
back to the shell cooked, then takes it back raw. The default Unix backend
does this without application code:

- The leave on suspend is the exact `sequences()` Close would write —
  mode resets, kitty pop, title pop — and the enter on resume is its exact
  inverse, assembled from the same table, so the stacks unwind in the same
  order a fresh Open would produce.
- Cooked mode is restored before `SIGTSTP` is re-raised with the shell's
  own disposition, so the shell reads clean input while the process is
  stopped. A `SIGTSTP` that arrives while already suspended is refused by
  the latch — one leave, never a double pop.
- The stop itself is the kernel's, not a signal handler's: lux never
  registers `SIGTSTP` with the Go runtime, so the disposition stays
  SIG_DFL and the re-raised stop truly stops the process group (the Go
  1.21+ runtime swallows a `Notify`→`Reset` cycle's self-raised `TSTP`,
  which would make the stop a silent no-op). Raw mode disables `ISIG`,
  so Ctrl+Z arrives as the ordinary byte `0x1A`; the run loop intercepts
  it, and `lux.WithSuspend(false)` hands that byte back as a key event
  for an application binding instead. An external `kill -TSTP` is a
  plain kernel-default stop without leave bytes; the normal resume path
  still picks the process up.
- The resume drops the diff baseline: the first Present after resume is a
  full repaint, because the shell or another job may have printed anything
  while the process was stopped. Present while suspended is a clean no-op
  that never commits a baseline.
- The resume state is the Open-time cooked state; the state MakeRaw
  returns on resume is discarded, so the final Close restores exactly what
  Open found. A failing MakeRaw keeps the suspend latched — no half-raw
  claim — and a later resume recovers.
- Models receive `event.Suspend` and `event.Resume` through the ordinary
  update path, and each also dirties the frame: a model can pause
  animation on Suspend while the runtime repaints fully after Resume.
  Models written before the kinds existed treat them as ordinary updates.
- `SIGHUP` is reported as an Error event and the run loop exits through
  the normal restoration path; a hangup usually means the terminal is
  already gone and restoration is best-effort.

Scope and honest limits:

- Designed for foreground job control. A `SIGTTOU` stop (a background
  write attempt) happens without this watcher's involvement; the terminal
  is not handed away there, and the next write resumes as if nothing
  happened.
- `SIGCONT` without a `SIGTSTP` through this watcher is a no-op.
- Windows has no job control. CTRL_CLOSE, LOGOFF, and SHUTDOWN surface as
  `SIGTERM` through `os/signal`: the backend releases the console
  immediately — raw input reset, leave written — and reports an Error
  event, but the OS still terminates the process within its ~5-second
  window. This is best-effort cleanup, not a resume path.
- The signal watcher and the suspend/resume byte contracts are
  unit-tested on file streams, and the whole cycle is now verified
  against real operating-system truth: the `e2e` suite (Linux/macOS) runs
  the runtime under `creack/pty` with an interactive shell as session
  leader and asserts the shell's own `Stopped` line after the raw `0x1A`
  byte, the `fg` resume with full repaint and re-pushed title, the
  double-`SIGTSTP` discard by `SIGCONT`, and probe-racing keystroke
  replay through the real kernel input buffer. One stop-chain fact the
  suite depends on: an orphaned process group (no session leader) has no
  parent to receive `waitpid`, so the kernel discards its stop signals —
  which is exactly why the suite runs the shell lane at all.

## Required degradation ladder

Future features must preserve meaning in this order:

```text
Kitty/Sixel/iTerm image → half-block/braille cells → ASCII
24-bit RGB             → ANSI 256              → ANSI 16 → monochrome
Unicode border         → ASCII border
modern keyboard        → legacy VT key events
interactive UI         → deterministic plain snapshot
```

Applications should also adapt structure by hiding decoration, shortening
labels, stacking regions, and finally scrolling or clipping. They should not
assume `width >= 80`, `height >= 24`, or a nonzero size during a resize.

The runtime bounds reported dimensions with `WithSizeLimit` (500×200 by
default) and uses `WithFallbackSize` (80×24 by default) when a backend cannot
report a viewport. `WithIO` changes the default adapter's files;
`WithBackend` supports PTY, SSH, test, or recording transports.

## Compatibility requirements before v1

- Expose capability provenance (default, probed, or environment-overridden) in
  the public snapshot; today the probe applies its verdicts internally and the
  snapshot remains boolean.
- Parse partial input incrementally with bounded memory and configurable escape
  disambiguation.
- Keep the implemented normal/error/panic restoration checks green; signal
  and suspend/resume handling is implemented and the PTY lane (the
  `e2e` module, Linux) is green — macOS and one BSD lane remain for CI.
- Treat the effective chain separately: outer emulator → multiplexer →
  transport → application.
- Gate OSC, clipboard, notifications, and graphics passthrough by positive
  evidence. (Deviation accepted for the sideband channels: OSC 9;4/9;777/52
  have no report a probe could parse without ambiguity, so they are gated by
  explicit opt-in consent instead — every channel defaults off and OSC 52 is
  write-only. Graphics remain evidence-gated.)
- Repaint fully after a partial write, resize, or detected desynchronization.

## Validation matrix

Before a compatibility claim is added to the README, record exact versions and
run at least:

| Lane | Cases |
|---|---|
| Windows | Windows Terminal/ConPTY; supported ConHost; cmd and PowerShell hosts |
| Unix | Linux PTY; macOS PTY; one BSD lane when CI is available |
| Multiplexers | current and minimum-supported tmux; GNU screen |
| Remote | OpenSSH local→remote; delayed and bandwidth-limited stream |
| Noninteractive | pipe, file, CI, `TERM=dumb`, `NO_COLOR` |
| Unicode | combining marks, ZWJ emoji, flags, VS15/VS16, CJK, final-column wide glyph |
| Lifecycle | resize storm, writer failure, cancellation, panic cleanup, suspend/resume — the `e2e` PTY module covers the suspend/resume round trip and probe-race replay on Linux |

Compatibility evidence belongs in reproducible scripts or CI artifacts, not in
an anecdotal "works on my terminal" table.

The stage 9 run of this matrix lives in `.verification/matrix/` with
self-asserting lane scripts and captured evidence. Two integration
findings from that run apply to anyone driving a LUX TUI app through
piped transports:

- **Zero-size pty from piped ssh clients.** `ssh -tt` sizes the remote
  pty from the client's local terminal. With piped client stdin there is
  no local terminal, so ssh requests a 0-row/0-col pty, and a 0-cell
  canvas paints nothing — the app runs, serves input, and exits, but
  never writes a frame. `WithFallbackSize` does not help here because a
  pty exists and reports 0x0 as a legal size. Size the remote side
  yourself: `ssh -tt host 'stty rows 24 cols 80; my-tui-app'`.
- **Keystrokes racing raw mode.** Input written in the first milliseconds
  after spawn can arrive while the line discipline is still cooked, and
  `0x11`/`0x13` are then consumed as XON/XOFF flow control instead of
  reaching the app. Scripted drivers should send keys only after raw
  mode is up; humans typing interactively always qualify.
