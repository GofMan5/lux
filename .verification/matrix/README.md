# Terminal Validation Matrix

Snapshot: **2026-10-09**.

Real-terminal evidence for LUX TUI stage 9, slice 3. Every lane below ran
against an actual terminal or terminal-shaped transport and left its
artifacts in this directory. Nothing here is a simulated or mocked
terminal: multiplexers allocate a pty, the ssh lane crosses a real
sshd/ssh pair, the netem lane shapes the loopback that carries it, and the
Windows lanes drive Windows Terminal and conhost on the host itself.

Each lane script asserts its own success and exits non-zero on any failed
needle; the evidence files are the raw captures those asserts ran on.

## Environments

| Role | Environment |
| --- | --- |
| Docker lanes | golang:1.25-alpine, tmux 3.7c, Screen 5.0.2, OpenSSH 10.3p1 (OpenSSL 3.5.7), util-linux script 2.42.3, strace 6.19, iproute2 tc 7.0.0, Docker 29.8.0 |
| Windows lanes | Windows 11 (NT 10.0.26200), Windows Terminal 1.24.12741.0, classic conhost (headless), Windows PowerShell 5.1 |
| Build | Go 1.25.5, windows/amd64 host; linux/amd64 in containers |

## Matrix

| Lane | Transport | Result | Evidence |
| --- | --- | --- | --- |
| tmux 3.7c | pty inside a multiplexer that answers capability queries | PASS | `tmux-pane-text.txt`, `tmux-pane-escape.txt`, `tmux-pane-after-resize.txt`, `tmux-help-text.txt` |
| GNU Screen 5.0.2 | pty inside a multiplexer (older DEC-style feature set) | PASS | `screen-pane.txt`, `screen-help.txt` |
| pipe | stdout redirected to a file, TERM set | PASS | `pipe-out.txt`, `pipe-err.txt` |
| dumb | TERM=dumb, output redirected | PASS | `dumb-out.txt` |
| unicode | pty via tmux, wide/CJK/emoji content | PASS | `unicode-pane.txt` |
| NO_COLOR | pty via tmux with NO_COLOR=1 | PASS | `nocolor-text.txt`, `nocolor-escape.txt` |
| ssh | pty over real sshd/ssh pair (127.0.0.1, `-tt`) | PASS | `ssh-wire.raw`, `ssh-text.txt` |
| netem | ssh loopback shaped: 200 ms delay, 512 kbit/s | PASS | `netem-wire.raw`, `netem-probe-trace.txt` |
| Windows Terminal 1.24 | interactive real window, scripted F1/TAB/edit/quit | PASS | `windows-terminal-main.png`, `windows-terminal-help.png` |
| conhost (headless) | classic conhost lifecycle, transcript capture | PASS | `conhost-transcript.bin` |
| Windows pipe | stdout redirected on the host | PASS | `windows-pipe-out.txt` |

## Lane scripts and how to re-run

All Unix-side lanes run in the same container shape (from the repo root):

```
docker run --rm -v <repo>:/src -w /src -e TERM=xterm-256color \
    -e GOFLAGS=-buildvcs=false golang:1.25-alpine \
    sh /src/.verification/matrix/<lane>.sh
```

`netem-lane.sh` additionally needs `--cap-add NET_ADMIN` for `tc`.
The ssh and netem lanes build their own sshd inside the container.

Windows lanes run on the host with Windows PowerShell 5.1:

```
powershell -NoProfile -ExecutionPolicy Bypass -File .verification\matrix\windows-terminal-lane.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .verification\matrix\conhost-lane.ps1
```

The WT lane scripts a real Windows Terminal window through F1 help, ESC,
TAB focus, an edit keystroke, and ctrl+q quit, taking screenshots along
the way and asserting the exit code. The conhost lane drives a headless
classic conhost and asserts the full alt-screen/focus/mouse/cursor
lifecycle byte-for-byte in the transcript, plus the redirected-output
plain profile. Both kill their launchers before and after (best effort).

## What each lane proves

- **tmux/screen**: a full-featured multiplexer answers the capability
  probe; frames, resize redraw, help dialog, and quit all behave, and the
  escape capture shows the wire protocol (sync, DECSET, title, alt screen).
- **pipe/dumb/Windows pipe**: a non-terminal stdout selects the plain
  output profile — one UTF-8 snapshot, no alt screen, no escape storm,
  clean exit code. `dumb` degrades to ASCII rules the same way.
- **unicode**: wide runes and emoji occupy the right cell counts on a real
  pty; no mojibake in the pane text.
- **NO_COLOR**: the honor-NO_COLOR profile strips SGR color while keeping
  layout (the escape capture is the proof: structure without color).
- **ssh**: full-screen mode crosses a real ssh transport — alt-screen
  enter and leave, frame content, and a clean exit with the leave
  sequence intact on the wire.
- **netem**: the same transport shaped to 200 ms / 512 kbit. Input still
  gets serviced (the scripted quit key works), frames still flow, and the
  run completes on its own — no freeze from blocking writes. The probe
  budget is measured with strace write(2) timestamps: with
  `LUX_TUI_PROBE_TIMEOUT=400` the alt-screen enter waits 0.402 s after
  the first probe write, versus 0.151 s with the default 150 ms budget —
  the knob does exactly what the docs claim.
- **WT/conhost/Windows pipe**: the Windows trio. A real interactive WT
  window (screenshots included), a classic conhost lifecycle transcript
  (headless), and the redirected plain profile.

## Findings worth keeping

- **Zero-size pty from piped ssh clients.** `ssh -tt` sizes the remote
  pty from the client's local terminal; with piped client stdin there is
  none, so ssh requests a 0-row/0-col pty. A 0-cell canvas paints
  nothing — the wire shows the handshake and then silence. This is
  terminal-integration reality, not a bug: size the pty on the remote
  side (`stty rows 24 cols 80`) before starting the TUI. The ssh and
  netem lanes do exactly that, and any user driving a TUI through piped
  ssh needs the same trick.
- **Keys sent at t=0 race raw mode.** A quit byte in the first
  milliseconds can arrive while the line discipline is still cooked, and
  0x11/0x13 are then eaten as XON/XOFF flow control rather than read as
  input. Under strace (which slows startup) this reproduces reliably.
  Test harnesses should send scripted keys after raw mode is up (the
  lanes sleep first); real users type later than that anyway.

## Not validated, on purpose

| Environment | Why | How to close |
| --- | --- | --- |
| macOS Terminal.app, iTerm2, kitty, alacritty, wezterm, foot, GNOME Terminal | No macOS host and none of these emulators on the Windows host | Run the same lane scripts on a machine with each emulator; the Docker-side lanes already cover the pty/wire layer these share |
| Interactive conhost window | Classic conhost cannot be launched as a targetable interactive window without registry console-delegation GUIDs, which are host policy | Record a manual session over the headless-verified protocol, or run on a host where console delegation is preconfigured |
| Real network latency (not loopback netem) | Requires a remote machine | Repeat the ssh lane against one with the same asserts |

The headless conhost lane still covers the full enter/leave byte
protocol against classic conhost; only the interactive window experience
is uncovered.
