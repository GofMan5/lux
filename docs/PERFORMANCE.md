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
with 0 alloc/op. Ctrl-word movement is 0 alloc/op and exact through the
grapheme scanner, but both boundary probes first test the bytes at the
caret: when the text around the position is ASCII the probes return in
constant time, which is the common case for code and prose. A pure non-ASCII
neighborhood still scans linearly — roughly 34 ms at 64 KiB — so a document
that is entirely one long non-ASCII line keeps that old cost.

The editor's horizontal window is recomputed with one grapheme pass per frame.
It was previously measured inside a loop, which is quadratic and made a pasted
3 KB value take about 2.1 s per frame; fuzzing surfaced it as a worker timeout.
The window is now linear in the value length, so a 64 KiB field costs about
16 ms per recomputation — still a form-field budget, not a document one.

Word wrap (`WrapLines`, `MeasureWrap`, and `Canvas.TextWrap`) is one linear
grapheme pass: a line builder, a word builder, and a whitespace builder,
each grapheme appended once, with a single re-scan reserved for an
unbreakable run wider than the limit. A 64 KiB paragraph therefore stays
in the same tens-of-milliseconds per-field class as the editor window
above in every wrap mode — the budget the text-truth stage is held to —
and the per-mode medians are recorded in the stage 7 verification
artifacts. The wrap is stateless and recomputes per call; the document-level
cache that avoids re-wrapping untouched lines lives in `ui.TextArea` instead.

`ui.TextArea` holds the wrap as a dirty-range cache: an edit invalidates from
the mutated line to the end of its logical neighborhood and re-wraps only
those lines, so a keystroke in a 400-line document re-wraps one line and
measures about 78 us/op with 7 alloc/op, where a full re-layout of the same
document costs about 4.8 ms. A single huge line defeats the line-granularity
cache by construction: a 128 KiB one-line document re-wraps that line per
keystroke, about 720 us/op after the boundary fast paths (previously
142 ms, before `floorBoundary` scanned the whole prefix on every edit).
Its full layout pass is about 31 ms and 47.5k allocs, and a 64 KiB line
lands in the same tens-of-milliseconds budget the wrap contract above
holds. Painting a 400-line viewport is about 0.80 ms with 3 alloc/op.
What is still future work, honestly: incremental per-row rewrap inside one
logical line, so a growing single paragraph stays flat instead of paying
the whole-line wrap; the cache stops at line granularity.

The fixed-row virtual range for a declared one-million-row viewport remains
O(1) memory and measured about 5 ns (8 visible rows), 13 ns (24 rows), and
48 ns (80 rows), all 0 alloc/op. Applications draw only that returned range;
LUX does not materialize one million IDs or strings.

`ui.Table` extends that virtualization to tabular data: `BenchmarkTableDraw`
paints a 120×40 table over a one-million-row source (39 visible rows, three
columns, a selection mid-table) in about 1.8 ms per full repaint with 41
alloc/op on the same machine (i5-10400F, Go 1.25.5, `-benchtime=2s`,
`-count=1`). The cost is the visible window plus per-cell EGC clipping, not
the row count — `rowAt` is called exactly once per visible row, a property
pinned by test, so a million-row and a hundred-row source paint identically.
Wheel scrolling repaints at the same cost after the viewport-only decoupling:
the offset moves without pulling the selection into view.

The stage 8 form controls are single-`Apply` widgets with draw costs that do
not grow with anything but the visible rows on the same machine
(`-benchtime=2s`, `-count=1`): `BenchmarkCheckboxDraw` about 14 µs with
2 alloc/op, `BenchmarkRadioGroupDraw` about 30 µs with 4 alloc/op for three
rows, and `BenchmarkSelectDrawClosed`/`BenchmarkSelectDrawOpen` about
5.4 µs and 7 µs with 1 alloc/op at 40 columns — the open popup pays only its
own visible rows, never the option count, and one switch reuses the anchor
row's region rectangle for the closed box.

The stage 8 display widgets round out the same table (same machine,
`-benchtime=2s`, `-count=1`): `BenchmarkTabsDraw` about 82 µs with 2
alloc/op for a 120-column strip over twelve eight-character labels — slot
layout plus styled text per tab, independent of any tab count beyond the
strip width that actually fits, and the `keepTabVisible` scan sums label
widths directly instead of building a layout per advance; `BenchmarkGaugeDraw` about 13 µs with 1
alloc/op for an 80-cell bar (49 full, one half, 30 track cells — one Put per
cell, like every other cell-addressed widget); `BenchmarkSpinnerDraw` about
0.52 µs with 1 alloc/op for one glyph cell, which is what an
absolute-time phase should cost. `BenchmarkSparklineDraw` about 15 µs with
1 alloc/op for a 60×3 window over 256 samples — one range pass per frame
feeds one Put per glyph cell; the window bounds the cost, not the series
length. `BenchmarkTreeDraw` about 197 µs with 11 alloc/op for 40 visible
rows with pooled indents and in-place label clipping; like the table, the
row source is consulted once per visible row, never per cell. These
benchmarks match
`BenchmarkTableDraw` in scope: no `Begin`, so region registration and focus
bookkeeping are outside the measured loop; registration is one bounded
append per control per frame.

Wheel routing through nested viewports is 0 alloc/op and grows with nesting
depth in its worst case, where every level is exhausted and the whole enclosure
chain is walked: about 173 ns for 2 levels, 242 ns for 8, and 516 ns for 32 in
the same run. Real interfaces nest a few levels, so no parent index is justified
yet.

Theme resolution (stage 8, slice 6) adds one comparable struct check per
resolved style field plus one guarded `Context.Theme()` read per widget
draw — no allocation. Re-running the same draw benchmarks after the wiring
shows no movement outside run noise: the spinner (the smallest draw, one
cell) moves from about 0.52 µs to about 0.54 µs, which is the cost of the
one mutex-guarded theme read; the other widgets stay within a few percent
(Tabs 83 vs 82 µs, Gauge 13 µs, Sparkline 15.5 vs 15.4 µs, Tree 197 µs,
Table 1.63 ms), and `alloc/op` counts are unchanged everywhere.

Until those steps are complete, describe LUX TUI as performance-oriented, not
as the fastest TUI framework.
