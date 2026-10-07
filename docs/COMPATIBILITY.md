# Compatibility

Snapshot: **2026-10-07**. Entries describe the code currently present, not a
release certification. Automated tests cover rendering, encoder, parser, and
negotiation logic; the real-terminal matrix below remains to be executed and
published.

Legend: **implemented** = code path exists; **degraded** = conservative/basic
behavior; **planned validation** = plausible path without current evidence.

| Environment | Current status | Expected behavior / limit |
|---|---|---|
| Windows Terminal + ConPTY | implemented, validation pending | VT output, raw input through `x/term`, 100 ms resize polling |
| Windows 10+ ConHost in cmd/PowerShell | degraded, validation pending | attempts `ENABLE_VIRTUAL_TERMINAL_PROCESSING`; modern mouse/input behavior depends on console support |
| Windows 7/8 legacy console | not supported | no published fallback to classic screen-buffer rendering |
| Unix PTYs on Linux/macOS/BSD | implemented, validation pending | termios raw mode, VT output, `SIGWINCH` resize |
| tmux | planned validation | legacy VT path should work; the probe hears whatever answers first — inside tmux that can be tmux itself — so a kitty flags reply is trusted as-is, and the backend does not yet distinguish the outer emulator from the multiplexer |
| GNU screen | planned validation | basic VT/ANSI path only; advanced protocols are not enabled |
| SSH/mosh | planned validation | negotiation runs before the first frame over the same transport; slow links can spend the whole 150 ms budget (raise `LUX_TUI_PROBE_TIMEOUT`), and impaired-link validation remains |
| CI, redirected stdout, `TERM=dumb` | implemented | monochrome newline-delimited full snapshots; no cursor addressing/input |

Interactive controls require an interactive terminal profile. Every pointer
action in the built-in controls has a keyboard equivalent because mouse support
is environment-derived rather than actively probed. Generic VT transports
provide committed UTF-8 text, not a portable IME preedit/candidate lifecycle.
PowerShell and cmd remain shells; their host terminal determines behavior.

TextInput is EGC-correct under the currently bundled uniseg Unicode 15.0 data.
Terminal/font width, RTL visual editing, locale-tailored words, IME preedit,
password hardening, undo, and multiline are outside the current claim. Paste is
bracketed/bounded by the adapter and then sanitized/bounded again by the field.
LUX TUI does not read/write OSC 52 automatically; clipboard mutation requires
an explicit application-supplied `Clipboard` implementation. No OS adapter is
bundled; remote OSC 52 remains outside core and needs capability/user policy.

Scoped shortcuts compare exact key code, rune, and modifiers while ignoring
repeat count and the press/repeat/release action: a shortcut fires on the
activation, once. Legacy VT aliases (Tab/Ctrl-I, Enter/Ctrl-M) remain
indistinguishable on the legacy path; once the kitty protocol is positively
negotiated, a terminal that reports the pairs as CSI u decodes them into
distinct events. Do not rely on the split until the terminal has answered the
probe.

`cmd.exe`, PowerShell, and shells running under SSH are command interpreters,
not terminal emulators. Compatibility decisions must be based on the actual TTY
and capabilities, never the shell name alone.

## Current capability behavior

The adapter records interactive mode, Unicode, color depth, alternate screen,
cursor addressing, mouse, focus, bracketed paste, enhanced keyboard,
synchronized output, hyperlinks, and graphics. Today:

- redirected output or `TERM=dumb` selects the portable monochrome profile;
- the enhanced (kitty) keyboard stays off until the probe's kitty query is
  positively answered — nothing about the legacy key path changes before
  that;
- `NO_COLOR` disables color;
- `COLORTERM=truecolor` or `24bit` selects true color;
- a `TERM` containing `256color` selects the 256-color palette;
- otherwise an interactive VT uses ANSI 16 colors;
- graphics remain cell-based;
- the terminal cursor stays hidden unless a frame requests a caret, and the
  plain profile ignores caret requests entirely;
- synchronized output is opt-in with `LUX_TUI_SYNC_OUTPUT=1` (also accepts
  `true`, `yes`, or `on`).

## Startup negotiation

Open probes an interactive terminal once, in raw mode, before the alternate
screen is entered, so the probe meets a quiet stream: the only reports in
flight are the ones it asked for. One write — primary device attributes
(`CSI c`) plus DECRQM for DEC private mode 6 (DECOM, the proxy for DECSTBM
scroll regions) and mode 2026 (synchronized output), plus the kitty keyboard
query (`CSI ? u`) — followed by a bounded read:

- the default budget is 150 ms; terminals that answer usually do so in
  single-digit milliseconds, and a terminal that answers DA1 but not DECRQM
  keeps the probe alive until the deadline, which startup pays once;
- once the classic trio (DA1 and both mode reports) has answered, a 25 ms
  grace replaces the rest of the budget for the kitty reply: fragmented
  replies still land, while a protocol-less terminal pays one extra 25 ms
  instead of the full window;
- `LUX_TUI_PROBE_TIMEOUT` sets the budget in milliseconds; `0`, a negative
  number, `off`, `false`, or `no` disables negotiation entirely (deterministic
  runs, terminals that answer nothing — including the kitty query, so it
  doubles as the escape hatch for a misbehaving multiplexer); an invalid value
  falls back to the default after a one-line notice on stderr;
- the mode probes can only veto: only a mode report that positively rejects a
  capability removes it from the snapshot (a "permanently reset" mode 2026
  counts, because the set-mode wrapper would be a permanent no-op). A silent
  terminal, a lost reply, or a partial report keeps the defaults, so a dead
  link can never corrupt output;
- the kitty query is the one enable-only verdict: a flags reply
  (`CSI ? flags u`) turns the enhanced keyboard on; silence or junk keeps the
  legacy path;
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

The verdict trusts the first answerer, so a multiplexer answering on behalf
of a terminal without the protocol (or the reverse) is accepted for now;
distinguishing them is deferred to DA1-based provenance. Disabling
negotiation with `LUX_TUI_PROBE_TIMEOUT=off` restores the pure legacy path.

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
- Keep the implemented normal/error/panic restoration checks green; add
  explicit signal and suspend/resume handling before v1.
- Treat the effective chain separately: outer emulator → multiplexer →
  transport → application.
- Gate OSC, clipboard, notifications, and graphics passthrough by positive
  evidence.
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
| Lifecycle | resize storm, writer failure, cancellation, panic cleanup, suspend/resume |

Compatibility evidence belongs in reproducible scripts or CI artifacts, not in
an anecdotal “works on my terminal” table.
