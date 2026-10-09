#!/bin/sh
# Netem lane: a slow terminal, for real. `tc netem` shapes loopback network
# traffic, not pty file descriptors — so the slow path here is the same
# ssh setup as the ssh lane (sshd and client on 127.0.0.1) with the
# loopback shaped to 200 ms delay / 512 kbit rate. A TUI that blocks its
# event loop on terminal writes would freeze on this transport; the lane
# asserts the app keeps serving input (quit key honored), renders frames
# through the shaped pipe, and exits cleanly.
#
# Sub-lane 2 measures the startup probe budget on a local pty: with no
# terminal answering the capability probe, the alt-screen enter must wait
# out LUX_TUI_PROBE_TIMEOUT (400 ms here) before switching — the timing
# file of `script -t` makes the wait a measured number, not a claim.
#
# Host-side invocation (from the repo root):
#   docker run --rm --cap-add NET_ADMIN -v <repo>:/src -w /src \
#       -e TERM=xterm-256color -e GOFLAGS=-buildvcs=false \
#       golang:1.25-alpine sh /src/.verification/matrix/netem-lane.sh
set -e
cd /src
apk add --no-cache openssh openssh-client-default util-linux iproute2-tc >/dev/null
go build -o /tmp/showcase ./examples/showcase

ssh-keygen -A >/dev/null 2>&1
mkdir -p /root/.ssh
ssh-keygen -t ed25519 -f /root/.ssh/id_ed25519 -N '' >/dev/null 2>&1
cp /root/.ssh/id_ed25519.pub /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys
/usr/sbin/sshd
sleep 1
echo "sshd: $(pgrep -f /usr/sbin/sshd | wc -l) processes"

# Shape the loopback: 200 ms delay, 512 kbit/s — every probe round trip
# costs at least 400 ms, every frame burst trickles out.
tc qdisc add dev lo root netem delay 200ms rate 512kbit
tc qdisc show dev lo | grep -q netem || { echo "FAIL: netem qdisc not applied"; exit 1; }
echo "netem: $(tc qdisc show dev lo | tail -1)"

raw=/src/.verification/matrix/netem-wire.raw
start=$(date +%s)
set +e
(printf '\011'; sleep 2; printf '\021'; sleep 2) | timeout 60 \
    ssh -tt -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR root@127.0.0.1 \
        'stty rows 24 cols 80; /tmp/showcase' \
    > "$raw" 2>/dev/null
status=$?
set -e
end=$(date +%s)
echo "netem lane exit: $status"
test "$status" -eq 0 || exit 1

bytes=$(wc -c < "$raw")
echo "netem wire bytes: $bytes"
test "$bytes" -gt 1000
cat "$raw" | grep -qF "$(printf '\033[?1049h')" || { echo "FAIL: no enter alt screen"; exit 1; }
cat "$raw" | grep -qF "$(printf '\033[?1049l')" || { echo "FAIL: no leave alt screen"; exit 1; }
cat "$raw" | grep -q "LUX"                          || { echo "FAIL: no frame content"; exit 1; }

# the quit key was served while the transport was still shaped: the run
# finished on its own within the timeout, and the wall clock proves the
# input pipe stayed live rather than an instant stall (4 s of scripted
# input sleeps plus the 400 ms probe round-trip budget)
elapsed_s=$(( end - start ))
echo "netem run wall time: ${elapsed_s}s"
test "$elapsed_s" -ge 4

# --- Sub-lane 2: probe budget on a local pty ----------------------------
# No terminal answers the capability probe here, so the alt-screen enter
# must wait out LUX_TUI_PROBE_TIMEOUT. busybox script has no per-chunk
# timing, so the clock comes from strace: every write(2) gets an epoch
# timestamp, and the delta between the first probe write and the write
# carrying the alt-screen enter is the measured probe wait.
apk add --no-cache strace >/dev/null 2>&1
ts() { awk '{for (i = 1; i <= NF; i++) if ($i ~ /^[0-9]+\.[0-9]+$/) { print $i; exit }}'; }
run_budget() {
    # the quit key goes out only after raw mode is surely up: a 0x11 sent
    # at t=0 races MakeRaw and the cooked line discipline eats it as XON,
    # leaving the app spinning under strace forever
    (sleep 2; printf '\021'; sleep 3) | timeout -s KILL 30 \
        strace -f -ttt -s 200 -e trace=write -o /tmp/trace-$1.txt \
        script -qec "stty rows 24 cols 80; $2 /tmp/showcase" /tmp/netem-pty-$1.raw \
        > /dev/null 2>&1
    rc=$?
    pkill -f /tmp/showcase 2>/dev/null || true
    pkill -x script 2>/dev/null || true
    echo "probe-budget run ($1) exit: $rc" >&2
    t_probe=$(grep -F '33[c' /tmp/trace-$1.txt | head -1 | ts)
    t_enter=$(grep -F '1049h' /tmp/trace-$1.txt | head -1 | ts)
    test -n "$t_probe" || { echo "FAIL: no probe write traced ($1)" >&2; exit 1; }
    test -n "$t_enter" || { echo "FAIL: no enter write traced ($1)" >&2; exit 1; }
    awk -v a="$t_probe" -v b="$t_enter" 'BEGIN { printf "%.3f\n", b - a }'
}
budget_wait=$(run_budget 400 'LUX_TUI_PROBE_TIMEOUT=400')
default_wait=$(run_budget default '')
echo "probe wait with 400ms budget: ${budget_wait}s (default 150ms: ${default_wait}s)"
awk -v p="$budget_wait" 'BEGIN { exit !(p >= 0.35) }' || { echo "FAIL: probe did not wait out its budget"; exit 1; }
cp /tmp/trace-400.txt /src/.verification/matrix/netem-probe-trace.txt
echo "netem lane OK"
