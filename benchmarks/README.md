# lux benchmarks

Reproducible, one-command performance comparison of **lux** against the two
most-used Go TUI frameworks — [bubbletea](https://github.com/charmbracelet/bubbletea)
(tea) and [tview](https://github.com/rivo/tview) — on identical scenes,
identical input byte streams, and real pseudo-terminals.

This is a nested Go module (`github.com/GofMan5/lux/benchmarks`): competitor
dependencies and Linux-only harness syscalls stay out of the root lux module,
which keeps its `<=3` dependency budget intact.

## Run it

From the repository root:

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  -e TERM=xterm-256color -e COLORTERM=truecolor -e BENCH_IMAGE=golang:1.26-alpine \
  golang:1.26-alpine sh benchmarks/run.sh
```

That single command builds everything, runs the full suite, and writes
`benchmarks/results/`:

- `report.md` — the human report (medians over 3 rounds per cell)
- `benchstat.txt` — greppable `Benchmark<Lane><Scene>WxH-<cores>` metrics
- `all-runs.json`, `raw/*.json` — every raw run, committed as evidence
- `machine.json` — what the numbers were measured on
- `logs/*.log` — child stdout/stderr

`golang:1.26-alpine` is required: the tea lane uses bubbletea v2, which needs
Go >= 1.26. The `luxbenchmod` volume caches the module cache across runs;
the first run pays one `go get`/`go mod tidy` for the competitor set.

Quick smoke run (~7 min on the recorded host, one round, 120x40 only,
not citable):

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  -e TERM=xterm-256color -e COLORTERM=truecolor -e BENCH_IMAGE=golang:1.26-alpine \
  golang:1.26-alpine sh benchmarks/run.sh -quick
```

A full run is about 55 minutes on the recorded host and should be run on
an otherwise idle machine. The default suite has 174 runs: nine comparable
scenes across three lanes, plus two lux-only scenes, at two sizes and three
rounds. Filter scenes without changing their pacing or round count:

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  golang:1.26-alpine sh benchmarks/run.sh -scenes tall,egc -out /src/benchmarks/results-targeted
```

Unknown names fail explicitly. `-scenes tall` runs six full measurements
(three rounds at each size); `-quick` is still smoke-only.

Rebuild only the derived artifacts (report.md, benchstat.txt) from an
existing `all-runs.json` without measuring anything:

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  golang:1.26-alpine sh -c "cd benchmarks; go build -o /tmp/bench .; \
  /tmp/bench -out /src/benchmarks/results -report-only"
```

The raw measurements in `all-runs.json`/`raw/*.json` are the truth; use
this when the report renderer changed after a run (it never rewrites the
raw files).

## What is measured

One child process per (lane, scene, size, round), spawned as a session
leader with a fresh pty pair. The framework renders to the pty slave; the
harness writes identical key-byte streams and resizes to the pty master;
SIGWINCH is real. Per run: wire bytes and burst count (output groups
separated by >=4 ms of silence), the lane's own paint/draw counters
(zeroed right after warmup, so they cover the measured phase only, like
the wire and CPU counters), CPU time and allocations via rusage +
MemStats around the measured phase only, 2 s idle-window CPU,
input-to-last-output latency where the scene defines it — the settle
wait requires output causally after the input, so silence from stale
output can never fake a settled action, and units without output are
counted separately instead of entering the latency distribution as 0 —
and burst-to-burst frame gaps.

Scenes 1–9 have identical logical cell content in every lane. Scenes 10–11
are lux-only diagnostics, not cross-framework comparisons:

| # | scene | drives | stresses |
|---|---|---|---|
| 1 | static | paced keys | steady-state idle diff |
| 2 | onecell | paced keys | one changed cell per frame |
| 3 | damage | paced keys | ~10% scattered cell changes |
| 4 | full | paced keys | every cell changes every frame |
| 5 | list | paced keys | 10k-row virtualized scrolling window |
| 6 | resize | alternating sizes | full repaints on SIGWINCH |
| 7 | inputburst | key bursts | coalescing under load |
| 8 | egc | paced keys | grapheme clusters, combining marks, ZWJ, flags |
| 9 | rtt | paced keys + settle | round-trip latency |
| 10 | subcell | paced keys | lux-only shaded 3D cube (scene3d) |
| 11 | tall | paced keys | lux-only, one precomputed 160-line document drawn with negative y through `Canvas.Text`, without virtualization |

`tall` intentionally prices measurement of the entire input plus drawing the
visible rows; `Text` returns the widest **input** line even when clipped.
Applications with huge documents should virtualize, as `list` does. A separate
`render.BenchmarkText/tall-overscroll` CPU benchmark uses 2,000 lines without
input pacing or idle-window assumptions.

Terminal sizes 120x40 and 240x80; three rounds per cell; medians reported.

## Honesty rules

- Numbers are only ever compared within one machine's run; nothing is
  copied across machines or from vendor pages.
- **ratatui is not measured** — it is a Rust crate, not a Go dependency;
  no cross-language numbers are invented.
- Paint semantics differ per lane and are reported as such: lux counts
  paints via the synchronous `Frame.OnPresented` callback; tview counts one
  paint per `Draw`+flush; tea coalesces to its renderer flush rate and
  reports `-1`, with wire bursts as the comparable fallback.
- Every lane renders through its own idiomatic API (lux row strings plus
  cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so
  the comparison measures frameworks, not applications.
- All tuning is defaults-only for every lane; nothing is hand-tuned.
- tview's `QueueUpdateDraw` blocks until drawn, so its burst-scene draws
  serialize — that is its real behavior and is reported, not hidden.
- tview runs through a small custom tcell `Tty` shim over the pty slave. It
  mirrors tcell's own `/dev/tty` device split — ioctls on one descriptor,
  reads through a non-blocking `dup` registered with Go's poller — because
  reads on the raw descriptor ignore read deadlines and would wedge
  teardown; the shim keeps teardown deterministic so it cannot skew the
  numbers.

## Layout

- `main.go` — parent: job fan-out, child spawning, report writing
- `child.go` — one measured cell in a subprocess
- `adapter.go` / `adapter_lux.go` / `adapter_tea.go` / `adapter_tview.go` — lane adapters
- `scene.go` — scene definitions, content, drive modes, pacing constants
- `pty.go` — pty sessions, master reader, burst counting, usage snapshots
- `metrics.go` — RunResult, aggregation, report and benchstat rendering
- `calibrate_test.go` — harness calibration tests: causal settle wait,
  phase-aligned counters, latency semantics, no-output accounting,
  `-report-only` regeneration
