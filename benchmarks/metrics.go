// Result records, aggregation, and report rendering. Facts only: numbers
// come from measured runs on this machine, nothing is copied cross-machine.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// RunResult is one measured child run, serialized to results/raw/.
type RunResult struct {
	Adapter string `json:"adapter"`
	Scene   string `json:"scene"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Round   int    `json:"round"`
	Quick   bool   `json:"quick"`

	Events  int    `json:"events"`
	Bytes   int64  `json:"bytes"`
	Bursts  int64  `json:"bursts"`
	Paints  int64  `json:"paints"` // -1 = not comparable for this lane
	Draws   int64  `json:"draws"`
	CPUUs   int64  `json:"cpu_us"`
	WallMs  int64  `json:"wall_ms"`
	AllocB  uint64 `json:"alloc_bytes"`
	Mallocs uint64 `json:"mallocs"`

	IdleCPUUs int64 `json:"idle_cpu_us"`
	SettleMs  int64 `json:"settle_ms"`

	BatchLatMs []float64 `json:"batch_latency_ms,omitempty"`
	// FrameCPUUs holds one process-CPU delta (us) per presented frame,
	// sampled at the lane's presentation callback. nil when the lane
	// cannot sample per-frame CPU — the same honesty rule as Paints == -1.
	FrameCPUUs []float64 `json:"frame_cpu_us,omitempty"`
	// FramePrepUs holds the runtime's Update+View+diff/encode wall time
	// (us) per presented frame, flush excluded; nil when unavailable.
	FramePrepUs []float64 `json:"frame_prep_us,omitempty"`
	FrameGapMs  []float64 `json:"frame_gap_ms,omitempty"`

	// NoOutput counts drive units (bursts, resizes, rtt steps) that
	// produced no output at all after the input — a distinct outcome,
	// never a 0 ms latency.
	NoOutput int      `json:"no_output,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func (r *RunResult) label() string {
	return fmt.Sprintf("%s %s %dx%d r%d", r.Adapter, r.Scene, r.Width, r.Height, r.Round)
}

func writeResultFile(path string, res *RunResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readResultFile(path string) (*RunResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res RunResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// machineSpec records what the numbers were measured on.
type machineSpec struct {
	GoVersion string    `json:"go_version"`
	GOOS      string    `json:"goos"`
	GOARCH    string    `json:"goarch"`
	CPUModel  string    `json:"cpu_model"`
	Cores     int       `json:"cores"`
	MemTotalB uint64    `json:"mem_total_bytes"`
	Kernel    string    `json:"kernel_release"`
	Machine   string    `json:"machine"`
	Image     string    `json:"bench_image"`
	Collected time.Time `json:"collected"`
}

func gatherMachineSpec() machineSpec {
	spec := machineSpec{
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		Cores:     runtime.NumCPU(),
		Image:     os.Getenv("BENCH_IMAGE"),
		Collected: time.Now().UTC(),
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if value, ok := strings.CutPrefix(line, "model name"); ok {
				spec.CPUModel = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), ":"))
				break
			}
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		var totalKB uint64
		for _, line := range strings.Split(string(data), "\n") {
			if n, err := fmt.Sscanf(line, "MemTotal: %d kB", &totalKB); n == 1 && err == nil {
				spec.MemTotalB = totalKB * 1024
				break
			}
		}
	}
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err == nil {
		spec.Kernel = utsField(&uts.Release)
		spec.Machine = utsField(&uts.Machine)
	}
	return spec
}

// utsField converts a fixed-size int8 array to a Go string.
func utsField(f *[65]int8) string {
	b := make([]byte, 0, 65)
	for _, c := range f {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

// laneOrder gives the report a stable lane order.
var laneOrder = map[string]int{"lux": 0, "tea": 1, "tview": 2}

const idleWindowUs = 2_000_000 // 2 s idle window, in microseconds

// laneAgg is the aggregated view of one lane inside one cell.
type laneAgg struct {
	Lane   string
	Runs   int
	Events int64
	Bytes  int64
	Bursts int64
	Paints int64
	Draws  int64
	Frames int64
	CPUUs  int64
	WallMs int64

	CPUPerFrame   float64
	MsPerFrame    float64
	BytesPerFrame float64
	AllocPerFrame float64
	Mallocs       uint64
	IdleCPUPct    float64
	LatP50        float64
	LatP95        float64
	GapP50        float64
	GapP95        float64
	GapP99        float64
	FrameCPUP50   float64
	FrameCPUP95   float64
	FrameCPUP99   float64
	PrepP50       float64
	PrepP95       float64
	PrepP99       float64
	NoOutput      int64
	Notes         []string
	ErrCount      int
}

// frames is the honest common denominator: lane paints when comparable,
// wire bursts otherwise.
func (l *laneAgg) frames() int64 {
	if l.Paints > 0 {
		return l.Paints
	}
	return l.Bursts
}

type cellAgg struct {
	Scene  string
	Width  int
	Height int
	Lanes  []laneAgg
}

type cellKey struct {
	scene  string
	width  int
	height int
}

func medianInt64(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func medianUint64(v []uint64) uint64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]uint64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func medianFloat(v []float64) float64 {
	return percentileFloat(v, 50)
}

// percentileFloat does linear interpolation, benchstat-style.
func percentileFloat(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	rank := p / 100 * float64(len(s)-1)
	lo := int(rank)
	hi := min(lo+1, len(s)-1)
	frac := rank - float64(lo)
	return s[lo]*(1-frac) + s[hi]*frac
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// aggregate groups runs into (scene, size) cells and aggregates lanes.
func aggregate(runs []*RunResult) []cellAgg {
	cells := make(map[cellKey][]*RunResult)
	for _, r := range runs {
		k := cellKey{r.Scene, r.Width, r.Height}
		cells[k] = append(cells[k], r)
	}
	keys := make([]cellKey, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		scA, okA := sceneFromName(keys[i].scene)
		scB, okB := sceneFromName(keys[j].scene)
		ia, ib := int(scA), int(scB)
		if !okA {
			ia = 99
		}
		if !okB {
			ib = 99
		}
		if ia != ib {
			return ia < ib
		}
		if keys[i].width != keys[j].width {
			return keys[i].width < keys[j].width
		}
		return keys[i].height < keys[j].height
	})

	out := make([]cellAgg, 0, len(keys))
	for _, k := range keys {
		cell := cellAgg{Scene: k.scene, Width: k.width, Height: k.height}
		for _, lane := range laneNames {
			var laneRuns []*RunResult
			for _, r := range cells[k] {
				if r.Adapter == lane {
					laneRuns = append(laneRuns, r)
				}
			}
			if len(laneRuns) == 0 {
				continue
			}
			cell.Lanes = append(cell.Lanes, aggregateLane(lane, laneRuns))
		}
		out = append(out, cell)
	}
	return out
}

func aggregateLane(laneName string, runs []*RunResult) laneAgg {
	l := laneAgg{Lane: laneName, Runs: len(runs)}

	var events, bytes, bursts, paints, draws, cpu, wall, idle, noout []int64
	var allocs, mallocs []uint64
	var lats, gaps, frameCPU, prep []float64
	notes := make(map[string]bool)
	for _, r := range runs {
		l.Paints = -1 // set below from median
		events = append(events, int64(r.Events))
		bytes = append(bytes, r.Bytes)
		bursts = append(bursts, r.Bursts)
		paints = append(paints, r.Paints)
		draws = append(draws, r.Draws)
		cpu = append(cpu, r.CPUUs)
		wall = append(wall, r.WallMs)
		idle = append(idle, r.IdleCPUUs)
		noout = append(noout, int64(r.NoOutput))
		allocs = append(allocs, r.AllocB)
		mallocs = append(mallocs, r.Mallocs)
		lats = append(lats, r.BatchLatMs...)
		gaps = append(gaps, r.FrameGapMs...)
		frameCPU = append(frameCPU, r.FrameCPUUs...)
		prep = append(prep, r.FramePrepUs...)
		for _, n := range r.Notes {
			notes[n] = true
		}
		if r.Error != "" {
			l.ErrCount++
			notes["error: "+truncate(r.Error, 80)] = true
		}
	}

	l.Events = medianInt64(events)
	l.Bytes = medianInt64(bytes)
	l.Bursts = medianInt64(bursts)
	l.Paints = medianInt64(paints)
	l.Draws = medianInt64(draws)
	l.CPUUs = medianInt64(cpu)
	l.WallMs = medianInt64(wall)
	l.Mallocs = medianUint64(mallocs)
	medianAlloc := medianUint64(allocs)
	l.NoOutput = medianInt64(noout)
	if l.NoOutput > 0 {
		notes[fmt.Sprintf("no-output units (latency omitted): %d", l.NoOutput)] = true
	}

	l.MsPerFrame = 0
	l.LatP50 = medianFloat(lats)
	l.LatP95 = percentileFloat(lats, 95)
	l.GapP50 = medianFloat(gaps)
	l.GapP95 = percentileFloat(gaps, 95)
	l.GapP99 = percentileFloat(gaps, 99)
	l.FrameCPUP50 = medianFloat(frameCPU)
	l.FrameCPUP95 = percentileFloat(frameCPU, 95)
	l.FrameCPUP99 = percentileFloat(frameCPU, 99)
	l.PrepP50 = medianFloat(prep)
	l.PrepP95 = percentileFloat(prep, 95)
	l.PrepP99 = percentileFloat(prep, 99)
	l.IdleCPUPct = float64(medianInt64(idle)) / float64(idleWindowUs) * 100

	frames := l.frames()
	if frames > 0 {
		l.CPUPerFrame = float64(l.CPUUs) / float64(frames)
		l.MsPerFrame = float64(l.WallMs) / float64(frames)
		l.BytesPerFrame = float64(l.Bytes) / float64(frames)
		l.AllocPerFrame = float64(medianAlloc) / float64(frames)
	} else {
		l.Notes = append(l.Notes, "no frames observed: lane produced no paint count and no wire output in this scene")
	}

	for n := range notes {
		l.Notes = append(l.Notes, n)
	}
	sort.Strings(l.Notes)
	return l
}

// renderReport writes the human-facing markdown report.
func renderReport(spec machineSpec, aggs []cellAgg, quick bool) string {
	var b strings.Builder
	b.WriteString("# lux benchmark report\n\n")

	mode := "full (3 rounds per cell, 120x40 and 240x80)"
	if quick {
		mode = "quick smoke (1 round, 120x40 only, shrunken pacing — smoke-level, NOT CITABLE)"
	}
	fmt.Fprintf(&b, "Mode: %s\n\n", mode)

	b.WriteString("## Machine\n\n")
	cpu := spec.CPUModel
	if cpu == "" {
		cpu = "(unreported)"
	}
	fmt.Fprintf(&b, "- CPU: %s, %d cores\n", cpu, spec.Cores)
	fmt.Fprintf(&b, "- Memory: %.1f GiB\n", float64(spec.MemTotalB)/(1<<30))
	fmt.Fprintf(&b, "- Kernel: %s %s\n", spec.Kernel, spec.Machine)
	fmt.Fprintf(&b, "- Go: %s (%s/%s)\n", spec.GoVersion, spec.GOOS, spec.GOARCH)
	if spec.Image != "" {
		fmt.Fprintf(&b, "- Image: %s\n", spec.Image)
	}
	fmt.Fprintf(&b, "- Collected: %s\n\n", spec.Collected.Format(time.RFC3339))

	b.WriteString("## Reproduce\n\n")
	b.WriteString("From the repository root:\n\n")
	b.WriteString("```sh\n")
	b.WriteString("docker run --rm -v luxbenchmod:/go/pkg/mod -v \"$PWD\":/src -w /src \\\n")
	b.WriteString("  -e TERM=xterm-256color -e COLORTERM=truecolor -e BENCH_IMAGE=golang:1.26-alpine \\\n")
	b.WriteString("  golang:1.26-alpine sh benchmarks/run.sh\n")
	b.WriteString("```\n\n")

	b.WriteString("## How to read the lanes\n\n")
	b.WriteString("- **events** = input keys (or resizes) driven; identical byte streams for every lane.\n")
	b.WriteString("- **bursts** = wire truth: output read from the pty master, grouped by >=4 ms of silence.\n")
	b.WriteString("- **paints** = lane's own paint counter; semantics differ per lane (see below), and `-1` means not comparable.\n")
	b.WriteString("  - lux: synchronous `Frame.OnPresented` callback — one per presented frame.\n")
	b.WriteString("  - tview: one paint per `Draw`+flush; redraws are serialized through `QueueUpdateDraw`.\n")
	b.WriteString("  - tea: renderer coalesces to its flush rate, so paints are `-1`; compare **bursts** instead.\n")
	b.WriteString("- **draws** = lane view/draw invocations (lux `View`, tea `View`, tview `Draw`).\n")
	b.WriteString("- **CPU us/frame**, **ms/frame**, **bytes/frame**, **alloc B/frame** are divided by the median frame count (paints, or bursts when paints are `-1`); ms/frame includes pacing sleeps. A `—` means the lane produced no measurable frames (no paints, no wire output), so per-frame division has no honest denominator.\n")
	b.WriteString("- **CPU p50/p95/p99 (us)** = per-frame process-CPU distribution, sampled through the process CPU clock at each presented frame (lux only; lanes without a presentation callback report `—`). The delta between two consecutive presents covers one full frame pipeline — input parse, update, view, compose, diff, encode, flush — plus in-process background work, so it shares the process-level semantics of the mean. The tail is what a mean hides: a lane can average a few us/frame while every 20th frame costs an order of magnitude more. The mean can also sit far *above* p50 in the same run: the mean's window extends to the final settle, whose output-wait loop polls every 2 ms, so a scene that emits no tail output (e.g. static, whose diff is empty) burns seconds of harness wait CPU that lands in the mean but never in the distribution — the samples telescope only between the first and last presented frame.\n")
	b.WriteString("- **prep p50/p95/p99 (us)** = the runtime's own per-frame preparation wall time from `Frame.OnPresentedWithMetrics` — Update + View + diff/encode, excluding terminal flush, scheduling and callbacks (lux only). This is the figure the 8 ms preparation budget is judged against; it is wall time, not CPU, and not input-to-screen latency.\n")
	b.WriteString("- **idle CPU%** = CPU during a 2 s quiet window after the run, as % of one core.\n")
	b.WriteString("- **lat p50/p95** = input-to-last-output latency, ms, where the scene defines it; the wait requires output causally after the input, and units with no output are counted in notes (`no-output units`), never as 0 ms.\n")
	b.WriteString("- **gap p50/p95/p99** = burst-to-burst gap distribution, ms.\n")
	b.WriteString("- Each lane renders through its own idiomatic API (lux row strings + cell puts, tea lipgloss styled lines, tview per-cell `SetContent`), so this measures frameworks, not applications. All tuning is defaults-only.\n")
	b.WriteString("- **ratatui is not measured**: it is a Rust crate, not a Go dependency; no cross-language numbers are invented.\n")
	b.WriteString("- Numbers are comparable only within this machine's run; nothing is copied across machines.\n\n")

	for _, cell := range aggs {
		fmt.Fprintf(&b, "## Scene %s %dx%d\n\n", cell.Scene, cell.Width, cell.Height)
		b.WriteString("| lane | events | bursts | paints | draws | CPU us/frame | CPU p50/p95/p99 (us) | prep p50/p95/p99 (us) | ms/frame | bytes/frame | alloc B/frame | idle CPU% | lat p50/p95 | gap p50/p95/p99 | notes |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, l := range cell.Lanes {
			lat := "—"
			if l.LatP50 > 0 || l.LatP95 > 0 {
				lat = fmt.Sprintf("%.1f / %.1f", l.LatP50, l.LatP95)
			}
			gap := "—"
			if l.GapP50 > 0 {
				gap = fmt.Sprintf("%.1f / %.1f / %.1f", l.GapP50, l.GapP95, l.GapP99)
			}
			cpuF, msF, bytesF, allocF := "—", "—", "—", "—"
			if l.frames() > 0 {
				cpuF = fmt.Sprintf("%.1f", l.CPUPerFrame)
				msF = fmt.Sprintf("%.2f", l.MsPerFrame)
				bytesF = fmt.Sprintf("%.0f", l.BytesPerFrame)
				allocF = fmt.Sprintf("%.0f", l.AllocPerFrame)
			}
			cpuP := "—"
			if l.FrameCPUP50 > 0 || l.FrameCPUP95 > 0 || l.FrameCPUP99 > 0 {
				cpuP = fmt.Sprintf("%.1f / %.1f / %.1f", l.FrameCPUP50, l.FrameCPUP95, l.FrameCPUP99)
			}
			prepP := "—"
			if l.PrepP50 > 0 || l.PrepP95 > 0 || l.PrepP99 > 0 {
				prepP = fmt.Sprintf("%.1f / %.1f / %.1f", l.PrepP50, l.PrepP95, l.PrepP99)
			}
			notes := "—"
			if len(l.Notes) > 0 {
				notes = strings.Join(l.Notes, "; ")
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s | %.2f | %s | %s | %s |\n",
				l.Lane, l.Events, l.Bursts, l.Paints, l.Draws,
				cpuF, cpuP, prepP, msF, bytesF, allocF,
				l.IdleCPUPct, lat, gap, notes)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderBenchstat emits a benchstat-shaped metrics file.
func renderBenchstat(spec machineSpec, aggs []cellAgg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "goos: %s\ngoarch: %s\npkg: github.com/GofMan5/lux/benchmarks\n\n", spec.GOOS, spec.GOARCH)
	for _, cell := range aggs {
		for _, l := range cell.Lanes {
			if l.frames() <= 0 {
				// No measurable frames: per-frame numbers would divide by
				// zero, and idle-only entries confuse benchstat comparisons.
				continue
			}
			fmt.Fprintf(&b, "Benchmark%s%s%dx%d-%d\n",
				capitalize(l.Lane), capitalize(cell.Scene), cell.Width, cell.Height, spec.Cores)
			fmt.Fprintf(&b, "  cpu_us_per_frame=%.1f\n", l.CPUPerFrame)
			if l.FrameCPUP50 > 0 || l.FrameCPUP95 > 0 || l.FrameCPUP99 > 0 {
				fmt.Fprintf(&b, "  cpu_us_per_frame_p50=%.1f\n", l.FrameCPUP50)
				fmt.Fprintf(&b, "  cpu_us_per_frame_p95=%.1f\n", l.FrameCPUP95)
				fmt.Fprintf(&b, "  cpu_us_per_frame_p99=%.1f\n", l.FrameCPUP99)
			}
			if l.PrepP50 > 0 || l.PrepP95 > 0 || l.PrepP99 > 0 {
				fmt.Fprintf(&b, "  prep_us_per_frame_p50=%.1f\n", l.PrepP50)
				fmt.Fprintf(&b, "  prep_us_per_frame_p95=%.1f\n", l.PrepP95)
				fmt.Fprintf(&b, "  prep_us_per_frame_p99=%.1f\n", l.PrepP99)
			}
			fmt.Fprintf(&b, "  bytes_per_frame=%.0f\n", l.BytesPerFrame)
			fmt.Fprintf(&b, "  alloc_b_per_frame=%.0f\n", l.AllocPerFrame)
			fmt.Fprintf(&b, "  idle_cpu_pct=%.2f\n", l.IdleCPUPct)
			if l.NoOutput > 0 {
				fmt.Fprintf(&b, "  no_output_units=%d\n", l.NoOutput)
			}
			if l.LatP50 > 0 || l.LatP95 > 0 {
				fmt.Fprintf(&b, "  batch_latency_ms_p50=%.2f\n", l.LatP50)
				fmt.Fprintf(&b, "  batch_latency_ms_p95=%.2f\n", l.LatP95)
			}
			if l.GapP50 > 0 {
				fmt.Fprintf(&b, "  frame_gap_ms_p50=%.2f\n", l.GapP50)
				fmt.Fprintf(&b, "  frame_gap_ms_p95=%.2f\n", l.GapP95)
				fmt.Fprintf(&b, "  frame_gap_ms_p99=%.2f\n", l.GapP99)
			}
		}
	}
	return b.String()
}
