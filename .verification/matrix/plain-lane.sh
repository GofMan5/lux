#!/bin/sh
# Plain-output lanes: redirected stdout (no TTY) and TERM=dumb both take
# the documented plain-text fallback — no escape bytes ever leave the
# process, the app exits cleanly at stdin EOF, and the frame text (UTF-8,
# box-drawing included) lands in the file.
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/plain-lane.sh
set -e
cd /src
go build -o /tmp/showcase ./examples/06-showcase

# 1. pipe lane: stdout redirected to a file, stdin at EOF.
TERM=xterm-256color /tmp/showcase < /dev/null \
    > /src/.verification/matrix/pipe-out.txt 2> /src/.verification/matrix/pipe-err.txt
echo "pipe lane exit: $?"
bytes=$(wc -c < /src/.verification/matrix/pipe-out.txt)
echo "pipe lane bytes: $bytes"
if grep -q "$(printf '\033')" /src/.verification/matrix/pipe-out.txt; then
    echo "FAIL: redirected stream carries escape bytes"
    exit 1
fi
test "$bytes" -gt 50            # a rendered frame came out
grep -q "LUX" /src/.verification/matrix/pipe-out.txt

# 2. TERM=dumb lane: same contract with the explicit dumb terminal.
TERM=dumb /tmp/showcase < /dev/null \
    > /src/.verification/matrix/dumb-out.txt 2>/dev/null
echo "dumb lane exit: $?"
if grep -q "$(printf '\033')" /src/.verification/matrix/dumb-out.txt; then
    echo "FAIL: TERM=dumb stream carries escape bytes"
    exit 1
fi
test "$(wc -c < /src/.verification/matrix/dumb-out.txt)" -gt 50
grep -q "LUX" /src/.verification/matrix/dumb-out.txt
echo "plain lane OK"
