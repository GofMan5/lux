# LUX over SSH

A small SSH server that serves a LUX TUI. Connect with any ssh client and
you get the full engine: raw mode, capability probes negotiated end to end
with your terminal, live resize, and a clean exit status. No LUX internals
are involved on the server side — it is pure byte plumbing.

## Run it

```
go build .                        # inside examples/ssh
./tui-ssh-server -addr 127.0.0.1:2222
ssh -t -p 2222 localhost          # password: letmein
```

The demo TUI shows the pty size (updates live when you resize your
terminal), the TERM the client asked for, and quits on `q` or Escape with
exit status 0. A host key is generated at startup, so expect an
unknown-host prompt on first connect.

The server is also a generic pty forwarder: pass a command after `--` and
it serves that instead — any program that wants a terminal works:

```
./tui-ssh-server -- ../showcase/showcase
./tui-ssh-server -- /bin/sh
```

With no arguments it re-execs its own binary with `-child`, so one build
serves both roles.

## How it works

Each SSH session is bridged onto a real pty via
[creack/pty](https://github.com/creack/pty) (`StartWithSize`, which does
`setsid` + `TIOCSCTTY`), and the requested program runs there as the
session leader:

- **The engine runs in the child, unchanged.** The TUI binary sees a
  genuine terminal on its stdin/stdout — raw mode, probe replies, and
  SIGWINCH are handled by the same code paths as a local terminal.
- **Resize**: the client's window-change request becomes `TIOCSWINSZ` on
  the pty; the kernel delivers `SIGWINCH` to the (foreground) child.
- **Exit status**: the child's wait status is sent back as an SSH
  exit-status request, so `ssh` reports the TUI's real exit code.
- **Disconnect**: the connection context is canceled when the channel
  dies, and the child's process group (it is a session leader after
  `setsid`) is killed — nothing leaks.

The server uses [gliderlabs/ssh](https://github.com/gliderlabs/ssh):
handler + password check, auto-generated host key. Two goroutines per
session pump bytes (keystrokes in, frames out); that is the whole server.

## Why a subprocess instead of driving the engine in-process

The LUX backend contract binds to an `*os.File` terminal and relies on
process-group and session semantics (suspend, foreground signals, probe
reads). Serving in-process would mean faking those behind a network
channel and maintaining a parallel "remote" code path. Running the engine
in a child on a real pty keeps the contract honest: the SSH layer is
plain byte forwarding, and everything terminal-specific stays in the
engine, tested by the same real-terminal matrix as everything else.

## Caveats

- **Unix only.** creack/pty has no Windows port; the files carry
  `//go:build !windows`.
- **Password auth is a demo.** `letmein` for everyone. Real deployments
  should swap in public-key handlers.
- **Signals are not forwarded.** `ssh` client signals (SIGINT etc.) would
  need `Signals(chan<- Signal)` plumbing; a disconnect already kills the
  child, so Ctrl-C at the client just closes the connection. Intentional
  scope cut for an example.
- **Zero-size ptys.** A piped `ssh -t` (no local terminal on the client
  side) requests a 0x0 pty; the app runs but paints nothing until the
  pty is sized (`stty rows R cols C`). See
  [docs/COMPATIBILITY.md](../../docs/COMPATIBILITY.md).

## Verification

Unit tests fake the session layer (including gliderlabs' newline munging
and the DSR query a real terminal answers) and drive `sh` on a real pty:
size/TERM propagation, exit codes, resize, disconnect-kill.

The end-to-end lane
[`.verification/matrix/ssh-example-lane.sh`](../../.verification/matrix/ssh-example-lane.sh)
runs the server and connects with a real OpenSSH client inside tmux: the
pane captures prove content at 80x24, repaint at 100x30, `q` → exit
status 0, and no leaked child after an abrupt disconnect.
