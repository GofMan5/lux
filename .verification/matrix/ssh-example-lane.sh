#!/bin/sh
# examples/ssh lane: the tui-ssh-server built from examples/ssh serves the
# demo TUI over a real ssh connection; tmux plays the client terminal — it
# allocates the client-side pty, answers the engine's capability queries,
# sizes the session, and delivers keystrokes and resizes exactly like a
# user's terminal would. The ssh wire itself is encrypted, so the evidence
# is pane captures, not raw bytes.
#
# Asserts: the TUI appears with correct content at 80x24, a resize to
# 100x30 repaints the live size readout (SIGWINCH crossed tmux, the ssh
# window-change request, and the server's TIOCSWINSZ), 'q' propagates to
# exit status 0 on the ssh client, and an abrupt client death kills the
# remote child instead of leaking it.
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e TERM=xterm-256color \
#       -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/ssh-example-lane.sh
set -e
cd /src
apk add --no-cache openssh-client tmux >/dev/null
EVID=/src/.verification/matrix
SERVER_PID=

cleanup() {
    tmux kill-server 2>/dev/null || true
    test -n "$SERVER_PID" && kill "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# examples/ssh is a nested module: build and test from inside it.
( cd examples/ssh &&
  go build -o /tmp/tui-ssh-server . &&
  go test -count=1 -timeout 60s . ) | tee "$EVID/ssh-example-unit.txt"

rm -f /tmp/ssh_exit.txt
/tmp/tui-ssh-server -addr 127.0.0.1:2222 -password letmein \
    > "$EVID/ssh-example-server.log" 2>&1 &
SERVER_PID=$!
n=0
until grep -q "listening on" "$EVID/ssh-example-server.log" 2>/dev/null; do
    n=$((n+1)); test $n -lt 10 || { echo "FAIL: server did not start"; cat "$EVID/ssh-example-server.log"; exit 1; }
    sleep 1
done

# Password auth without a tty: OpenSSH >= 8.4 can be forced to use an
# askpass helper, so no sshpass and no expect are needed.
printf '#!/bin/sh\necho letmein\n' > /tmp/ask.sh
chmod +x /tmp/ask.sh
export SSH_ASKPASS=/tmp/ask.sh SSH_ASKPASS_REQUIRE=force DISPLAY=:0
SSHOPTS='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR'

# Session 1: the full demo TUI. The pane outlives the ssh client so the
# exit code can be read from the pane and captured afterwards.
tmux -f /dev/null new-session -d -s tui -x 80 -y 24 \
    "ssh $SSHOPTS -tt -p 2222 tui@127.0.0.1; echo SSH_EXIT=\$? > /tmp/ssh_exit.txt; sleep 60"

n=0
until tmux capture-pane -t tui -p 2>/dev/null | grep -q "LUX OVER SSH"; do
    n=$((n+1)); test $n -lt 30 || {
        echo "FAIL: TUI never appeared in the pane"
        tmux capture-pane -t tui -p > "$EVID/ssh-example-pane-80x24.txt" 2>/dev/null || true
        exit 1
    }
    sleep 1
done
tmux capture-pane -t tui -p > "$EVID/ssh-example-pane-80x24.txt"

grep -q "LUX OVER SSH"           "$EVID/ssh-example-pane-80x24.txt" || { echo "FAIL: no title"; exit 1; }
grep -q "80 x 24"                "$EVID/ssh-example-pane-80x24.txt" || { echo "FAIL: no 80x24 size readout"; exit 1; }
grep -q "tmux-256color"          "$EVID/ssh-example-pane-80x24.txt" || { echo "FAIL: TERM not propagated from client"; exit 1; }
grep -q "ssh -> pty -> LUX engine" "$EVID/ssh-example-pane-80x24.txt" || { echo "FAIL: no transport line"; exit 1; }

# Resize the client terminal: tmux resizes the client pty, ssh sends a
# window-change request, the server calls TIOCSWINSZ, the child gets
# SIGWINCH, and the app repaints its size readout.
tmux resize-window -t tui -x 100 -y 30
n=0
until tmux capture-pane -t tui -p 2>/dev/null | grep -q "100 x 30"; do
    n=$((n+1)); test $n -lt 15 || { echo "FAIL: resize never reached the app"; exit 1; }
    sleep 1
done
tmux capture-pane -t tui -p > "$EVID/ssh-example-pane-100x30.txt"
grep -q "100 x 30" "$EVID/ssh-example-pane-100x30.txt" || { echo "FAIL: no 100x30 readout"; exit 1; }

# 'q' must quit the demo TUI with exit status 0 on the ssh client side.
tmux send-keys -t tui q
n=0
until test -f /tmp/ssh_exit.txt; do
    n=$((n+1)); test $n -lt 15 || { echo "FAIL: ssh client never exited after q"; exit 1; }
    sleep 1
done
cp /tmp/ssh_exit.txt "$EVID/ssh-example-exit.txt"
tmux capture-pane -t tui -p > "$EVID/ssh-example-pane-final.txt" 2>/dev/null || true
grep -q "SSH_EXIT=0" "$EVID/ssh-example-exit.txt" || { echo "FAIL: exit status not 0: $(cat "$EVID/ssh-example-exit.txt")"; exit 1; }
tmux kill-session -t tui

# Session 2: abrupt client death must not leak the child. Killing the
# tmux session tears down the ssh client's connection; the server's
# session context is canceled and the child's process group is killed.
tmux -f /dev/null new-session -d -s tui2 -x 80 -y 24 \
    "ssh $SSHOPTS -tt -p 2222 tui@127.0.0.1"
n=0
until pgrep -f 'tui-ssh-server -child' >/dev/null 2>&1; do
    n=$((n+1)); test $n -lt 15 || { echo "FAIL: child never appeared"; ps; exit 1; }
    sleep 1
done
ps | grep 'tui-ssh-server -child' | grep -v grep > "$EVID/ssh-example-child-alive.txt" || true
test -s "$EVID/ssh-example-child-alive.txt" || { echo "FAIL: no child process recorded"; exit 1; }

tmux kill-session -t tui2
n=0
while pgrep -f 'tui-ssh-server -child' >/dev/null 2>&1; do
    n=$((n+1)); test $n -lt 15 || { echo "FAIL: child survived the disconnect"; ps; exit 1; }
    sleep 1
done
ps > "$EVID/ssh-example-child-gone.txt"
if ps | grep 'tui-ssh-server -child' | grep -v grep; then
    echo "FAIL: child still running after disconnect"; exit 1
fi

echo "ssh-example lane OK"
