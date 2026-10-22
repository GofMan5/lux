#!/bin/sh
# GNU screen lane: the showcase inside a detached screen session, driven
# with stuffed raw bytes (stuff sends bytes, not keys — F1 is its terminfo
# sequence \033[11~ for TERM=screen), captured with hardcopy, quit with
# ctrl+q (byte 0x11).
#
# Host-side invocation (from the repo root):
#   docker run --rm -v <repo>:/src -w /src -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/screen-lane.sh
set -e
cd /src
apk add --no-cache screen >/dev/null
echo "screen: $(screen --version 2>&1 | head -n1)"
go build -o /tmp/showcase ./examples/06-showcase

# UTF-8 in .screenrc: screen defaults to latin1 unless told otherwise.
printf 'defutf8 on\n' > /tmp/screenrc
screen -c /tmp/screenrc -dmS lane /tmp/showcase
sleep 2

screen -S lane -X stuff "$(printf '\033[11~')"   # F1 — open the help dialog
sleep 1
screen -S lane -p 0 -X hardcopy /src/.verification/matrix/screen-help.txt
screen -S lane -X stuff "$(printf '\033')"       # Esc — close it again
sleep 1
screen -S lane -X stuff "$(printf '\011')"       # Tab — move focus
sleep 1
screen -S lane -p 0 -X hardcopy /src/.verification/matrix/screen-pane.txt

# assertions: help opened on the terminfo F1 sequence, footer survived,
# and the pane is substantial (not a blank crash).
grep -q "quit application" /src/.verification/matrix/screen-help.txt
grep -q "ctrl+q] quit"     /src/.verification/matrix/screen-pane.txt
test "$(wc -c < /src/.verification/matrix/screen-pane.txt)" -gt 400

screen -S lane -X stuff "$(printf '\021')"       # ctrl+q
sleep 2
if screen -ls 2>/dev/null | grep -q 'lane'; then
    echo "FAIL: showcase did not exit on ctrl+q"
    screen -S lane -X quit
    exit 1
fi
echo "screen lane OK"
