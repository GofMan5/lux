# Performance contract

Snapshot: **2026-08-09**. LUX TUI has microbenchmarks, but it has not published
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

1. Add a test terminal that applies emitted VT operations to a reference grid.
2. Make renderer output reusable without hidden per-frame allocations.
3. Calibrate the implemented latest-state-wins adaptive scheduler under real
   terminals and impaired transports.
4. Add stronger writer-failure/full-repaint tests.
5. Build equivalent competitor adapters and publish raw results.
6. Benchmark interaction routing at 10/100/1,000 regions and TextInput edits at
   32 B/1 KiB/64 KiB before adding a spatial index or boundary cache.

Current local interaction baselines (Windows amd64, i5-10400F, Go 1.25.5)
show topmost hit routing at roughly 72 ns/op for 10, 100, and 1,000 regions,
with 0 alloc/op. Ctrl-word movement is 0 alloc/op but linear in text length:
roughly 11 us at 32 B, 0.42 ms at 1 KiB, and 34 ms at 64 KiB. The current
single-line editor is therefore tuned for ordinary form fields, not documents;
a cached boundary table belongs in a future document editor rather than this
small control.

Until those steps are complete, describe LUX TUI as performance-oriented, not
as the fastest TUI framework.
