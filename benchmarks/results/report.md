# lux benchmark report

Mode: full (3 rounds per cell, 120x40 and 240x80)

## Machine

- CPU: Intel(R) Core(TM) i5-10400F CPU @ 2.90GHz, 10 cores
- Memory: 15.6 GiB
- Kernel: 6.6.87.2-microsoft-standard-WSL2 x86_64
- Go: go1.26.9 (linux/amd64)
- Image: golang:1.26-alpine
- Collected: 2026-10-12T08:18:33Z

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
| lux | 300 | 0 | 288 | 288 | 7472.2 | 2478.6 / 3513.1 / 3863.1 | 103.48 | 0 | 1386 | 0.14 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | — | 1.00 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 300 | 600 | 600 | 2330.6 | — | 8.26 | 12095 | 205650 | 0.01 | — | 16.0 / 17.2 / 17.7 | — |

## Scene static 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 288 | 288 | 11739.5 | 6568.2 / 9063.7 / 10368.4 | 103.48 | 0 | 1384 | 0.14 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | — | 1.00 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 600 | 600 | 600 | 7729.2 | — | 8.27 | 46602 | 794733 | 0.01 | — | 7.7 / 11.5 / 12.8 | — |

## Scene onecell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 276 | 288 | 288 | 6635.0 | 2745.0 / 3765.7 / 4527.1 | 103.48 | 27 | 1425 | 0.15 | — | 16.7 / 17.9 / 33.8 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 276 | -1 | 300 | 3116.2 | — | 17.99 | 22 | 47627 | 1.10 | — | 16.7 / 18.2 / 34.1 | — |
| tview | 300 | 300 | 600 | 600 | 2254.1 | — | 8.26 | 12156 | 205983 | 0.01 | — | 16.0 / 17.0 / 17.5 | — |

## Scene onecell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 276 | 288 | 288 | 11282.7 | 6868.1 / 9490.8 / 10566.3 | 103.48 | 27 | 1423 | 0.13 | — | 16.7 / 19.6 / 33.9 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 272 | -1 | 300 | 7679.6 | — | 18.25 | 22 | 156217 | 1.02 | — | 16.7 / 20.9 / 34.3 | — |
| tview | 300 | 600 | 600 | 600 | 8226.2 | — | 8.35 | 46663 | 795062 | 0.01 | — | 7.8 / 11.6 / 13.1 | — |

## Scene damage 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 3409.7 | 3349.7 / 4653.2 / 5315.8 | 17.20 | 14905 | 3333 | 0.11 | — | 16.6 / 17.8 / 18.2 | — |
| tea | 300 | 286 | -1 | 300 | 6643.3 | — | 17.36 | 17800 | 303706 | 1.05 | — | 16.7 / 17.9 / 18.7 | — |
| tview | 300 | 300 | 600 | 600 | 3182.5 | — | 8.26 | 37376 | 336965 | 0.01 | — | 16.0 / 17.4 / 18.1 | — |

## Scene damage 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 9518.3 | 9165.2 / 12174.4 / 14318.4 | 17.23 | 59389 | 9089 | 0.11 | — | 16.7 / 18.9 / 33.6 | — |
| tea | 300 | 298 | -1 | 300 | 21601.1 | — | 17.33 | 71325 | 1036805 | 1.05 | — | 16.8 / 20.6 / 31.8 | — |
| tview | 300 | 600 | 600 | 600 | 12125.7 | — | 50.35 | 148360 | 1321384 | 0.01 | — | 8.9 / 14.1 / 19.1 | no output observed after the drive phase (expected tail frames) |

## Scene full 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 4169.5 | 4703.6 / 7352.0 / 10450.3 | 17.21 | 87947 | 20653 | 0.12 | — | 16.8 / 49.1 / 50.9 | — |
| tea | 300 | 289 | -1 | 300 | 21967.2 | — | 17.34 | 87961 | 1432913 | 1.02 | — | 17.0 / 50.8 / 67.4 | — |
| tview | 300 | 494 | 600 | 600 | 7883.3 | — | 8.30 | 150354 | 915870 | 0.01 | — | 9.6 / 17.5 / 25.8 | no output observed after the drive phase (expected tail frames) |

## Scene full 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 147 | 146 | 146 | 15385.6 | 13714.1 / 22694.8 / 45000.0 | 34.16 | 350108 | 79351 | 0.12 | — | 32.4 / 99.2 / 101.3 | — |
| tea | 300 | 302 | -1 | 300 | 94504.7 | — | 65.85 | 347555 | 5466096 | 1.22 | — | 64.6 / 95.0 / 153.3 | — |
| tview | 300 | 610 | 600 | 600 | 28788.6 | — | 64.36 | 599622 | 3640475 | 0.01 | — | 20.9 / 30.0 / 40.5 | no output observed after the drive phase (expected tail frames) |

## Scene list 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 2930.1 | 2747.9 / 3958.7 / 4451.5 | 17.22 | 174 | 1428 | 0.14 | — | 16.7 / 17.7 / 18.0 | — |
| tea | 300 | 288 | -1 | 300 | 2643.4 | — | 17.25 | 158 | 21492 | 1.15 | — | 16.7 / 18.3 / 32.1 | — |
| tview | 300 | 300 | 600 | 600 | 2079.4 | — | 8.26 | 9194 | 147927 | 0.01 | — | 15.9 / 17.2 / 17.6 | — |

## Scene list 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 239 | 240 | 240 | 9073.7 | 8045.9 / 12112.8 / 16863.2 | 20.73 | 354 | 1627 | 0.15 | — | 16.9 / 33.7 / 35.6 | — |
| tea | 300 | 287 | -1 | 300 | 5978.8 | — | 17.31 | 284 | 44790 | 1.08 | — | 16.6 / 18.8 / 26.3 | — |
| tview | 300 | 602 | 600 | 600 | 7061.3 | — | 8.35 | 34390 | 564087 | 0.01 | — | 7.7 / 12.2 / 18.6 | no output observed after the drive phase (expected tail frames) |

## Scene resize 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 75246.1 | 11900.7 / 18445.8 / 18814.8 | 1500.70 | 6621 | 75400 | 0.12 | 2.4 / 3.4 | 250.7 / 251.5 / 251.7 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 60851.1 | — | 1500.70 | 6492 | 446812 | 1.04 | 13.6 / 19.2 | 249.5 / 252.9 / 260.1 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 20 | 20 | 20 | 59265.0 | — | 1500.65 | 22419 | 1846910 | 0.01 | 4.2 / 7.6 | 250.2 / 253.9 / 254.2 | no output observed after the drive phase (expected tail frames) |

## Scene resize 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 81607.9 | 18189.3 / 38518.3 / 39386.5 | 1500.75 | 24456 | 291065 | 0.11 | 8.6 / 21.6 | 250.6 / 261.4 / 262.1 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 68968.4 | — | 1500.65 | 24178 | 1739958 | 1.09 | 18.8 / 31.0 | 248.6 / 257.2 / 262.1 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 40 | 20 | 20 | 72319.0 | — | 1500.70 | 84211 | 7318692 | 0.01 | 15.8 / 26.1 | 13.6 / 250.6 / 256.7 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 106552.5 | 4368.2 / 14535.5 / 15213.4 | 2168.75 | 52 | 41378 | 0.12 | 19.3 / 19.8 | 17.7 / 154.5 / 154.7 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 24 | -1 | 600 | 62471.5 | — | 1095.88 | 91 | 761388 | 1.11 | 65.8 / 69.1 | 16.9 / 166.7 / 167.1 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 18 | 1200 | 1200 | 3358.3 | — | 23.42 | 12154 | 205902 | 0.01 | 330.2 / 437.4 | 87.2 / 443.5 / 454.6 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 106515.6 | 8362.7 / 17742.2 / 18623.7 | 2170.50 | 53 | 42666 | 0.11 | 22.8 / 23.7 | 17.8 / 157.6 / 158.4 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 89 | -1 | 600 | 35238.6 | — | 307.64 | 117 | 837795 | 1.23 | 240.7 / 319.2 | 16.7 / 165.6 / 167.0 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 1200 | 1200 | 1200 | 8047.5 | — | 27.93 | 46661 | 794999 | 0.01 | 1284.3 / 1453.7 | 6.0 / 9.0 / 15.0 | no output observed after the drive phase (expected tail frames) |

## Scene egc 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 3353.1 | 3158.4 / 4317.6 / 5169.5 | 17.20 | 735 | 1424 | 0.12 | — | 16.6 / 17.6 / 18.1 | — |
| tea | 300 | 287 | -1 | 300 | 2640.5 | — | 17.29 | 12339 | 64716 | 1.06 | — | 16.8 / 17.5 / 18.1 | — |
| tview | 300 | 300 | 600 | 600 | 3180.1 | — | 8.26 | 27943 | 375698 | 0.01 | — | 16.0 / 17.3 / 17.9 | — |

## Scene egc 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 8469.1 | 8213.4 / 11228.9 / 13648.5 | 17.22 | 1437 | 1423 | 0.11 | — | 16.7 / 18.8 / 20.9 | — |
| tea | 300 | 288 | -1 | 300 | 6234.6 | — | 17.23 | 48041 | 174153 | 1.06 | — | 16.7 / 18.4 / 19.5 | — |
| tview | 300 | 600 | 600 | 600 | 9623.5 | — | 8.58 | 112906 | 1491980 | 0.01 | — | 8.0 / 10.8 / 12.8 | no output observed after the drive phase (expected tail frames) |

## Scene rtt 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 122934.2 | 25359.3 / 32369.0 / 34609.9 | 2586.33 | 28 | 1451 | 0.12 | 1.6 / 2.1 | 502.6 / 503.6 / 504.3 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 115234.3 | — | 2599.83 | 22 | 80829 | 0.97 | 15.3 / 17.0 | 516.6 / 517.5 / 517.6 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 12 | 24 | 24 | 50886.1 | — | 1293.96 | 12156 | 206161 | 0.01 | 3.2 / 3.7 | 504.4 / 505.3 / 505.5 | no output observed after the drive phase (expected tail frames) |

## Scene rtt 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 130239.2 | 30179.0 / 34019.2 / 39099.6 | 2590.00 | 28 | 1450 | 0.11 | 5.3 / 6.5 | 506.4 / 507.6 / 509.0 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 116717.2 | — | 2599.75 | 22 | 177838 | 0.98 | 15.4 / 17.0 | 516.6 / 518.1 / 518.4 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 24 | 24 | 24 | 57063.2 | — | 1298.71 | 46663 | 795185 | 0.01 | 12.4 / 14.9 | 7.9 / 509.1 / 510.4 | no output observed after the drive phase (expected tail frames) |

## Scene subcell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 2494.3 | 2393.8 / 3412.9 / 3880.1 | 17.20 | 2629 | 2180 | 0.12 | — | 16.7 / 17.6 / 18.0 | — |

## Scene subcell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 5658.1 | 5624.7 / 8202.5 / 9183.6 | 17.21 | 6914 | 2179 | 0.11 | — | 16.6 / 18.6 / 19.3 | — |

## Scene tall 120x40

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 5929.0 | 5926.9 / 8275.7 / 9641.5 | 17.21 | 267 | 1424 | 0.12 | — | 16.7 / 18.1 / 19.0 | — |

## Scene tall 240x80

| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 11716.8 | 11432.2 / 14640.9 / 17149.7 | 17.22 | 661 | 1423 | 0.14 | — | 16.6 / 19.2 / 20.6 | — |

