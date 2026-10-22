#!/usr/bin/env bash
# Interleaved A/B of the micro benchmark battery: baseline worktree vs HEAD.
# usage: regress.sh <baseline-tree> <head-tree> <rounds> <outdir>
set -euo pipefail
base=$1 head=$2 rounds=$3 out=$4
pkgs="./render ./ui ./internal/terminal ./scene3d"
: >"$out/before.txt"; : >"$out/after.txt"
for r in $(seq "$rounds"); do
  for side in before after; do
    tree=$base; [ $side = after ] && tree=$head
    (cd "$tree" && go test -p 1 -count=1 -run '^$' -bench . -benchtime 300ms -benchmem $pkgs) >>"$out/$side.txt"
  done
  echo "round $r done $(date +%T)"
done
