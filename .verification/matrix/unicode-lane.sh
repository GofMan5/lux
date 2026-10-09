#!/bin/sh
# Unicode lane: CJK ideographs, emoji with ZWJ grapheme clusters, and
# regional-indicator flags typed into the showcase's text input through a
# real tmux pane — the bytes cross a real kernel pty, get parsed into
# grapheme clusters by the editor, and render back through tmux.
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/unicode-lane.sh
set -e
cd /src
apk add --no-cache tmux >/dev/null
echo "tmux: $(tmux -V)"
go build -o /tmp/showcase ./examples/showcase

tmux -f /dev/null new-session -d -s uni -x 110 -y 32 /tmp/showcase
sleep 2

# Type into the Project-name input (the interaction panel owns initial
# focus): CJK, a simple emoji, a ZWJ family cluster, and a flag.
tmux send-keys -t uni -l "世界"
sleep 1
tmux send-keys -t uni -l " 🎉"
sleep 1
tmux send-keys -t uni -l " 👨‍👩‍👧"
sleep 1
tmux send-keys -t uni -l " 🇺🇦"
sleep 1
tmux capture-pane -t uni -p > /src/.verification/matrix/unicode-pane.txt

grep -q "世界"        /src/.verification/matrix/unicode-pane.txt
grep -q "🎉"          /src/.verification/matrix/unicode-pane.txt
grep -q "👨‍👩‍👧"   /src/.verification/matrix/unicode-pane.txt
grep -q "🇺🇦"       /src/.verification/matrix/unicode-pane.txt

tmux send-keys -t uni C-q
sleep 1
tmux kill-session -t uni 2>/dev/null || true
echo "unicode lane OK"
