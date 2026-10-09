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

Quick smoke run (~2-3 min, one round, 120x40 only, not citable):

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  -e TERM=xterm-256color -e COLORTERM=truecolor -e BENCH_IMAGE=golang:1.26-alpine \
  golang:1.26-alpine sh benchmarks/run.sh -quick
```

A full run is ~25-35 minutes on one core-set and should be run on an
otherwise idle machine.

## What is measured

One child process per (lane, scene, size, round), spawned as a session
leader with a fresh pty pair. The framework renders to the pty slave; the
harness writes identical key-byte streams and resizes to the pty master;
SIGWINCH is real. Per run: wire bytes and burst count (output groups
separated by >=4 ms of silence), the lane's own paint/draw counters, CPU
time and allocations via rusage + MemStats around the measured phase only,
2 s idle-window CPU, input-to-last-output latency where the scene defines
it, and burst-to-burst frame gaps.

Scenes (identical cell content in every lane):

| # | scene | drives | stresses |
|---|---|---|---|
| 1 | static | paced keys | steady-state idle diff |
| 2 | onecell | paced keys | one changed cell per frame |
| 3 | rtt | paced keys + settle | round-trip latency |
| 4 | damage | paced keys | ~10% scattered cell changes |
| 5 | resize | alternating sizes | full repaints on SIGWINCH |
| 6 | inputburst | key bursts | coalescing under load |
| 7 | full | paced keys | every cell changes every frame |
| 8 | list | paced keys | 10k-row scrolling window |
| 9 | egc | paced keys | grapheme clusters, combining marks, ZWJ, flags |
| 10 | subcell | paced keys | lux-only shaded 3D cube (scene3d) |

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
