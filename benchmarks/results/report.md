# lux benchmark report

Mode: full (3 rounds per cell, 120x40 and 240x80)

## Machine

- CPU: Intel(R) Core(TM) i5-10400F CPU @ 2.90GHz, 10 cores
- Memory: 15.6 GiB
- Kernel: 6.6.87.2-microsoft-standard-WSL2 x86_64
- Go: go1.26.9 (linux/amd64)
- Collected: 2026-10-11T23:44:11Z

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
- **idle CPU%** = CPU during a 2 s quiet window after the run, as % of one core.
- **lat p50/p95** = input-to-last-output latency, ms, where the scene defines it; the wait requires output causally after the input, and units with no output are counted in notes (`no-output units`), never as 0 ms.
- **gap p50/p95/p99** = burst-to-burst gap distribution, ms.
- Each lane renders through its own idiomatic API (lux row strings + cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so this measures frameworks, not applications. All tuning is defaults-only.
- **ratatui is not measured**: it is a Rust crate, not a Go dependency; no cross-language numbers are invented.
- Numbers are comparable only within this machine's run; nothing is copied across machines.

## Scene static 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 289 | 289 | 6063.3 | 2422.1 / 3602.6 / 4059.8 | 103.12 | 0 | 1385 | 0.13 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | — | 0.71 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 300 | 600 | 600 | 2071.3 | — | 8.26 | 12095 | 205652 | 0.01 | — | 16.0 / 16.9 / 17.4 | — |

## Scene static 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 289 | 289 | 10879.3 | 6673.9 / 9790.6 / 11102.1 | 103.12 | 0 | 1382 | 0.10 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | — | 0.82 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 600 | 600 | 600 | 6286.3 | — | 8.27 | 46602 | 794732 | 0.01 | — | 7.8 / 11.9 / 13.2 | — |

## Scene onecell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 276 | 288 | 288 | 6096.2 | 2616.5 / 3550.9 / 4215.9 | 103.48 | 27 | 1428 | 0.11 | — | 16.6 / 18.1 / 33.9 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 274 | -1 | 300 | 2713.1 | — | 18.12 | 22 | 51247 | 0.71 | — | 16.7 / 17.9 / 33.9 | — |
| tview | 300 | 300 | 600 | 600 | 2004.5 | — | 8.26 | 12156 | 205988 | 0.01 | — | 16.0 / 16.9 / 17.2 | — |

## Scene onecell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 278 | 289 | 289 | 7183.9 | 7029.0 / 9314.7 / 11189.7 | 17.21 | 27 | 1418 | 0.10 | — | 16.6 / 20.1 / 34.3 | — |
| tea | 300 | 276 | -1 | 300 | 6744.1 | — | 17.99 | 22 | 155468 | 0.71 | — | 16.7 / 18.8 / 33.9 | — |
| tview | 300 | 599 | 600 | 600 | 6183.1 | — | 8.27 | 46663 | 795061 | 0.01 | — | 7.8 / 11.9 / 12.9 | — |

## Scene damage 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 3388.0 | 3189.4 / 4195.0 / 5229.8 | 17.20 | 14914 | 3334 | 0.10 | — | 16.7 / 17.6 / 17.9 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 287 | -1 | 300 | 6000.9 | — | 17.30 | 17811 | 300404 | 0.77 | — | 16.6 / 17.7 / 18.2 | — |
| tview | 300 | 300 | 600 | 600 | 2777.5 | — | 8.26 | 37375 | 336949 | 0.01 | — | 16.0 / 17.0 / 17.5 | no output observed after the drive phase (expected tail frames) |

## Scene damage 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 9462.8 | 9226.1 / 11338.2 / 14849.7 | 17.22 | 59403 | 9088 | 0.13 | — | 16.6 / 18.1 / 20.9 | — |
| tea | 300 | 286 | -1 | 300 | 20445.3 | — | 17.41 | 71802 | 1076650 | 0.73 | — | 16.7 / 18.7 / 21.8 | — |
| tview | 300 | 600 | 600 | 600 | 8972.1 | — | 8.28 | 148376 | 1321477 | 0.01 | — | 7.9 / 9.5 / 10.8 | — |

## Scene full 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 3890.8 | 3823.5 / 4725.7 / 5794.9 | 17.20 | 87957 | 20645 | 0.10 | — | 16.8 / 32.9 / 33.9 | — |
| tea | 300 | 286 | -1 | 300 | 19990.3 | — | 17.40 | 87876 | 1442323 | 0.71 | — | 16.6 / 18.5 / 33.0 | — |
| tview | 300 | 319 | 600 | 600 | 5911.0 | — | 8.27 | 150353 | 915873 | 0.01 | — | 15.9 / 17.1 / 17.5 | — |

## Scene full 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 250 | 250 | 250 | 11470.2 | 11470.7 / 13840.3 / 17634.3 | 19.88 | 349013 | 78386 | 0.12 | — | 16.8 / 33.8 / 35.1 | — |
| tea | 300 | 301 | -1 | 300 | 75009.6 | — | 52.87 | 348710 | 5479728 | 0.70 | — | 50.2 / 67.0 / 70.8 | — |
| tview | 300 | 600 | 600 | 600 | 21322.9 | — | 18.96 | 599626 | 3640483 | 0.01 | — | 18.5 / 21.3 / 26.5 | — |

## Scene list 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 2678.5 | 2615.2 / 3199.1 / 3848.3 | 17.20 | 174 | 1421 | 0.10 | — | 16.4 / 17.4 / 17.7 | — |
| tea | 300 | 288 | -1 | 300 | 2074.8 | — | 17.24 | 158 | 22240 | 0.73 | — | 16.6 / 17.6 / 17.8 | — |
| tview | 300 | 300 | 600 | 600 | 1773.4 | — | 8.26 | 9194 | 147945 | 0.01 | — | 15.8 / 16.9 / 17.2 | — |

## Scene list 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 7341.2 | 7209.4 / 8628.3 / 12396.2 | 17.21 | 298 | 1418 | 0.09 | — | 16.6 / 17.8 / 20.9 | — |
| tea | 300 | 287 | -1 | 300 | 4943.5 | — | 17.30 | 284 | 45013 | 0.68 | — | 16.7 / 17.6 / 18.8 | — |
| tview | 300 | 588 | 600 | 600 | 5619.8 | — | 8.27 | 34390 | 564089 | 0.01 | — | 8.2 / 12.4 / 15.9 | — |

## Scene resize 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 58648.1 | 9517.3 / 10370.8 / 11363.7 | 1500.65 | 6621 | 75388 | 0.09 | 2.4 / 3.1 | 250.7 / 251.4 / 251.7 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 50597.7 | — | 1500.70 | 6492 | 440341 | 0.65 | 11.9 / 18.0 | 249.9 / 252.6 / 258.1 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 20 | 20 | 20 | 49551.4 | — | 1500.65 | 22419 | 1846914 | 0.01 | 3.7 / 6.0 | 250.0 / 253.0 / 253.6 | no output observed after the drive phase (expected tail frames) |

## Scene resize 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 66551.1 | 16258.1 / 19433.6 / 21886.1 | 1500.65 | 24456 | 291328 | 0.09 | 8.7 / 11.6 | 250.4 / 252.7 / 256.0 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 58887.3 | — | 1500.75 | 24178 | 1743735 | 0.81 | 17.7 / 25.8 | 247.8 / 256.8 / 270.7 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 40 | 20 | 20 | 61267.6 | — | 1500.75 | 84211 | 7318680 | 0.01 | 12.9 / 21.6 | 10.2 / 249.5 / 255.0 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 88568.1 | 3205.9 / 9686.0 / 9861.0 | 2168.33 | 52 | 43069 | 0.10 | 19.3 / 19.7 | 17.5 / 153.7 / 154.0 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 23 | -1 | 600 | 55164.2 | — | 1142.74 | 80 | 782061 | 0.69 | 64.4 / 69.3 | 16.9 / 166.6 / 167.1 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 6 | 1200 | 1200 | 2229.2 | — | 22.92 | 12154 | 205899 | 0.01 | 265.3 / 276.4 | 416.1 / 428.3 / 428.3 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 92628.8 | 7662.1 / 14243.9 / 16802.7 | 2170.67 | 53 | 43350 | 0.09 | 23.3 / 24.3 | 17.5 / 158.1 / 160.1 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 74 | -1 | 600 | 30573.2 | — | 366.64 | 130 | 1006741 | 0.79 | 206.0 / 334.2 | 16.8 / 164.8 / 167.0 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 1199 | 1200 | 1200 | 6713.8 | — | 26.96 | 46661 | 794995 | 0.01 | 1063.9 / 1199.3 | 5.2 / 6.6 / 9.7 | no output observed after the drive phase (expected tail frames) |

## Scene egc 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 3771.6 | 3630.2 / 4431.0 / 5700.1 | 17.20 | 735 | 1423 | 0.09 | — | 16.7 / 17.5 / 18.4 | — |
| tea | 300 | 288 | -1 | 300 | 2233.2 | — | 17.24 | 12339 | 66065 | 0.72 | — | 16.8 / 17.4 / 17.8 | — |
| tview | 300 | 300 | 600 | 600 | 2635.6 | — | 8.26 | 27943 | 375704 | 0.01 | — | 16.0 / 17.0 / 17.4 | — |

## Scene egc 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 11048.6 | 10816.9 / 13053.3 / 16201.1 | 17.25 | 1437 | 1422 | 0.11 | — | 16.8 / 18.3 / 21.5 | — |
| tea | 300 | 288 | -1 | 300 | 5575.8 | — | 17.23 | 48041 | 171933 | 0.75 | — | 16.7 / 17.6 / 18.9 | — |
| tview | 300 | 600 | 600 | 600 | 8869.0 | — | 8.39 | 112906 | 1491980 | 0.01 | — | 7.8 / 10.3 / 11.7 | — |

## Scene rtt 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 104570.9 | 22082.3 / 28093.6 / 36762.3 | 2586.58 | 28 | 1451 | 0.10 | 1.8 / 2.3 | 502.9 / 503.9 / 504.5 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 95294.3 | — | 2599.92 | 22 | 80838 | 0.72 | 15.0 / 16.7 | 516.7 / 517.2 / 517.4 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 12 | 24 | 24 | 41729.0 | — | 1293.79 | 12156 | 206164 | 0.01 | 3.0 / 3.8 | 504.1 / 505.3 / 505.6 | no output observed after the drive phase (expected tail frames) |

## Scene rtt 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 111401.2 | 28261.3 / 32057.3 / 39624.6 | 2591.92 | 28 | 1451 | 0.09 | 6.2 / 10.1 | 507.6 / 511.5 / 519.9 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 92585.3 | — | 2600.00 | 22 | 177851 | 0.71 | 15.1 / 17.2 | 516.6 / 518.4 / 519.6 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 24 | 24 | 24 | 45724.0 | — | 1298.17 | 46663 | 795239 | 0.01 | 11.5 / 13.0 | 6.3 / 509.0 / 509.9 | no output observed after the drive phase (expected tail frames) |

## Scene subcell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 289 | 289 | 2169.1 | 2264.2 / 3656.2 / 4077.5 | 17.20 | 2628 | 2170 | 0.11 | — | 16.7 / 17.6 / 18.0 | — |

## Scene subcell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 4915.7 | 4766.1 / 6475.0 / 7974.7 | 17.20 | 6894 | 2173 | 0.10 | — | 16.8 / 18.0 / 19.5 | — |

