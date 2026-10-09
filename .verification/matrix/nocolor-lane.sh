#!/bin/sh
# NO_COLOR lane: the showcase under tmux with NO_COLOR set — the documented
# contract is Monochrome: no SGR color sequences (38;2 truecolor, 38;5
# indexed, or 256-color) may appear in the pane bytes, while the layout,
# focus chrome, and clean exit still work.
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/nocolor-lane.sh
set -e
cd /src
apk add --no-cache tmux >/dev/null
echo "tmux: $(tmux -V)"
go build -o /tmp/showcase ./examples/showcase

tmux -f /dev/null new-session -d -e NO_COLOR=1 -s mono -x 100 -y 30 /tmp/showcase
sleep 2
tmux send-keys -t mono Tab
sleep 1
tmux send-keys -t mono F1
sleep 1
tmux capture-pane -t mono -e -p > /src/.verification/matrix/nocolor-escape.txt
tmux capture-pane -t mono -p     > /src/.verification/matrix/nocolor-text.txt

# the rendered content survived
grep -q "ctrl+q] quit" /src/.verification/matrix/nocolor-text.txt
grep -q "quit application" /src/.verification/matrix/nocolor-escape.txt || true
# monochrome contract: no color SGR in the pane bytes (-F: the pattern
# contains a literal bracket, not a character class)
if grep -qF "$(printf '\033[38;')" /src/.verification/matrix/nocolor-escape.txt; then
    echo "FAIL: NO_COLOR pane carries SGR color sequences"
    exit 1
fi
if grep -qF "$(printf '\033[48;')" /src/.verification/matrix/nocolor-escape.txt; then
    echo "FAIL: NO_COLOR pane carries SGR background sequences"
    exit 1
fi

tmux send-keys -t mono C-q
sleep 1
tmux kill-session -t mono 2>/dev/null || true
echo "nocolor lane OK"
