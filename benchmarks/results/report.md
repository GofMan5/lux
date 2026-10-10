# lux benchmark report

Mode: full (3 rounds per cell, 120x40 and 240x80)

## Machine

- CPU: Intel(R) Core(TM) i5-10400F CPU @ 2.90GHz, 10 cores
- Memory: 15.6 GiB
- Kernel: 6.6.87.2-microsoft-standard-WSL2 x86_64
- Go: go1.26.9 (linux/amd64)
- Image: golang:1.26-alpine
- Collected: 2026-10-10T04:16:11Z

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
- **lat p50/p95** = input-to-last-output latency, ms, where the scene defines it; the wait requires output causally after the input, and units with no output are counted in notes (`no-output units`), never as 0 ms.
- **gap p50/p95/p99** = burst-to-burst gap distribution, ms.
- Each lane renders through its own idiomatic API (lux row strings + cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so this measures frameworks, not applications. All tuning is defaults-only.
- **ratatui is not measured**: it is a Rust crate, not a Go dependency; no cross-language numbers are invented.
- Numbers are comparable only within this machine's run; nothing is copied across machines.

## Scene static 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 289 | 289 | 5967.4 | 103.12 | 0 | 1294 | 0.10 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | 0.79 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 300 | 600 | 600 | 2037.3 | 8.26 | 12095 | 205652 | 0.01 | — | 16.0 / 17.1 / 17.5 | — |

## Scene static 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 0 | 289 | 289 | 10086.7 | 103.12 | 0 | 1295 | 0.10 | — | — | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 0 | -1 | 300 | — | — | — | — | 0.84 | — | — | no frames observed: lane produced no paint count and no wire output in this scene; no output observed after the drive phase (expected tail frames) |
| tview | 300 | 600 | 600 | 600 | 7864.1 | 8.62 | 46602 | 794723 | 0.01 | — | 9.2 / 17.9 / 24.1 | — |

## Scene onecell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 278 | 289 | 289 | 2992.1 | 17.20 | 27 | 1331 | 0.12 | — | 16.6 / 18.1 / 34.0 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 276 | -1 | 300 | 2790.0 | 17.99 | 22 | 50923 | 0.72 | — | 16.7 / 17.9 / 33.6 | no output observed after the drive phase (expected tail frames) |
| tview | 300 | 300 | 600 | 600 | 2053.0 | 8.26 | 12156 | 205989 | 0.01 | — | 16.0 / 17.0 / 17.6 | no output observed after the drive phase (expected tail frames) |

## Scene onecell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 278 | 289 | 289 | 7191.8 | 17.21 | 27 | 1326 | 0.13 | — | 16.9 / 33.6 / 48.2 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 276 | -1 | 300 | 6725.4 | 17.99 | 22 | 155733 | 0.76 | — | 16.8 / 33.6 / 48.9 | no output observed after the drive phase (expected tail frames) |
| tview | 300 | 600 | 600 | 600 | 7459.0 | 49.69 | 46663 | 795069 | 0.01 | — | 9.8 / 20.4 / 30.9 | no output observed after the drive phase (expected tail frames) |

## Scene damage 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 3239.7 | 17.26 | 14926 | 3247 | 0.09 | — | 16.7 / 17.6 / 17.8 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 286 | -1 | 300 | 6229.0 | 17.36 | 17806 | 304392 | 0.76 | — | 16.7 / 17.7 / 33.2 | no output observed after the drive phase (expected tail frames) |
| tview | 300 | 300 | 600 | 600 | 2789.6 | 8.26 | 37379 | 336968 | 0.02 | — | 16.0 / 17.2 / 19.7 | no output observed after the drive phase (expected tail frames) |

## Scene damage 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 9294.3 | 17.22 | 59389 | 9004 | 0.13 | — | 16.8 / 44.1 / 59.0 | — |
| tea | 300 | 288 | -1 | 300 | 19629.8 | 17.30 | 71830 | 1072816 | 0.76 | — | 17.1 / 51.4 / 66.1 | — |
| tview | 300 | 600 | 600 | 600 | 9031.4 | 8.36 | 148387 | 1321563 | 0.01 | — | 8.5 / 31.4 / 35.5 | — |

## Scene full 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 3888.8 | 17.26 | 87947 | 20561 | 0.10 | — | 16.8 / 33.3 / 34.1 | — |
| tea | 300 | 286 | -1 | 300 | 19828.2 | 17.41 | 87890 | 1442083 | 0.72 | — | 16.7 / 32.7 / 48.9 | — |
| tview | 300 | 309 | 600 | 600 | 5758.2 | 8.27 | 150354 | 915873 | 0.01 | — | 15.9 / 17.0 / 17.7 | — |

## Scene full 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 11062.8 | 17.29 | 350016 | 78145 | 0.14 | — | 16.7 / 71.3 / 101.1 | — |
| tea | 300 | 303 | -1 | 300 | 73707.6 | 52.00 | 346408 | 5443787 | 0.67 | — | 50.1 / 66.5 / 72.7 | — |
| tview | 300 | 600 | 600 | 600 | 22231.9 | 60.09 | 599624 | 3640472 | 0.01 | — | 18.5 / 22.4 / 28.0 | no output observed after the drive phase (expected tail frames) |

## Scene list 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 2896.0 | 17.20 | 174 | 1335 | 0.11 | — | 16.5 / 17.7 / 18.3 | — |
| tea | 300 | 288 | -1 | 300 | 2174.0 | 17.24 | 158 | 21731 | 0.70 | — | 16.6 / 17.6 / 17.7 | — |
| tview | 300 | 300 | 600 | 600 | 1718.6 | 8.26 | 9194 | 147939 | 0.01 | — | 15.8 / 16.8 / 17.0 | — |

## Scene list 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 7339.9 | 17.21 | 298 | 1333 | 0.10 | — | 16.6 / 17.7 / 18.8 | — |
| tea | 300 | 288 | -1 | 300 | 4899.2 | 17.23 | 283 | 44874 | 0.73 | — | 16.7 / 17.6 / 18.6 | — |
| tview | 300 | 591 | 600 | 600 | 5999.2 | 8.27 | 34390 | 564085 | 0.01 | — | 7.7 / 12.3 / 15.6 | no output observed after the drive phase (expected tail frames) |

## Scene resize 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 59532.8 | 1500.80 | 6621 | 75390 | 0.10 | 2.4 / 3.7 | 250.6 / 251.6 / 252.3 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 50739.4 | 1500.70 | 6492 | 432784 | 0.75 | 11.5 / 18.0 | 249.7 / 251.7 / 252.2 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 20 | 20 | 20 | 48220.8 | 1500.70 | 22419 | 1847197 | 0.01 | 3.6 / 6.0 | 250.2 / 253.6 / 253.8 | no output observed after the drive phase (expected tail frames) |

## Scene resize 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 20 | 20 | 20 | 20 | 65483.1 | 1500.65 | 24456 | 291007 | 0.09 | 8.6 / 11.6 | 250.5 / 252.5 / 255.5 | no output observed after the drive phase (expected tail frames) |
| tea | 20 | 20 | -1 | 20 | 56524.4 | 1500.75 | 24178 | 1740495 | 0.71 | 16.8 / 24.2 | 247.3 / 254.7 / 262.2 | no output observed after the drive phase (expected tail frames) |
| tview | 20 | 40 | 20 | 20 | 58100.1 | 1500.65 | 84211 | 7318979 | 0.01 | 13.2 / 21.8 | 9.5 / 250.2 / 255.5 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 87898.9 | 2168.33 | 52 | 40010 | 0.10 | 19.3 / 19.6 | 17.6 / 153.9 / 154.1 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 23 | -1 | 600 | 54284.4 | 1142.65 | 84 | 785445 | 0.83 | 65.7 / 160.5 | 16.8 / 166.5 / 166.7 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 110 | 1200 | 1200 | 5529.2 | 24.69 | 12153 | 205906 | 0.02 | 676.1 / 816.1 | 22.0 / 132.8 / 417.8 | no output observed after the drive phase (expected tail frames) |

## Scene inputburst 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 600 | 12 | 12 | 12 | 93100.6 | 2170.58 | 53 | 41578 | 0.10 | 23.0 / 24.0 | 17.7 / 158.5 / 159.3 | no output observed after the drive phase (expected tail frames) |
| tea | 600 | 74 | -1 | 600 | 33289.2 | 366.68 | 129 | 1006797 | 1.46 | 205.9 / 708.9 | 16.7 / 29.9 / 167.2 | no output observed after the drive phase (expected tail frames) |
| tview | 600 | 1290 | 1200 | 1200 | 17980.9 | 35.16 | 46661 | 795009 | 0.02 | 3094.2 / 3632.7 | 9.4 / 27.6 / 33.9 | no output observed after the drive phase (expected tail frames) |

## Scene egc 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 288 | 288 | 288 | 12753.5 | 103.80 | 735 | 1339 | 0.14 | — | 16.7 / 17.6 / 19.5 | no output observed after the drive phase (expected tail frames) |
| tea | 300 | 287 | -1 | 300 | 9687.6 | 104.08 | 12339 | 66875 | 1.42 | — | 16.7 / 17.4 / 17.9 | no output observed after the drive phase (expected tail frames) |
| tview | 300 | 301 | 600 | 600 | 7457.6 | 49.78 | 27943 | 375691 | 0.01 | — | 16.0 / 17.4 / 23.4 | no output observed after the drive phase (expected tail frames) |

## Scene egc 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 149 | 149 | 149 | 37407.1 | 35.60 | 1437 | 2325 | 0.14 | — | 17.9 / 49.0 / 60.2 | — |
| tea | 300 | 263 | -1 | 300 | 12880.4 | 18.98 | 46214 | 181931 | 1.26 | — | 16.8 / 27.5 / 32.5 | no output observed after the drive phase (expected tail frames) |
| tview | 300 | 674 | 600 | 600 | 31543.0 | 65.39 | 112906 | 1491975 | 0.02 | — | 15.3 / 32.6 / 38.7 | no output observed after the drive phase (expected tail frames) |

## Scene rtt 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 201967.2 | 2589.08 | 28 | 1389 | 0.14 | 2.9 / 3.1 | 503.7 / 504.9 / 505.2 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 94747.6 | 2600.08 | 22 | 80822 | 0.71 | 15.7 / 16.9 | 516.7 / 517.3 / 517.6 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 12 | 24 | 24 | 40580.3 | 1293.96 | 12156 | 206121 | 0.01 | 3.0 / 3.6 | 504.2 / 505.5 / 505.5 | no output observed after the drive phase (expected tail frames) |

## Scene rtt 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 12 | 12 | 12 | 12 | 119639.0 | 2591.58 | 28 | 1386 | 0.10 | 6.3 / 15.4 | 507.8 / 517.4 / 520.1 | no output observed after the drive phase (expected tail frames) |
| tea | 12 | 12 | -1 | 12 | 103364.9 | 2600.00 | 22 | 177826 | 0.71 | 15.2 / 16.7 | 516.7 / 517.8 / 518.1 | no output observed after the drive phase (expected tail frames) |
| tview | 12 | 24 | 24 | 24 | 45056.2 | 1298.17 | 46663 | 795271 | 0.01 | 11.4 / 13.5 | 7.7 / 508.6 / 510.6 | no output observed after the drive phase (expected tail frames) |

## Scene subcell 120x40

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 2061.8 | 17.20 | 2635 | 2087 | 0.10 | — | 16.7 / 17.6 / 17.8 | — |

## Scene subcell 240x80

| lane | events | bursts | paints | draws | CPU us/frame | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lux | 300 | 289 | 289 | 289 | 4764.1 | 17.20 | 6888 | 2084 | 0.12 | — | 16.7 / 17.5 / 18.1 | — |

