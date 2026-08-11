# Compatibility

Snapshot: **2026-08-11**. Entries describe the code currently present, not a
release certification. Automated tests cover pure rendering and parser logic;
the real-terminal matrix below remains to be executed and published.

Legend: **implemented** = code path exists; **degraded** = conservative/basic
behavior; **planned validation** = plausible path without current evidence.

| Environment | Current status | Expected behavior / limit |
|---|---|---|
| Windows Terminal + ConPTY | implemented, validation pending | VT output, raw input through `x/term`, 100 ms resize polling |
| Windows 10+ ConHost in cmd/PowerShell | degraded, validation pending | attempts `ENABLE_VIRTUAL_TERMINAL_PROCESSING`; modern mouse/input behavior depends on console support |
| Windows 7/8 legacy console | not supported | no published fallback to classic screen-buffer rendering |
| Unix PTYs on Linux/macOS/BSD | implemented, validation pending | termios raw mode, VT output, `SIGWINCH` resize |
| tmux | planned validation | legacy VT path should work; no version-aware passthrough or capability provenance yet |
| GNU screen | planned validation | basic VT/ANSI path only; advanced protocols are not enabled |
| SSH/mosh | planned validation | custom backends and latest-frame pacing exist; terminal-chain capability provenance and impaired-link validation remain |
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
repeat count. Legacy VT aliases (Tab/Ctrl-I, Enter/Ctrl-M, Backspace/Ctrl-H)
remain indistinguishable; do not register them as separate commands until a
modern keyboard protocol has been positively negotiated.

`cmd.exe`, PowerShell, and shells running under SSH are command interpreters,
not terminal emulators. Compatibility decisions must be based on the actual TTY
and capabilities, never the shell name alone.

## Current capability behavior

The adapter records interactive mode, Unicode, color depth, alternate screen,
cursor addressing, mouse, focus, bracketed paste, synchronized output,
hyperlinks, and graphics. Today:

- redirected output or `TERM=dumb` selects the portable monochrome profile;
- `NO_COLOR` disables color;
- `COLORTERM=truecolor` or `24bit` selects true color;
- a `TERM` containing `256color` selects the 256-color palette;
- otherwise an interactive VT uses ANSI 16 colors;
- graphics remain cell-based;
- the terminal cursor stays hidden unless a frame requests a caret, and the
  plain profile ignores caret requests entirely;
- synchronized output is opt-in with `LUX_TUI_SYNC_OUTPUT=1` (also accepts
  `true`, `yes`, or `on`).

No active terminal queries are emitted yet. That avoids startup stalls and
unconsumed replies, but it also means environment mistakes can produce a poor
profile.

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

- Store capability values as known/probed/overridden rather than one optimistic
  boolean when active negotiation is added.
- Bound startup probing to a short interactive budget and never wait seconds
  for an optional reply.
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
