#!/bin/sh
# annex lane: drive the examples/annex demo inside tmux with set-clipboard
# on, and prove the three sideband channels end to end.
#
#   - OSC 52 is proven BEHAVIORALLY: tmux (set-clipboard on) decodes the
#     clipboard annex into its own paste buffer, and `tmux show-buffer`
#     must return exactly the text the app sent.
#   - OSC 9;4 and OSC 9;777 are proven as byte truth: tmux implements
#     neither, so a real terminal that draws the taskbar bar or raises the
#     toast cannot be part of this container. What CAN be proven is the
#     exact bytes the app wrote to the pty: pipe-pane records every byte
#     the process emitted, before tmux's screen model ever sees it.
#   - The leave sequence must end with the OSC 9;4 reset — the taskbar
#     bar never outlives the program.
#
# Host-side invocation (from the repo root, Windows or Linux):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/annex-lane.sh
set -e
cd /src
apk add --no-cache tmux >/dev/null
echo "tmux: $(tmux -V)"
go build -o /tmp/annex-demo ./examples/annex

ESC=$(printf '\033')
BEL=$(printf '\007')
CLIP_B64=$(printf 'hello from lux OSC 52' | base64)

OUT=/src/.verification/matrix
# -f /dev/null: no user config interferes. The first session runs a
# placeholder so the server exists and set-clipboard can be armed BEFORE
# the demo writes its first OSC 52 — the option must be in effect when
# the sequence arrives, not after.
tmux -f /dev/null new-session -d -s annex -x 80 -y 24 'sleep 60'
tmux set-option -g set-clipboard on
tmux set-option -t annex remain-on-exit on
# pipe-pane records the pane's own output stream — the raw bytes the
# process wrote to the pty — from before the demo starts.
tmux pipe-pane -t annex 'cat >> /tmp/annex-output.raw'

tmux respawn-pane -k -t annex 'sh -c "echo $TERM > /tmp/pane-term; exec /tmp/annex-demo"'
sleep 1
echo "pane TERM: $(cat /tmp/pane-term)"
sleep 1

# Wait until the pane shows the demo chrome.
tmux capture-pane -t annex -p > $OUT/annex-pane-text.txt
grep -q "sideband annex" $OUT/annex-pane-text.txt

# c: queue a clipboard write. The next View flushes it, and tmux's
# set-clipboard turns the OSC 52 into the paste buffer.
tmux send-keys -t annex c
sleep 1
BUFFER=$(tmux show-buffer)
echo "tmux buffer: $BUFFER"
printf '%s' "$BUFFER" > $OUT/annex-buffer.txt
if [ "$BUFFER" != "hello from lux OSC 52" ]; then
    echo "FAIL: tmux paste buffer does not match the OSC 52 payload"
    tmux kill-session -t annex
    exit 1
fi

# n: queue a toast; a: bump the percent; four spaces: walk the progress
# state machine indeterminate → normal → error → paused. Every key lands
# in the raw capture on the frame that queued it, so each of the five
# OSC 9;4 payload shapes must be present.
tmux send-keys -t annex n
sleep 1
tmux send-keys -t annex a
sleep 1
tmux send-keys -t annex ' '
sleep 1
tmux send-keys -t annex ' '
sleep 1
tmux send-keys -t annex ' '
sleep 1
tmux send-keys -t annex ' '
sleep 1
tmux capture-pane -t annex -p > $OUT/annex-pane-after-keys.txt
grep -q "progress state: paused" $OUT/annex-pane-after-keys.txt
grep -q "percent           10" $OUT/annex-pane-after-keys.txt

# q: quit. The leave sequence must carry the OSC 9;4 reset. The raw
# capture is copied once the app is gone — stopping a pipe-pane on a
# dead pane fails in tmux, so the pipe simply dies with the session
# after the asserts have run.
tmux send-keys -t annex q
sleep 1
cp /tmp/annex-output.raw $OUT/annex-output.raw

# Byte-truth asserts on the raw pane output: the first frame's report
# (state none carries no percent field), the four walked states after
# [a] and four spaces, the toast, the clipboard write with its exact
# base64 payload, and the leave-side reset.
grep -qF "${ESC}]9;4;0${BEL}" $OUT/annex-output.raw
grep -qF "${ESC}]9;4;1;10${BEL}" $OUT/annex-output.raw
grep -qF "${ESC}]9;4;2;10${BEL}" $OUT/annex-output.raw
grep -qF "${ESC}]9;4;3;10${BEL}" $OUT/annex-output.raw
grep -qF "${ESC}]9;4;4;10${BEL}" $OUT/annex-output.raw
grep -qF "${ESC}]777;notify;Sideband;a desktop notification, queued from a frame${ESC}\\" $OUT/annex-output.raw
grep -qF "${ESC}]52;c;${CLIP_B64}${ESC}\\" $OUT/annex-output.raw
if ! tail -c 64 $OUT/annex-output.raw | grep -qF "${ESC}]9;4;0${BEL}"; then
    echo "FAIL: the leave sequence does not end with the OSC 9;4 reset"
    tmux kill-session -t annex
    exit 1
fi

# The demo must have exited cleanly on q.
if tmux list-panes -t annex -F '#{pane_dead}' | grep -qv 1; then
    echo "FAIL: annex demo did not exit on q"
    tmux kill-session -t annex
    exit 1
fi
EXIT_STATUS=$(tmux list-panes -t annex -F '#{pane_dead_status}')
echo "demo exit status: $EXIT_STATUS"
if [ "$EXIT_STATUS" != "0" ]; then
    echo "FAIL: annex demo exited with $EXIT_STATUS"
    tmux kill-session -t annex
    exit 1
fi
tmux kill-session -t annex
echo "annex lane OK"
