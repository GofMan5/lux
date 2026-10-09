# lux benchmark report

Mode: full (3 rounds per cell, 120x40 and 240x80)

## Machine

- CPU: Intel(R) Core(TM) i5-10400F CPU @ 2.90GHz, 10 cores
- Memory: 15.6 GiB
- Kernel: 6.6.87.2-microsoft-standard-WSL2 x86_64
- Go: go1.26.9 (linux/amd64)
- Image: golang:1.26-alpine
- Collected: 2026-10-09T21:07:37Z

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
- **idle CPU%** = CPU during a 2 s quiet window after the run, as % of one core.
- **lat p50/p95** = input-burst-to-last-output latency, ms, where the scene defines it.
- **gap p50/p95/p99** = burst-to-burst gap distribution, ms.
- Each lane renders through its own idiomatic API (lux row strings + cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so this measures frameworks, not applications. All tuning is defaults-only.
- **ratatui is not measured**: it is a Rust crate, not a Go dependency; no cross-language numbers are invented.
- Numbers are comparable only within this machine's run; nothing is copied across machines.

## Scene static 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 379 | 379 | 1977.5 | 12.67 | 0 | 986 | 0.28 | — | — | — |
| tea | 300 | 0 | -1 | 404 | — | — | — | — | 1.13 | — | — | no frames observed: lane produced no paint count and no wire output in this scene |
| tview | 300 | 300 | 802 | 802 | 1729.8 | 6.18 | 9049 | 153859 | 0.01 | — | 16.0 / 17.2 / 17.9 | — |

## Scene static 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 379 | 379 | 5717.7 | 12.67 | 0 | 984 | 0.60 | — | — | — |
| tea | 300 | 0 | -1 | 404 | — | — | — | — | 1.21 | — | — | no frames observed: lane produced no paint count and no wire output in this scene |
| tview | 300 | 602 | 802 | 802 | 6557.7 | 6.34 | 34864 | 594555 | 0.01 | — | 7.9 / 11.9 / 16.3 | — |

## Scene onecell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 278 | 380 | 380 | 2323.2 | 13.08 | 20 | 1012 | 0.15 | — | 16.6 / 18.0 / 33.8 | — |
| tea | 300 | 274 | -1 | 404 | 3441.9 | 18.12 | 22 | 49453 | 1.14 | — | 16.7 / 18.0 / 34.1 | — |
| tview | 300 | 300 | 802 | 802 | 1838.9 | 6.18 | 9094 | 154103 | 0.01 | — | 16.0 / 17.1 / 17.5 | — |

## Scene onecell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 276 | 378 | 378 | 6483.4 | 13.16 | 20 | 1017 | 0.12 | — | 16.7 / 23.9 / 34.5 | — |
| tea | 300 | 274 | -1 | 404 | 7418.6 | 18.11 | 22 | 157511 | 0.98 | — | 16.7 / 20.5 / 34.2 | — |
| tview | 300 | 601 | 802 | 802 | 6113.2 | 6.21 | 34910 | 594813 | 0.01 | — | 8.2 / 11.8 / 17.0 | — |

## Scene damage 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 379 | 379 | 3020.3 | 13.08 | 11335 | 2467 | 0.13 | — | 16.7 / 17.9 / 18.3 | — |
| tea | 300 | 286 | -1 | 404 | 6987.1 | 17.36 | 17817 | 303415 | 0.98 | — | 16.7 / 17.9 / 18.8 | — |
| tview | 300 | 300 | 802 | 802 | 2413.7 | 6.18 | 27970 | 252127 | 0.01 | — | 16.0 / 17.3 / 17.9 | — |

## Scene damage 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 378 | 378 | 7916.0 | 13.13 | 45264 | 6865 | 0.15 | — | 16.6 / 19.3 / 21.9 | — |
| tea | 300 | 299 | -1 | 404 | 21624.1 | 17.32 | 71818 | 1037185 | 0.95 | — | 16.7 / 19.4 / 21.5 | — |
| tview | 300 | 602 | 802 | 802 | 8567.1 | 7.44 | 110995 | 988606 | 0.01 | — | 9.0 / 12.1 / 16.6 | — |

## Scene full 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 380 | 380 | 3353.4 | 13.08 | 66894 | 15623 | 0.11 | — | 16.7 / 17.8 / 18.3 | — |
| tea | 300 | 296 | -1 | 404 | 23791.5 | 17.66 | 87600 | 1401475 | 1.03 | — | 16.7 / 19.0 / 33.0 | — |
| tview | 300 | 373 | 802 | 802 | 4900.2 | 6.19 | 112484 | 685191 | 0.01 | — | 13.2 / 17.3 / 18.4 | — |

## Scene full 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 207 | 293 | 293 | 8581.5 | 16.99 | 245846 | 55504 | 0.13 | — | 17.4 / 36.2 / 51.5 | — |
| tea | 300 | 302 | -1 | 404 | 88919.9 | 61.94 | 347555 | 5464384 | 1.11 | — | 62.4 / 80.9 / 95.0 | — |
| tview | 300 | 600 | 802 | 802 | 18326.5 | 15.79 | 448596 | 2723542 | 0.01 | — | 20.2 / 26.3 / 33.7 | — |

## Scene list 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 379 | 379 | 2330.0 | 13.11 | 132 | 1017 | 0.11 | — | 16.5 / 17.7 / 18.3 | — |
| tea | 300 | 288 | -1 | 404 | 2216.6 | 17.24 | 158 | 21024 | 0.91 | — | 16.6 / 17.6 / 17.9 | — |
| tview | 300 | 300 | 802 | 802 | 1625.4 | 6.18 | 6878 | 110686 | 0.01 | — | 15.9 / 17.2 / 17.9 | — |

## Scene list 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 379 | 379 | 6022.8 | 13.10 | 227 | 1015 | 0.11 | — | 16.7 / 18.9 / 21.8 | — |
| tea | 300 | 288 | -1 | 404 | 6027.7 | 17.23 | 283 | 44849 | 0.96 | — | 16.6 / 18.4 / 19.9 | — |
| tview | 300 | 599 | 802 | 802 | 4881.7 | 6.19 | 25728 | 422016 | 0.01 | — | 7.7 / 12.0 / 14.5 | — |

## Scene resize 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 111 | 111 | 711.7 | 45.18 | 1193 | 13584 | 0.10 | — | 250.8 / 251.8 / 252.2 | — |
| tea | 20 | 20 | -1 | 124 | 5758.3 | 250.60 | 6492 | 438440 | 0.81 | — | 250.0 / 252.1 / 258.1 | — |
| tview | 20 | 20 | 222 | 222 | 499.4 | 22.58 | 2020 | 166407 | 0.02 | — | 250.1 / 254.0 / 254.4 | — |

## Scene resize 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 111 | 111 | 2132.1 | 45.16 | 4407 | 52445 | 0.11 | — | 251.0 / 254.0 / 256.4 | — |
| tea | 20 | 20 | -1 | 124 | 12372.5 | 250.65 | 24178 | 1735048 | 0.80 | — | 247.7 / 256.1 / 258.0 | — |
| tview | 20 | 40 | 222 | 222 | 1622.4 | 22.58 | 7587 | 659341 | 0.02 | — | 13.5 / 251.8 / 254.7 | — |

## Scene inputburst 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 2 | 93 | 93 | 166.9 | 1.84 | 1 | 4996 | 0.17 | 0.0 / 2.7 | 17.4 / 17.6 / 17.6 | — |
| tea | 600 | 0 | -1 | 704 | — | — | — | — | 18.75 | — | — | no frames observed: lane produced no paint count and no wire output in this scene |
| tview | 600 | 8 | 1402 | 1402 | 1491.6 | 1.86 | 10403 | 176233 | 0.01 | 282.3 / 310.0 | 409.8 / 437.1 / 441.5 | — |

## Scene inputburst 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 0 | 92 | 92 | 30.2 | 0.01 | 0 | 2832 | 0.96 | — | — | — |
| tea | 600 | 0 | -1 | 704 | — | — | — | — | 67.97 | — | — | no frames observed: lane produced no paint count and no wire output in this scene |
| tview | 600 | 1201 | 1402 | 1402 | 5967.8 | 5.84 | 39938 | 680450 | 0.01 | 1173.8 / 1330.4 | 5.7 / 8.1 / 10.2 | — |

## Scene egc 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 379 | 379 | 3115.1 | 13.07 | 558 | 1016 | 0.11 | — | 16.6 / 17.9 / 18.7 | — |
| tea | 300 | 288 | -1 | 404 | 2564.4 | 17.24 | 12339 | 65351 | 0.95 | — | 16.8 / 17.6 / 18.2 | — |
| tview | 300 | 300 | 802 | 802 | 2151.5 | 6.18 | 20905 | 281073 | 0.01 | — | 16.0 / 17.1 / 17.7 | — |

## Scene egc 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 287 | 378 | 378 | 9469.7 | 13.15 | 1091 | 1020 | 0.13 | — | 16.6 / 21.0 / 25.0 | — |
| tea | 300 | 288 | -1 | 404 | 6613.3 | 17.23 | 48041 | 175085 | 1.00 | — | 16.7 / 18.6 / 19.9 | — |
| tview | 300 | 602 | 802 | 802 | 7191.8 | 6.37 | 84468 | 1116194 | 0.01 | — | 7.9 / 10.9 / 13.6 | — |

## Scene rtt 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 1 | 93 | 93 | 273.7 | 5.42 | 0 | 122 | 0.34 | 0.0 / 1.7 | — | — |
| tea | 12 | 1 | -1 | 116 | 24070.0 | 514.00 | 22 | 264384 | 1.24 | 0.0 / 12.7 | — | — |
| tview | 12 | 12 | 226 | 226 | 1130.1 | 26.77 | 1291 | 21895 | 0.01 | 3.1 / 4.1 | 504.3 / 506.1 / 506.8 | — |

## Scene rtt 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 1 | 93 | 93 | 312.7 | 5.46 | 0 | 122 | 0.77 | 0.0 / 6.0 | — | — |
| tea | 12 | 1 | -1 | 116 | 25989.0 | 516.00 | 22 | 362640 | 2.10 | 0.0 / 12.9 | — | — |
| tview | 12 | 24 | 226 | 226 | 1691.8 | 27.25 | 4955 | 84445 | 0.01 | 12.1 / 14.5 | 6.9 / 508.8 / 511.5 | — |

## Scene subcell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 380 | 380 | 1669.8 | 13.08 | 2001 | 1590 | 0.12 | — | 16.7 / 17.6 / 17.9 | — |

## Scene subcell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 379 | 379 | 4196.7 | 13.12 | 5259 | 1586 | 0.15 | — | 16.6 / 18.4 / 19.8 | — |

