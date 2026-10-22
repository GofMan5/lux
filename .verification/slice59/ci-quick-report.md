# lux benchmark report

Mode: quick smoke (1 round, 120x40 only, shrunken pacing — smoke-level, NOT CITABLE)

## Machine

- CPU: AMD EPYC 7763 64-Core Processor, 4 cores
- Memory: 15.6 GiB
- Kernel: 7.0.0-1017-azure x86_64
- Go: go1.26.10 (linux/amd64)
- Image: golang:1.26-alpine
- Collected: 2026-10-22T12:48:25Z

## Reproduce

From the repository root:

```sh
docker run --rm -v luxbenchmod:/go/pkg/mod -v "$PWD":/src -w /src \
  -e TERM=xterm-256color -e COLORTERM=truecolor -e BENCH_IMAGE=golang:1.26-alpine \
  golang:1.26-alpine sh benchmarks/run.sh
```

## How to read the lanes

- **events** = input keys (or resizes) driven; identical byte streams for every lane.
- **bursts** = wire truth: output read from the pty master, grouped by >=4 ms of silence.
- **paints** = lane's own paint counter; semantics differ per lane (see below), and `-1` means not comparable.
  - lux: synchronous `Frame.OnPresented` callback — one per presented frame.
  - tview: one paint per `Draw`+flush; redraws are serialized through `QueueUpdateDraw`.
  - tea: renderer coalesces to its flush rate, so paints are `-1`; compare **bursts** instead.
- **draws** = lane view/draw invocations (lux `View`, tea `View`, tview `Draw`).
- **CPU us/frame**, **ms/frame**, **bytes/frame**, **alloc B/frame** are divided by the median frame count (paints, or bursts when paints are `-1`); ms/frame includes pacing sleeps. A `—` means the lane produced no measurable frames (no paints, no wire output), so per-frame division has no honest denominator.
- **CPU p50/p95/p99 (us)** = per-frame process-CPU distribution, sampled through the process CPU clock at each presented frame (lux only; lanes without a presentation callback report `—`). The delta between two consecutive presents covers one full frame pipeline — input parse, update, view, compose, diff, encode, flush — plus in-process background work, so it shares the process-level semantics of the mean. The tail is what a mean hides: a lane can average a few us/frame while every 20th frame costs an order of magnitude more. The mean can also sit far *above* p50 in the same run: the mean's window extends to the final settle, whose output-wait loop polls every 2 ms, so a scene that emits no tail output (e.g. static, whose diff is empty) burns seconds of harness wait CPU that lands in the mean but never in the distribution — the samples telescope only between the first and last presented frame.
- **prep p50/p95/p99 (us)** = the runtime's own per-frame preparation wall time from `Frame.OnPresentedWithMetrics` — Update + View + diff/encode, excluding terminal flush, scheduling and callbacks (lux only). This is the figure the 8 ms preparation budget is judged against; it is wall time, not CPU, and not input-to-screen latency.
- **idle CPU%** = CPU during a 2 s quiet window after the run, as % of one core.
- **lat p50/p95** = input-to-last-output latency, ms, where the scene defines it; the wait requires output causally after the input, and units with no output are counted in notes (`no-output units`), never as 0 ms.
- **gap p50/p95/p99** = burst-to-burst gap distribution, ms.
- Each lane renders through its own idiomatic API (lux row strings + cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so this measures frameworks, not applications. All tuning is defaults-only.
- **ratatui is not measured**: it is a Rust crate, not a Go dependency; no cross-language numbers are invented.
- Numbers are comparable only within this machine's run; nothing is copied across machines.

## Scene static 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 0 | 58 | 58 | 9306.0 | 1037.1 / 1377.1 / 1479.4 | 346.8 / 434.2 / 525.1 | 447.62 | 0 | 1491 | 0.01 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 60 | 0 | -1 | 60 | — | — | — | — | — | — | 0.74 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 60 | 60 | 120 | 120 | 1977.1 | — | — | 9.29 | 12095 | 205654 | 0.01 | — | 15.9 / 16.9 / 17.2 | — |

## Scene onecell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 56 | 58 | 58 | 1238.1 | 1208.3 / 1586.7 / 1616.1 | 324.6 / 368.2 / 443.6 | 19.26 | 27 | 1514 | 0.01 | — | 16.8 / 17.4 / 33.1 | — |
| tea | 60 | 54 | -1 | 60 | 3065.6 | — | — | 20.65 | 22 | 48482 | 0.82 | — | 16.7 / 23.8 / 33.5 | — |
| tview | 60 | 60 | 120 | 120 | 1991.7 | — | — | 9.28 | 12156 | 206015 | 0.01 | — | 15.9 / 16.8 / 17.2 | — |

## Scene damage 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 1819.1 | 1761.7 / 2145.0 / 2210.2 | 783.4 / 893.4 / 1022.7 | 19.29 | 15006 | 3460 | 0.01 | — | 16.4 / 17.4 / 17.4 | — |
| tea | 60 | 58 | -1 | 60 | 6629.9 | — | — | 19.50 | 17923 | 283581 | 0.81 | — | 16.6 / 17.6 / 18.1 | — |
| tview | 60 | 60 | 120 | 120 | 2749.1 | — | — | 9.29 | 37472 | 337220 | 0.01 | — | 16.0 / 17.0 / 17.2 | — |

## Scene full 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 3296.8 | 3223.7 / 3663.6 / 3758.6 | 1601.0 / 1797.4 / 1864.9 | 19.33 | 85096 | 20815 | 0.01 | — | 16.6 / 17.5 / 17.6 | — |
| tea | 60 | 54 | -1 | 60 | 21865.1 | — | — | 20.93 | 85013 | 1541513 | 0.78 | — | 16.8 / 25.0 / 35.4 | — |
| tview | 60 | 60 | 120 | 120 | 5647.8 | — | — | 9.32 | 147522 | 900732 | 0.02 | — | 16.1 / 17.0 / 17.2 | — |

## Scene list 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 1403.9 | 1310.7 / 1760.8 / 2054.9 | 420.4 / 493.8 / 502.7 | 19.26 | 173 | 1524 | 0.01 | — | 16.9 / 17.0 / 17.0 | — |
| tea | 60 | 57 | -1 | 60 | 2099.2 | — | — | 19.56 | 160 | 24623 | 0.73 | — | 16.5 / 17.5 / 17.7 | — |
| tview | 60 | 60 | 120 | 120 | 6207.6 | — | — | 216.38 | 9194 | 147960 | 0.01 | — | 15.8 / 16.8 / 17.2 | no output observed after the drive phase (expected tail frames) |

## Scene resize 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 8 | 8 | 8 | 8 | 67554.2 | 5149.6 / 5623.6 / 5661.9 | 577.5 / 641.7 / 656.9 | 3375.38 | 6621 | 188063 | 0.01 | 0.9 / 5.3 | 250.3 / 250.4 / 250.4 | no output observed after the drive phase (expected tail frames) |
| tea | 8 | 8 | -1 | 8 | 76407.2 | — | — | 3375.62 | 6492 | 639632 | 0.75 | 5.8 / 8.4 | 249.5 / 251.4 / 251.4 | no output observed after the drive phase (expected tail frames) |
| tview | 8 | 8 | 8 | 8 | 71480.6 | — | — | 3375.75 | 22419 | 1847088 | 0.01 | 3.6 / 4.2 | 250.6 / 251.0 / 251.0 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 200 | 4 | 4 | 4 | 131596.2 | 1281.8 / 5227.4 / 5578.1 | 409.9 / 416.6 / 417.1 | 6334.50 | 51 | 37792 | 0.01 | 18.4 / 18.5 | 17.8 / 137.5 / 148.1 | no output observed after the drive phase (expected tail frames) |
| tea | 200 | 9 | -1 | 200 | 76774.7 | — | — | 2827.78 | 85 | 696337 | 0.83 | 72.6 / 79.3 | 16.5 / 114.8 / 156.5 | no output observed after the drive phase (expected tail frames) |
| tview | 200 | 2 | 400 | 400 | 2680.7 | — | — | 64.41 | 12154 | 205908 | 0.01 | 229.7 / 229.8 | 379.9 / 379.9 / 379.9 | no output observed after the drive phase (expected tail frames) |

## Scene egc 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 1831.5 | 1742.1 / 2189.7 / 2204.7 | 852.1 / 997.7 / 1048.4 | 19.29 | 735 | 1619 | 0.01 | — | 16.4 / 17.4 / 17.5 | — |
| tea | 60 | 57 | -1 | 60 | 2303.4 | — | — | 19.56 | 12339 | 63414 | 0.66 | — | 16.6 / 17.6 / 17.7 | — |
| tview | 60 | 60 | 120 | 120 | 2597.8 | — | — | 9.28 | 27943 | 375738 | 0.01 | — | 16.0 / 16.7 / 17.5 | — |

## Scene rtt 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 3 | 3 | 3 | 3 | 173417.3 | 12294.1 / 12301.5 / 12302.1 | 314.4 / 335.6 / 337.5 | 8836.00 | 28 | 1824 | 0.01 | 0.5 / 0.5 | 502.3 / 502.4 / 502.4 | no output observed after the drive phase (expected tail frames) |
| tea | 3 | 3 | -1 | 3 | 195165.7 | — | — | 8849.67 | 22 | 169987 | 0.82 | 16.4 / 16.5 | 517.1 / 517.6 / 517.6 | no output observed after the drive phase (expected tail frames) |
| tview | 3 | 3 | 6 | 6 | 89746.8 | — | — | 4418.50 | 12156 | 206116 | 0.01 | 2.9 / 2.9 | 503.2 / 503.3 / 503.3 | no output observed after the drive phase (expected tail frames) |

## Scene subcell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 1910.7 | 1827.3 / 2335.3 / 2430.2 | 923.2 / 1054.1 / 1094.6 | 19.29 | 2548 | 2352 | 0.01 | — | 16.4 / 17.5 / 17.6 | — |

## Scene tall 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 60 | 58 | 58 | 58 | 1726.4 | 1682.6 / 2102.0 / 2123.8 | 728.8 / 832.2 / 860.0 | 19.28 | 242 | 1515 | 0.01 | — | 16.4 / 17.4 / 17.4 | — |

