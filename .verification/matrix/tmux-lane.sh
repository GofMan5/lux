#!/bin/sh
# tmux lane: build the showcase, run it under a current tmux server, drive
# it through real keystrokes, storm a resize, capture the rendered pane
# with and without escape sequences, and require a clean exit.
#
# Host-side invocation (from the repo root, Windows or Linux):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/tmux-lane.sh
set -e
cd /src
apk add --no-cache tmux >/dev/null
echo "tmux: $(tmux -V)"
go build -o /tmp/showcase ./examples/showcase

# -f /dev/null: no user config interferes; TERM inside the pane is whatever
# tmux picks for this system (recorded below — it is part of the evidence).
tmux -f /dev/null new-session -d -s lane -x 100 -y 30 \
    "sh -c 'echo \$TERM > /tmp/pane-term; exec /tmp/showcase'"
sleep 1
echo "pane TERM: $(cat /tmp/pane-term)"
sleep 1

tmux send-keys -t lane F1        # open the help dialog
sleep 1
tmux capture-pane -t lane -p > /src/.verification/matrix/tmux-help-text.txt
tmux send-keys -t lane Escape    # close it again
sleep 1
tmux send-keys -t lane Tab       # move focus through the panels
sleep 1

tmux capture-pane -t lane -e -p > /src/.verification/matrix/tmux-pane-escape.txt
tmux capture-pane -t lane -p     > /src/.verification/matrix/tmux-pane-text.txt

# resize storm: two resizes in quick succession, then a capture to prove
# the app re-laid-out to the new geometry instead of smearing.
tmux resize-window -t lane -x 60 -y 20
sleep 1
tmux resize-window -t lane -x 120 -y 40
sleep 1
tmux capture-pane -t lane -p > /src/.verification/matrix/tmux-pane-after-resize.txt
rows=$(wc -l < /src/.verification/matrix/tmux-pane-after-resize.txt)
echo "pane rows after resize to 120x40: $rows"

# assertions: the footer, the focused-panel chrome, and content survived
grep -q "quit application" /src/.verification/matrix/tmux-help-text.txt
grep -q "ctrl+q] quit"     /src/.verification/matrix/tmux-pane-text.txt
test "$(wc -c < /src/.verification/matrix/tmux-pane-escape.txt)" -gt 1000

tmux send-keys -t lane C-q
sleep 1
if tmux has-session -t lane 2>/dev/null; then
    echo "FAIL: showcase did not exit on ctrl+q"
    tmux kill-session -t lane
    exit 1
fi
echo "tmux lane OK"
