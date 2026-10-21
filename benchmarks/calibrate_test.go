// Unit tests for the measurement-calibration fixes: causal settle waits
// and phase-aligned lane counters. These run without a pty: masterReader
// is exercised through record() directly, and lanes are constructed but
// never started.
package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GofMan5/lux"
)

func TestWaitQuietSinceRejectsStaleRead(t *testing.T) {
	r := newMasterReader(nil, 0)
	r.record(16)                      // output from a *previous* action
	time.Sleep(30 * time.Millisecond) // silence far beyond quiet

	t0 := time.Now() // a new action starts now
	// The old, non-causal waitQuiet would accept this immediately.
	// The causal wait must not: no read exists at or after t0.
	ok, gotNew := r.waitQuietSince(t0, 20*time.Millisecond, 60*time.Millisecond)
	if ok {
		t.Fatal("waitQuietSince accepted silence from a read that predates t0")
	}
	if gotNew {
		t.Fatal("waitQuietSince reported new output when no read happened after t0")
	}
}

func TestWaitQuietSinceAcceptsFreshReadThenSilence(t *testing.T) {
	r := newMasterReader(nil, 0)
	t0 := time.Now()
	time.Sleep(5 * time.Millisecond)
	r.record(8) // the action's acknowledgement

	ok, gotNew := r.waitQuietSince(t0, 20*time.Millisecond, 2*time.Second)
	if !ok || !gotNew {
		t.Fatalf("waitQuietSince = (%v, %v), want (true, true) after a fresh read settled", ok, gotNew)
	}
}

func TestWaitQuietSinceTimeoutAfterFreshRead(t *testing.T) {
	r := newMasterReader(nil, 0)
	t0 := time.Now()
	r.record(4) // fresh, but quiet is longer than the timeout

	ok, gotNew := r.waitQuietSince(t0, 40*time.Millisecond, 10*time.Millisecond)
	if ok {
		t.Fatal("waitQuietSince reported settled before quiet elapsed since the fresh read")
	}
	if !gotNew {
		t.Fatal("waitQuietSince missed the read that happened after t0")
	}
}

func TestLatMsSemantics(t *testing.T) {
	t0 := time.Now()
	if got := latMs(time.Time{}, t0); got != 0 {
		t.Fatalf("latMs(zero read) = %v, want 0", got)
	}
	if got := latMs(t0.Add(-time.Second), t0); got != 0 {
		t.Fatalf("latMs(stale read) = %v, want 0", got)
	}
	got := latMs(t0.Add(10*time.Millisecond), t0)
	if got < 10 || got > 100 {
		t.Fatalf("latMs(fresh read 10ms after t0) = %v, want ~10", got)
	}
}

func TestLaneResetCountsZeroesMeasuredPhase(t *testing.T) {
	lux := newLuxLane()
	lux.paints.Store(5)
	lux.draws.Store(7)
	lux.resetCounts()
	if p, d := lux.counts(); p != 0 || d != 0 {
		t.Fatalf("lux counts after reset = (%d, %d), want (0, 0)", p, d)
	}

	tea := newTeaLane()
	tea.draws.Store(9)
	tea.resetCounts()
	if p, d := tea.counts(); p != -1 || d != 0 {
		t.Fatalf("tea counts after reset = (%d, %d), want (-1, 0)", p, d)
	}

	tv := newTviewLane()
	tv.paints.Store(3)
	tv.draws.Store(4)
	tv.resetCounts()
	if p, d := tv.counts(); p != 0 || d != 0 {
		t.Fatalf("tview counts after reset = (%d, %d), want (0, 0)", p, d)
	}
}

func TestAggregateLaneCountsNoOutput(t *testing.T) {
	runs := []*RunResult{
		{
			Adapter: "lux", Scene: "inputburst", Width: 120, Height: 40, Round: 1,
			Paints: 10, Draws: 10, NoOutput: 2,
			BatchLatMs: []float64{12.5},
		},
		{
			Adapter: "lux", Scene: "inputburst", Width: 120, Height: 40, Round: 2,
			Paints: 11, Draws: 11, NoOutput: 2,
			BatchLatMs: []float64{13.5},
		},
	}
	l := aggregateLane("lux", runs)
	if l.NoOutput != 2 {
		t.Fatalf("lane NoOutput = %d, want 2 (median across rounds)", l.NoOutput)
	}
	found := false
	for _, n := range l.Notes {
		if strings.Contains(n, "no-output units (latency omitted): 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lane notes = %v, want a no-output entry", l.Notes)
	}
	// Latencies from real units only — nothing that fakes 0 ms.
	if l.LatP50 <= 0 || l.LatP95 <= 0 {
		t.Fatalf("lat p50/p95 = %v/%v, want positive from the recorded units", l.LatP50, l.LatP95)
	}
}

func TestAggregateLaneNoOutputOmitsZeroLatency(t *testing.T) {
	// Every unit produced no output: no latency samples at all, so the
	// lane must report no latency numbers rather than 0 ms.
	runs := []*RunResult{
		{Adapter: "tea", Scene: "rtt", Width: 120, Height: 40, Round: 1,
			Paints: -1, Bursts: 4, NoOutput: 3},
		{Adapter: "tea", Scene: "rtt", Width: 120, Height: 40, Round: 2,
			Paints: -1, Bursts: 4, NoOutput: 3},
	}
	l := aggregateLane("tea", runs)
	if l.NoOutput != 3 {
		t.Fatalf("lane NoOutput = %d, want 3", l.NoOutput)
	}
	if l.LatP50 != 0 || l.LatP95 != 0 {
		t.Fatalf("lat p50/p95 = %v/%v, want 0 (absent) when all units are no-output", l.LatP50, l.LatP95)
	}
	found := false
	for _, n := range l.Notes {
		if strings.Contains(n, "no-output units (latency omitted): 3") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lane notes = %v, want a no-output entry", l.Notes)
	}
}

func TestAggregateLaneFrameCPUDistribution(t *testing.T) {
	// 1..20 pooled from two rounds: p50 lands between the two 10s and
	// interpolates to 10.5, p95 to 19.05, p99 to 20.0.
	series := make([]float64, 0, 20)
	for v := 1.0; v <= 20.0; v++ {
		series = append(series, v)
	}
	runs := []*RunResult{
		{Adapter: "lux", Scene: "static", Width: 120, Height: 40, Round: 1,
			Paints: 20, Draws: 20, FrameCPUUs: series},
		{Adapter: "lux", Scene: "static", Width: 120, Height: 40, Round: 2,
			Paints: 20, Draws: 20, FrameCPUUs: series},
	}
	l := aggregateLane("lux", runs)
	want := map[string]struct {
		got, want float64
	}{
		"p50": {l.FrameCPUP50, 10.5},
		"p95": {l.FrameCPUP95, 19.05},
		"p99": {l.FrameCPUP99, 20.0},
	}
	for name, tc := range want {
		if math.Abs(tc.got-tc.want) > 1e-6 {
			t.Fatalf("frame CPU %s = %v, want %v", name, tc.got, tc.want)
		}
	}
	if !(l.FrameCPUP50 <= l.FrameCPUP95 && l.FrameCPUP95 <= l.FrameCPUP99) {
		t.Fatalf("frame CPU percentiles not ordered: %v/%v/%v",
			l.FrameCPUP50, l.FrameCPUP95, l.FrameCPUP99)
	}

	// Lanes that cannot sample per-frame CPU report nothing, not zero.
	tea := aggregateLane("tea", []*RunResult{
		{Adapter: "tea", Scene: "static", Width: 120, Height: 40, Round: 1,
			Paints: -1, Bursts: 4},
	})
	if tea.FrameCPUP50 != 0 || tea.FrameCPUP95 != 0 || tea.FrameCPUP99 != 0 {
		t.Fatalf("tea frame CPU percentiles = %v/%v/%v, want 0 (not sampled)",
			tea.FrameCPUP50, tea.FrameCPUP95, tea.FrameCPUP99)
	}
}

func TestLuxLaneFrameCPUSamplerWindow(t *testing.T) {
	lux := newLuxLane()

	// The first sample after reset (or start) only establishes the CPU
	// baseline: there is no previous present to subtract from yet.
	lux.sampleFrameCPU()
	if got := len(lux.frameCPUs()); got != 0 {
		t.Fatalf("first sample recorded %d deltas, want 0 (baseline only)", got)
	}

	// The second sample records exactly one delta for the frame between
	// the two presents. The value may be tiny on a fast host; the count
	// is what pins the contract.
	lux.sampleFrameCPU()
	if got := len(lux.frameCPUs()); got != 1 {
		t.Fatalf("after two samples, recorded %d deltas, want 1", got)
	}

	// resetCounts covers the whole measured window: samples from the
	// warmup phase must not leak into the measured distribution, and the
	// sampler must re-establish its baseline afterwards.
	lux.resetCounts()
	if got := len(lux.frameCPUs()); got != 0 {
		t.Fatalf("resetCounts left %d frame CPU samples, want 0", got)
	}
	lux.sampleFrameCPU()
	if got := len(lux.frameCPUs()); got != 0 {
		t.Fatalf("sample right after reset recorded %d deltas, want 0 (new baseline)", got)
	}
	lux.sampleFrameCPU()
	if got := len(lux.frameCPUs()); got != 1 {
		t.Fatalf("after post-reset baseline, recorded %d deltas, want 1", got)
	}

	// frameCPUs must hand out a copy: mutating the returned slice cannot
	// corrupt the lane's samples.
	samples := lux.frameCPUs()
	if len(samples) > 0 {
		samples[0] = -1
		if lux.frameCPUs()[0] == -1 {
			t.Fatal("frameCPUs returned a slice aliasing lane state")
		}
	}
}

func TestLuxLanePrepSamplerWindow(t *testing.T) {
	l := newLuxLane()

	// A backend that cannot split flush from preparation reports nothing:
	// a zero sample would drag the budget percentiles down.
	l.samplePrep(lux.Metrics{PreparationTime: time.Millisecond})
	if got := len(l.framePreps()); got != 0 {
		t.Fatalf("untimed backend recorded %d prep samples, want 0", got)
	}
	l.samplePrep(lux.Metrics{BackendTiming: true, PreparationTime: 1500 * time.Microsecond})
	if got := l.framePreps(); len(got) != 1 || got[0] != 1500 {
		t.Fatalf("prep samples = %v, want [1500]", got)
	}
	l.framePreps()[0] = -1
	if l.framePreps()[0] == -1 {
		t.Fatal("framePreps returned a slice aliasing lane state")
	}
	l.resetCounts()
	if got := len(l.framePreps()); got != 0 {
		t.Fatalf("resetCounts left %d prep samples, want 0", got)
	}
}

func TestRebuildOutputsRegeneratesArtifacts(t *testing.T) {
	dir := t.TempDir()
	spec := machineSpec{
		GoVersion: "go1.26.9", GOOS: "linux", GOARCH: "amd64",
		CPUModel: "test-cpu", Cores: 4, Image: "golang:1.26-alpine",
	}
	specRaw, _ := json.Marshal(spec)
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), specRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	runs := []*RunResult{
		{Adapter: "lux", Scene: "inputburst", Width: 120, Height: 40, Round: 1,
			Paints: 4, Draws: 4, NoOutput: 2, BatchLatMs: []float64{12.5},
			FrameCPUUs:  []float64{2, 4, 6, 8, 10, 12, 14, 16, 18, 20},
			FramePrepUs: []float64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}},
	}
	runsRaw, _ := json.Marshal(runs)
	if err := os.WriteFile(filepath.Join(dir, "all-runs.json"), runsRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	if code := rebuildOutputs(dir); code != 0 {
		t.Fatalf("rebuildOutputs exit code = %d, want 0", code)
	}
	bench, err := os.ReadFile(filepath.Join(dir, "benchstat.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bench), "no_output_units=2") {
		t.Fatalf("benchstat.txt missing no_output_units line:\n%s", bench)
	}
	// The per-frame CPU distribution must survive into the benchstat
	// metrics: 10 samples 2..20 give p50 11.0, p95 19.1, p99 19.8.
	for _, want := range []string{
		"cpu_us_per_frame_p50=11.0",
		"cpu_us_per_frame_p95=19.1",
		"cpu_us_per_frame_p99=19.8",
		"prep_us_per_frame_p50=550.0",
		"prep_us_per_frame_p95=955.0",
		"prep_us_per_frame_p99=991.0",
	} {
		if !strings.Contains(string(bench), want) {
			t.Fatalf("benchstat.txt missing %q:\n%s", want, bench)
		}
	}
	report, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "no-output units (latency omitted): 2") {
		t.Fatalf("report.md missing the no-output note:\n%s", report)
	}
	// The report gains the per-frame CPU distribution column next to the
	// mean, so tail frames are visible, not averaged away.
	if !strings.Contains(string(report), "CPU p50/p95/p99 (us)") {
		t.Fatalf("report.md missing the CPU distribution column header:\n%s", report)
	}
	if !strings.Contains(string(report), "11.0 / 19.1 / 19.8") {
		t.Fatalf("report.md missing the frame CPU distribution values:\n%s", report)
	}
	if !strings.Contains(string(report), "prep p50/p95/p99 (us)") ||
		!strings.Contains(string(report), "550.0 / 955.0 / 991.0") {
		t.Fatalf("report.md missing the preparation distribution:\n%s", report)
	}
	// The measurement inputs must survive a rebuild untouched.
	runsAfter, err := os.ReadFile(filepath.Join(dir, "all-runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(runsAfter) != string(runsRaw) {
		t.Fatal("rebuildOutputs rewrote all-runs.json instead of only the derived artifacts")
	}
}
