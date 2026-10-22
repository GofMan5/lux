#!/bin/sh
# OpenSSH lane: sshd and the ssh client both inside the container; the
# showcase runs in a pty allocated by ssh -tt, its bytes cross the SSH
# transport (wire format: channel windows, heartbeats — a genuinely
# different transport than a local pty), and the client side pipes the
# keystrokes. Asserts the full-screen handshake, frame content, and the
# leave sequence on exit.
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e TERM=xterm-256color \
#       -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/ssh-lane.sh
set -e
cd /src
apk add --no-cache openssh openssh-client-default >/dev/null
go build -o /tmp/showcase ./examples/06-showcase

ssh-keygen -A >/dev/null 2>&1
mkdir -p /root/.ssh
ssh-keygen -t ed25519 -f /root/.ssh/id_ed25519 -N '' >/dev/null 2>&1
cp /root/.ssh/id_ed25519.pub /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys
/usr/sbin/sshd
sleep 1                      # sshd daemonizes; let the listener bind port 22
echo "sshd: $(pgrep -f /usr/sbin/sshd | wc -l) processes"

set +e
# `ssh -tt` sizes the remote pty from the client's local terminal; with a
# piped client stdin there is no local terminal, so ssh requests a 0x0 pty
# and a 0-cell canvas paints nothing (the frame loop idles, no bytes cross
# the wire, and the lane sees a bare transport). Sizing the pty on the
# remote side before the showcase starts fixes the canvas: frames render,
# and the size change arrives as a regular SIGWINCH resize.
(printf '\011'; sleep 2; printf '\021'; sleep 2) | timeout 30 \
    ssh -tt -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR root@127.0.0.1 \
        'stty rows 24 cols 80; /tmp/showcase' \
    > /src/.verification/matrix/ssh-wire.raw 2>/dev/null
status=$?
set -e
echo "ssh lane exit: $status"
test "$status" -eq 0 || exit 1

wire=$(cat /src/.verification/matrix/ssh-wire.raw)
# full-screen handshake and the leave on exit crossed the wire as raw bytes
printf '%s' "$wire" | grep -qF "$(printf '\033[?1049h')"   || { echo "FAIL: no enter alt screen"; exit 1; }
printf '%s' "$wire" | grep -qF "$(printf '\033[?1049l')"   || { echo "FAIL: no leave alt screen"; exit 1; }
# text-only view for human review and content asserts: strip escape
# sequences first — on the raw wire, frame text is interleaved with cursor
# moves and color SGRs, so contiguous-word greps belong on the stripped view
sed -e $'s/\033\\[[0-9;?]*[a-zA-Z]//g' -e $'s/\033\\][^\007]*\007//g' \
    /src/.verification/matrix/ssh-wire.raw \
    > /src/.verification/matrix/ssh-text.txt
text=$(cat /src/.verification/matrix/ssh-text.txt)
printf '%s' "$text" | grep -q "LUX TUI"                    || { echo "FAIL: no frame content"; exit 1; }
printf '%s' "$text" | grep -q "terminal graphics"          || { echo "FAIL: no subtitle content"; exit 1; }
bytes=$(wc -c < /src/.verification/matrix/ssh-wire.raw)
echo "ssh wire bytes: $bytes"
test "$bytes" -gt 1000
echo "ssh lane OK"
