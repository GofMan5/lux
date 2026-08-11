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
- one assembled byte slice per `Present` call;
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
3. Add stronger writer-failure/full-repaint tests.
4. Build equivalent competitor adapters and publish raw results.
5. Benchmark interaction routing at 10/100/1,000 regions and TextInput edits at
   32 B/1 KiB/64 KiB before adding a spatial index or boundary cache.

A test terminal that applies emitted VT operations to a reference grid now
exists: `internal/terminal/vtgrid_test.go` replays real encoder output and
requires the reconstructed grid, including the caret, to equal the source buffer
across the four color depths, synchronized output on and off, successive diffs,
and a resize repaint.

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
