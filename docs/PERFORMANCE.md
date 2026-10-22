# Performance contract

Snapshot: **2026-10-12**. Cross-framework numbers are now published: the
full comparative run lives in [benchmarks/results/report.md](../benchmarks/results/report.md)
(174 runs, 0 failed, one pinned host, reproduced by the single docker
command in [benchmarks/README.md](../benchmarks/README.md)). This document
keeps defining how claims must be earned, and the committed report is the
current earned baseline.

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
- text measured once per cluster: `Text` feeds its own grapheme
  measurement into the same guarded cell-writer path `Put` uses, and rows
  outside the canvas or the active clip skip cell writes while hidden
  lines still count toward the widest-input-line return;
- widget draw paths that consult the canvas's drawable rectangle
  (`Canvas.DrawableRect`): rows a tighter `ClipRect` hides are neither
  formatted nor priced — `Table` and `Tree` `rowAt`, `List` `DisabledAt`,
  `RadioGroup` labels — while region registration keeps the full logical
  bounds so hit mapping and Tab reachability are unchanged;
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

The window title rides that same startup write and the matching restore
write, not its own syscalls: `WithTitle` costs zero additional Write calls
and zero per-frame bytes — the title is a lifecycle claim written once
between the probe and the alternate screen (a push, the OSC 0 string, and
the enter it joins in one write; the pop on the leave write). The title
bytes themselves are bounded by the title's length plus eight.

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
row's region rectangle for the closed box. (DrawOpen drew a closed box until
its missing `Open` state was fixed alongside the drawable-band work below;
the figures here predate that fix.)

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

Hidden-work elimination was measured on 2026-10-12 (production-plan stage
5, item 2). `Canvas.Text` no longer routes every cluster through `Put` for
a second segmentation/width pass: it measures each cluster once and writes
cells through the same guarded path, and rows outside the canvas or the
active clip, plus columns past the right edge, skip the write entirely.
Correctness is pinned by a reference-algorithm test and a fuzz target that
compare the full backing buffer and the return value. Interleaved A/B
microbenchmarks (six alternating rounds, benchstat, i5-10400F, Windows
Go 1.25.5, every case 0 alloc/op):

| `BenchmarkText` case | Before | After | Change |
|---|---|---|---|
| visible-ascii 120×40 | 39.25 µs | 31.88 µs | −18.78% (p=0.041) |
| visible-egc | 76.84 µs | 40.76 µs | −46.95% (p=0.002) |
| clip-band | 1.306 ms | 1.043 ms | −20.12% (p=0.015) |
| exhausted-columns | 557.5 µs | 415.1 µs | −25.54% (p=0.009) |
| tall-overscroll 2000 rows (CPU-only) | 69.56 ms | 48.88 ms | −29.73% (p=0.002) |

Geomean −29.03%. The same interleaved session re-checked the cell and
frame paths: `CanvasFrame` −19.92% (p=0.002, B/op 314.5→259.0), while the
`Put` microbenchmark moved +4.22% (p=0.015) — inside run-to-run noise; put
cell and fill are unchanged. On the PTY harness the 160-line tall scene
improved p50/p95 from 7.80/11.71 ms to 6.22/8.41 ms at 120×40 and from
15.27/23.98 ms to 13.00/17.96 ms at 240×80 (three interleaved rounds,
Docker `golang:1.26-alpine`), with idle CPU at 0.11–0.17% of one core and
zero errors.

The committed battery was re-earned with that code: 174 runs, 0 failed —
the 168-run baseline stays in git history as the A/B "before". Cross-
session p95 deltas against the old battery ranged from −14.0% (egc 240×80)
to +98.2% (resize 240×80). Those alarms did not survive same-session
adjudication: an interleaved confirmation battery (30 runs per version,
five scenes × two sizes × three alternating rounds in one session) moved
p95 by −23.9% to +9.9% with per-paint allocations flat and paint counts
stable. `frame_cpu_us` is a per-present process-CPU delta, so a round with
fewer presentations reports larger per-frame values; paired with the
microbenchmark results above, the cross-session alarms are
presentation-count artifacts, not an evidenced library regression. The
lux-only `rtt` scene was outside the confirmation set, so its cross-session
delta is adjudicated only by those microbenchmarks — recorded, not assumed.

Widget-level clip awareness was measured on 2026-10-12 (production-plan
stage 5, item 3). `Canvas.DrawableRect()` exposes the drawable area in the
canvas's local coordinates; the row widgets (`Table`, `Tree`, `List`,
`RadioGroup`) intersect their bounds with it, fill only the drawable part,
and walk only the drawable row band, so `rowAt`, `DisabledAt`, and label
formatting price visible rows only. Single-row and single-cell widgets
(`Button`, `Checkbox`, `Gauge`, `Sparkline`, `Tabs`, `Spinner`) consult the
same rectangle in their early return. Region registration still passes the
full logical bounds — `GlobalRect` clips the published rect, `originY`
keeps the unclipped origin, and content height caps at the clipped bottom —
so hit mapping, Tab reachability, and scroll clamps are byte-identical; a
reference-equivalence guard test class pins that a clipped draw matches an
unclipped draw cell-for-cell inside the clip and leaves `render.BlankCell`
outside. Interleaved A/B microbenchmarks (six alternating rounds, benchstat,
i5-10400F, Windows Go 1.25.5, parent commit 690adbd as "before"):

| `Benchmark…ClippedHalf` | Before | After | sec/op | B/op | allocs/op |
|---|---|---|---|---|---|
| RadioGroupDrawClippedHalf | 42.91 µs | 24.47 µs | −42.97% (p=0.002) | 160→128 | 7→4 |
| TableDrawClippedHalf | 324.5 µs | 178.1 µs | −45.13% (p=0.002) | 1094→651 | 57→32 |
| TreeDrawClippedHalf | 175.18 µs | 81.35 µs | −53.56% (p=0.002) | 396→218 | 11→5 |

The unclipped baselines moved within noise in the same session:
`RadioGroupDraw` 24.97→24.98 µs (p=0.699), `TableDraw` 1.700→1.707 ms
(p=0.818), `TreeDraw` 197.6→204.4 µs (p=0.240); geomean over the six
benchmarks −27.02%. The observable cost change is documented in the
CHANGELOG: `rowAt` and `DisabledAt` callbacks are invoked only for rows the
clip can paint.

Single-measurement row painting landed on 2026-10-12 (production-plan
stage 5, item 2 remainder). `TextArea.drawAreaRow`, `TextInput`'s `drawInput`, and
`drawPlaceholder` used to measure each visible cluster once for layout
(`Canvas.GraphemeWidth`) and then pay a second segmentation-and-measurement
pass inside `Canvas.Put` — every visible cluster was priced twice, which is
what `Text` stopped doing earlier in this stage. `Canvas.PutMeasured(x, y,
grapheme, width, style)` writes one caller-measured cluster: the row
painters keep their single `GraphemeWidth` call and hand the measured width
straight to the painter. Zero-width combining clusters still merge into the
preceding cell, control and invalid clusters still write nothing, and the
`PutMeasured`-versus-`Put` equivalence is pinned by a reference battery
(cluster set × 8 width policies × 6 canvas geometries, including clipped,
seeded, and off-canvas cases) plus a fuzz row walk. Interleaved A/B
microbenchmarks (six alternating rounds, benchstat, i5-10400F, Windows Go
1.25.5, parent commit 60140f1 as "before"):

| `Benchmark…` | Before | After | sec/op | B/op | allocs/op |
|---|---|---|---|---|---|
| TextAreaDrawClippedHalf | 89.34 µs | 75.98 µs | −14.96% (p=0.002) | 0→0 | 0→0 |
| TextInputDraw | 81.00 µs | 79.24 µs | −2.18% (p=0.041) | 0→0 | 0→0 |

TextInputDraw moves only marginally because its row is one line of ~40
clusters — region registration, the caret-column pass, and window
bookkeeping dominate that frame, so the removed re-measurement is real but
small there. Geomean over the two benchmarks: −8.79%. The draw is
byte-identical for every input the equivalence battery and fuzz walk
cover, so no CHANGELOG-observable behavior changed.

Visibility-aware scheduling is demonstrated in `examples/08-tour`: the
gauge and spinner stop requesting animation frames while scrolled out of
view, pinned by an end-to-end test (a hidden spinner requests zero
recurring frames; revealing it resumes animation; reduced motion freezes
decoration).

The same drawable-row band then closed the last two full-geometry widgets
(parent commit 389561e as "before", six alternating rounds, benchstat,
i5-10400F, Windows Go 1.25.5). `ui.Select`'s popup paints only its drawable
rows: a hidden anchor row skips its label truncation, the popup fill covers
the band instead of the whole rect, and each hidden option row skips its
label clipping — while placement, flip, and press geometry stay on
`SelectPlacement`'s decision against the canvas bounds, so the committed
region is untouched and press rows still match drawn rows. `Modal` fills
only the panel rows the clip can paint and skips a hidden title row's
clipping; the modal scope and the returned interior keep the full
`bounds∩canvas` rect, and the border keeps its full geometry so its corners
stay where they belong (a shrunk border rect would move them). Equivalence
is pinned by clipped-versus-unclipped cell comparisons for both placements
(below-anchor and flipped above) and for the modal, plus a
`testing.AllocsPerRun` gate: a select drawing six visible of twelve
truncated options through the clip stays within the visible rows' budget
while the six hidden rows cost no truncation.

| `Benchmark…` | Before | After | sec/op | B/op | allocs/op |
|---|---|---|---|---|---|
| SelectDrawClippedHalf | 555.1 µs | 268.9 µs | −51.57% (p=0.002) | 2.277 Ki→1.143 Ki | 75→36 |
| ModalDrawClippedHalf | 38.63 µs | 29.21 µs | −24.40% (p=0.002) | 105→105 | 2→2 |

Geomean: −39.49%. The modal moves time only — its hidden-row cost was a
fill walk, not an allocation — while the select's halved bytes and
allocations are the skipped truncations. Output is byte-identical on every
covered case, so no CHANGELOG-observable behavior changed.

## Deterministic regression gates

`TestWidgetDrawAllocationBudgets` in [ui/alloc_guard_test.go](../ui/alloc_guard_test.go)
protects 20 warmed draw scenarios through ordinary `go test ./...`, including
the existing Go 1.23/stable × Linux/macOS/Windows race matrix. Calibration
on Windows/amd64, i5-10400F, Go 1.25.5 produced identical counts in five
uncached repetitions (`AllocsPerRun(100)` per scenario):

| Scenario | Measured allocs/draw | Ceiling |
|---|---:|---:|
| Button | 1 | 1 |
| Checkbox | 2 | 2 |
| RadioGroup, six rows | 7 | 8 |
| RadioGroup, three clipped rows | 4 | 4 |
| List, six rows | 2 | 2 |
| Tabs, twelve labels | 2 | 2 |
| Gauge | 1 | 1 |
| Spinner | 1 | 1 |
| Sparkline | 1 | 1 |
| Select, closed | 1 | 1 |
| Select, open (eight options) | 9 | 10 |
| Select, seven clipped long options | 36 | 43 |
| Modal, clipped panel | 2 | 2 |
| Table, one-million-row source, 120×40 | 41 | 49 |
| Table, six clipped rows, 24×12 | 32 | 38 |
| Tree, ten rows | 11 | 13 |
| Tree, four clipped rows | 5 | 6 |
| TextArea, warm mixed-cluster clipped viewport | 0 | 0 |
| Input, warm mixed-cluster window | 0 | 0 |
| ScrollViewport, one-million-row range | 1 | 1 |

Counts of four or fewer are pinned exactly; larger counts allow at most
20% growth, rounded down to whole allocations. These are fixed upper
bounds, not an automatically updated baseline or a timing test. A negative
control that added ten escaping heap allocations to every measured draw
failed all 20 subtests; it was removed before the passing repetitions.

Scope matters: buffers, data, and editor layout are warmed before sampling.
As in the draw benchmarks, region registration/commit is outside the loop
(the modal includes its scope). The table includes the caller's fresh
three-string slice per visible row. Edits, cold wrapping, application data
loading, allocation bytes, and whole-frame lifetime costs are not covered
by this widget-count gate. No runtime behavior or public API changed.

The existing [renderer guards](../render/alloc_guard_test.go) and
[encoder guards](../internal/terminal/alloc_guard_test.go) already require
zero allocations for canvas painting, unchanged diff, scratch-reused
encoding, and idle Present. Existing `TestEncoderFrameBudget`,
`TestScrollBudgetIsIndependentOfScreenSize`, unchanged-frame encoder tests,
and `TestANSIFrameExactDiffAndWideCells` pin output bytes;
this slice reuses them rather than introducing another byte-budget system.
Reproduce the widget calibration with:

```sh
go test ./ui -run '^TestWidgetDrawAllocationBudgets$' -count=5 -v
```

## Preparation-only frame profile on reference flows

`lux.Metrics` carries per-frame phases (`UpdateTime`, `ViewTime`,
`DiffEncodeTime`, `EncodeTime`, `FlushTime`) and `PreparationTime`, their sum
without flush; `Frame.OnPresentedWithMetrics` reports them for exactly the
frame that reached the terminal. The internal `phaseprofile` package drives a
real model through the real runtime against an in-memory backend that
diff-encodes every frame like an interactive terminal (true color,
synchronized output, scroll regions) and discards the bytes, so flush is zero
and one scripted event is delivered per presented frame. Four reference apps
ship a scripted `BenchmarkPreparation120x40`:

```sh
cd examples && go test -p 1 -run '^$' -bench Preparation120x40 -benchtime 20x ./08-tour ./09-procs ./14-workspace ./15-jobs
```

Each reports `prep-p50/p95/p99-us`, `update/view/encode-p95-us`,
`bytes/frame`, `frames/op` and `clock-step-us`. Phases are timed with
`time.Now`; on the Windows reference host its step is 0.5–1 ms, so phases
shorter than a step read 0 and p95 there is an upper bound quantized to the
step. Linux steps are tens of nanoseconds.

The first profile showed `Canvas.Fill` (and `Clear`, built on it) at 43% of
procs CPU: every cell re-segmented and re-measured the same grapheme through
`Put`. Fill now measures once; single-cell graphemes fill each row's visible
span as a slice and route only the two end cells through the per-cell
wide-cell repair, which is cell-for-cell identical to the old per-cell loop
(`TestFillMatchesPerCellReference`, 4000 randomized buffers with wide cells,
subviews and clips; both mutants that skip an end cell fail it).
`BenchmarkCanvasFill`: Clear 120×40 821→22 µs (−97.3%), clipped panel
274→5 µs (−98.1%), wide-grapheme fill 697→150 µs (−78.5%), 0 allocs.

Interleaved A/B on the reference flows, 3 rounds × 10 runs, i5-10400F,
Windows, Go 1.25.5, 120×40, ms:

| Flow | p95 before | p95 after | p99 before | p99 after |
| --- | ---: | ---: | ---: | ---: |
| tour (9 pages, overlays) | 2.01 | 1.04 | 3.00 | 1.30 |
| procs (500 rows, filter) | 2.15 | 1.01 | 3.14 | 1.13 |
| workspace (editor, palette) | 1.67 | 0.51 | 2.11 | 0.67 |
| jobs (log follow, filters) | 1.39 | 0.51 | 2.02 | 0.91 |

Every flow's preparation p95 is now within about one clock step of zero and
far inside the 8 ms budget. Wire bytes per frame are unchanged. Absolute
numbers move with host load (an earlier run on a host at 88% CPU load showed
before-fix p95 of 4.5–16 ms), so compare only interleaved runs.

### Long-session bounded memory on reference flows

`TestLongSessionMemoryIsBounded` in the same four examples drives a cycle of
events (pages and overlays, filters, editing and the palette, log follow) plus
resizes to 100×30, 160×50 and back, one event per presented frame, and samples
live heap after a forced GC at the end of every cycle. It fails if heap grows
more than 128 KiB after warmup, or goroutines grow at all. Soaks are opt-in
because each paces thousands of frames:

```bash
cd examples && LUX_SOAK=1 go test -count=1 -run LongSession -v ./08-tour ./09-procs ./14-workspace ./15-jobs
```

| Flow | Cycles after warmup | Live heap growth | Goroutines |
| --- | ---: | ---: | ---: |
| tour | 49 | +21 KB | 4 → 4 |
| procs | 129 | +54 KB | 4 → 4 |
| workspace | 109 | +9 KB | 4 → 4 |
| jobs | 129 | +30 KB | 4 → 4 |

A heap profile of the residual growth attributes it to the examples' scripted
test doubles and application strings, not framework queues, caches or
history; a 64-byte-per-event leak injected into the runtime fails all four
tests. CI runs the soaks on Linux and Windows without race instrumentation.

Still open in stage 5: the final regression report. The comparative harness also publishes lux-lane
`prep p50/p95/p99` beside per-present process-CPU, which includes in-process
background work. Existing dirty-row/diff mechanisms do not imply a need for a
new damage-tracking API; add one only if profiling warrants it.

Until those steps are complete, describe LUX TUI as performance-oriented, not
as the fastest TUI framework.
