#!/bin/sh
# One-command reproducible benchmark run inside golang:1.26-alpine.
# Invoked by the documented docker command; safe to re-run.
set -e

cd /src/benchmarks

# Record the docker image the numbers were produced in (machine.json).
export BENCH_IMAGE="${BENCH_IMAGE:-golang:1.26-alpine}"

# creack/pty is benchmark-harness-only and lives in this nested module so
# the root lux module keeps its <=3 dependency budget.
go get github.com/creack/pty
go mod tidy
go build -o /tmp/bench .

/tmp/bench -out /src/benchmarks/results "$@"
