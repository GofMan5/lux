// Unit tests for the measurement-calibration fixes: causal settle waits
// and phase-aligned lane counters. These run without a pty: masterReader
// is exercised through record() directly, and lanes are constructed but
// never started.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			Paints: 4, Draws: 4, NoOutput: 2, BatchLatMs: []float64{12.5}},
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
	report, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "no-output units (latency omitted): 2") {
		t.Fatalf("report.md missing the no-output note:\n%s", report)
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
