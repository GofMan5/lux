# Performance contract

Snapshot: **2026-08-11**. LUX TUI has microbenchmarks, but it has not published
cross-framework results. This document defines how claims must be earned.

## Current mechanisms, not benchmark claims

The implementation already provides:

- comparable styles and cells for direct diff checks;
- caller-reusable diff storage;
- front/back buffers;
- no terminal output for an unchanged frame;
- changed row runs rather than unconditional full-screen VT output;
- one reused byte slice per `Present` call, and one reused retained frame;
- shortest-form VT encoding: relative cursor moves, SGR deltas, and
  erase-to-end-of-line instead of transmitted blanks;
- a scrolling region for vertical row shifts, which makes the cost of scrolling
  a list independent of the screen height;
- absolute-time tweens and springs that skip missed visual states;
- an idle runtime that disarms its frame timer;
- three reusable surfaces with a one-slot latest-frame queue;
- adaptive presentation pacing through 60→30→20→10 FPS steps;
- caller-owned destination storage in the public flex API;
- bounded escape-sequence and paste parsing;
- reusable CPU 3D depth and pixel buffers after viewport warmup.

These design choices should help, but none proves superiority. Terminal output,
transport latency, layout, allocations, and application work must be measured
separately.

Run the local baselines with:

```sh
go test ./...
go test -run '^$' -bench . -benchmem ./...
```

Do not copy numbers between machines or compare runs with different terminal
sizes, toolchains, transports, power modes, or background load.

## Comparative benchmark contract

When framework adapters exist, compare current stable versions of Bubble Tea +
Lip Gloss, tview + tcell, Ratatui + Crossterm, Textual, Ink, and OpenTUI. Each
must render equivalent content and receive equivalent events.

### Scenes

1. Static tree, warm no-op frame.
2. One changed cell.
3. Ten-percent random damage.
4. Full-frame animated field.
5. Large virtualized list.
6. Resize burst.
7. Input burst while rendering.
8. Extended-grapheme stress.
9. 100 ms RTT with constrained output bandwidth.
10. Subcell/3D scene, only where equivalent output exists.

Run at 120×40 and 240×80. Separate CPU-only buffer tests from PTY and real
terminal tests.

### Metrics

- p50/p95/p99 update, layout, draw, diff, encode, and flush time;
- input-to-visible latency;
- bytes and writes per committed frame;
- allocations/op and peak resident memory;
- idle CPU;
- coalesced or dropped frames;
- resize convergence time;
- visual correctness hash or terminal-model diff.

### Acceptance properties

- An unchanged warmed frame emits zero bytes.
- A committed ANSI frame is written as one coherent payload when the writer
  accepts it.
- At most one stale visual frame may be pending; newer state replaces it.
- Animation uses elapsed time and does not slow down under render lag.
- Idle applications do not wake on a periodic redraw timer unless requested.
- Parser and render queues remain bounded.
- A partial write or encoding error cannot leave internal front-buffer state
  falsely marked as presented.
- Snapshots are deterministic for the same size, capabilities, state, and time.

## Reporting rules

A performance report must include:

- repository revisions and dependency versions;
- Go/toolchain and competitor runtime versions;
- OS, CPU, memory, power plan, terminal, multiplexer, and transport;
- warmup, sample count, confidence interval or relative margin of error;
- raw machine-readable results;
- visual-equivalence validation;
- failures and excluded lanes.

Publish phase-level results and output bytes, not only FPS. A terminal may be
the bottleneck, and the fastest producer can create the worst latency if it
queues more output than the terminal can display.

## Planned performance work

1. Make renderer output reusable without hidden per-frame allocations.
2. Calibrate the implemented latest-state-wins adaptive scheduler under real
   terminals and impaired transports.
3. Extend writer-failure recovery beyond the current contract, which discards
   the terminal model on a failed write so the next frame repaints.
4. Build equivalent competitor adapters and publish raw results.
5. Benchmark interaction routing at 10/100/1,000 regions and TextInput edits at
   32 B/1 KiB/64 KiB before adding a spatial index or boundary cache.

A test terminal that applies emitted VT operations to a reference grid now
exists: `internal/terminal/vtgrid_test.go` replays real encoder output and
requires the reconstructed grid, including the caret, to equal the source buffer
across the four color depths, synchronized output on and off, successive diffs,
and a resize repaint. It models exactly the sequences the encoder can emit —
`CUP`, `CUF`, carriage return, `EL`, `SGR`, `DECSTBM`, insert- and
delete-line, `DECTCEM`, and synchronized output —
and fails on anything else, so a shorter encoding cannot be shipped without
being proved equivalent.

Output size was measured against the previous encoder on the same frames
(`paintSample`, truecolor, Windows amd64, i5-10400F, Go 1.25.5):

| Frame | Before | After | Change |
|---|---|---|---|
| 80×24 repaint | 2420 B | 607 B | −74.9% |
| 120×40 repaint | 5412 B | 767 B | −85.8% |
| Sparse diff | 277 B | 259 B | −6.5% |
| 120×40 dense half-block scene | 177155 B | 167557 B | −5.4% |
| 80×24 list scrolled one row | 826 B | 66 B | −92.0% |
| 120×40 list scrolled one row | 1434 B | 66 B | −95.4% |
| 120×40 list scrolled five rows | 1439 B | 218 B | −84.8% |

Most of the repaint saving is erase-to-end-of-line replacing transmitted blanks,
which is why a sparse redraw gains far more than a dense 3D scene where every
cell carries two distinct colors. A scroll is the sharpest case: its cost is the
rows the shift exposes, so moving a list by one row costs the same 66 bytes on
an 80×24 screen and on a 120×40 one. `TestEncoderFrameBudget` and
`TestScrollBudgetIsIndependentOfScreenSize` pin those numbers so a future change
cannot quietly grow them. Encoding itself is 0 alloc/op; scanning an unchanged
120×40 frame now goes through a per-row bitset of dirty rows and costs about
25 µs (median, i5-10400F, was roughly 100–210 µs before the bitset), and the
whole idle Present path — scan plus the no-change skip — costs about 25 µs
where it previously cost about 102 µs. A 249 B diff frame encodes in about
75 µs (was 109 µs) and a one-row scroll of a 120×40 list in about 122 µs
(was 289 µs). The scan lives in the terminal package but compares rows
through `render.Buffer.EqualRow`, so the row-comparison cost is included in
the encoder scan numbers above rather than hidden in the render package: a
standalone 40-row `EqualRow` pass over the same frame costs 33–40 µs, i.e.
roughly 0.8–1.0 µs per row, and the bitset keeps the full scan below the raw
40-compare cost. Re-measuring the capability-negotiation tree interleaved
against the previous commit on this machine (ambient load from a running
screen recorder) kept every hot-path benchmark within that noise band;
negotiation adds no per-frame cost.

Startup also negotiates capabilities before entering the alternate screen: a
terminal that answers finishes as soon as its DA1 and DECRPM replies arrive
(a few milliseconds, well under the deadline), while a silent terminal costs
exactly one bounded wait of 150 ms by default, tunable or disableable via
`LUX_TUI_PROBE_TIMEOUT`. Bytes typed during the probe are replayed into the
event loop losslessly, so the tax never swallows input.

Current local interaction baselines (Windows amd64, i5-10400F, Go 1.25.5)
show topmost hit routing at roughly 72 ns/op for 10, 100, and 1,000 regions,
with 0 alloc/op. Ctrl-word movement is 0 alloc/op but linear in text length:
roughly 11 us at 32 B, 0.42 ms at 1 KiB, and 34 ms at 64 KiB. The current
single-line editor is therefore tuned for ordinary form fields, not documents;
a cached boundary table belongs in a future document editor rather than this
small control.

The editor's horizontal window is recomputed with one grapheme pass per frame.
It was previously measured inside a loop, which is quadratic and made a pasted
3 KB value take about 2.1 s per frame; fuzzing surfaced it as a worker timeout.
The window is now linear in the value length, so a 64 KiB field costs about
16 ms per recomputation — still a form-field budget, not a document one.

The fixed-row virtual range for a declared one-million-row viewport remains
O(1) memory and measured about 5 ns (8 visible rows), 13 ns (24 rows), and
48 ns (80 rows), all 0 alloc/op. Applications draw only that returned range;
LUX does not materialize one million IDs or strings.

Wheel routing through nested viewports is 0 alloc/op and grows with nesting
depth in its worst case, where every level is exhausted and the whole enclosure
chain is walked: about 173 ns for 2 levels, 242 ns for 8, and 516 ns for 32 in
the same run. Real interfaces nest a few levels, so no parent index is justified
yet.

Until those steps are complete, describe LUX TUI as performance-oriented, not
as the fastest TUI framework.
