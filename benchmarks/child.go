// Child mode: one (adapter, scene, size, round) cell measured in this
// process. The parent spawned this process as a session leader with the
// pty slave as controlling terminal, so SIGWINCH and input parsing behave
// exactly like a real terminal.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

// runChild measures one cell and writes the JSON result to outPath. A
// panic anywhere in the measurement is still written as an error result.
func runChild(adapterName, sceneName string, w, h, round int, quick bool, outPath string) int {
	// Closing the pty master at teardown sends SIGHUP to the foreground
	// process group — which is this process, since it is the session
	// leader on that pty. The result must still be written, so hold the
	// hangup instead of dying mid-report.
	signal.Ignore(syscall.SIGHUP)
	sc, ok := sceneFromName(sceneName)
	if !ok {
		writeChildError(outPath, adapterName, sceneName, w, h, round, quick,
			fmt.Errorf("unknown scene %q", sceneName))
		return 1
	}
	defer func() { fmt.Fprintf(os.Stderr, "child: phase done\n") }()
	phase := func(name string) { fmt.Fprintf(os.Stderr, "child: phase %s\n", name) }
	res := childMeasurePhase(adapterName, sc, w, h, round, quick, phase)
	if err := writeResultFile(outPath, &res); err != nil {
		fmt.Fprintf(os.Stderr, "child: write result %s: %v\n", outPath, err)
		return 1
	}
	if res.Error != "" {
		return 1
	}
	return 0
}

func childMeasure(adapterName string, sc sceneID, w, h, round int, quick bool) (res RunResult) {
	return childMeasurePhase(adapterName, sc, w, h, round, quick, func(string) {})
}

func childMeasurePhase(adapterName string, sc sceneID, w, h, round int, quick bool, phase func(string)) (res RunResult) {
	res = RunResult{
		Adapter: adapterName,
		Scene:   sc.name(),
		Width:   w,
		Height:  h,
		Round:   round,
		Quick:   quick,
		Paints:  -1,
		Notes:   []string{},
	}
	defer func() {
		if r := recover(); r != nil {
			res.Error = fmt.Sprintf("panic: %v", r)
			res.Notes = append(res.Notes, "recovered panic in child")
		}
	}()

	phase("openpty")
	sess, err := openPTY(w, h)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	delay := time.Duration(0)
	if sc == scRTT {
		delay = rttLinkDelay
	}
	reader := newMasterReader(sess.master, delay)
	reader.start()

	phase("adapter-new")
	adapter, err := newAdapter(adapterName)
	if err != nil {
		res.Error = err.Error()
		sess.close()
		reader.waitDrained()
		return res
	}
	phase("adapter-start")
	if err := adapter.start(sess.slave, sc, w, h); err != nil {
		res.Error = err.Error()
		sess.close()
		reader.waitDrained()
		return res
	}

	notes := res.Notes
	failNote := func(format string, args ...any) {
		notes = append(notes, fmt.Sprintf(format, args...))
	}

	// Warmup: paced keys, then settle. Counters restart after this.
	phase("warmup")
	warmKeys := warmupTickCount(quick)
	for i := 0; i < warmKeys; i++ {
		sess.write(burstKey(i))
		adapter.inputTick()
		time.Sleep(tickInterval)
	}
	reader.waitQuiet(settleQuiet, settleTimeout)

	// Measured phase: wire, counters, and usage all start at zero here,
	// so paints/draws and bytes/bursts/CPU/alloc cover the same window.
	phase("reset")
	reader.reset()
	adapter.resetCounts()
	runtime.GC()
	runtime.GC()
	base := snapshotUsage()
	driveStart := time.Now()

	events := 0
	phase("drive")
	switch sc.drive() {
	case drivePaced:
		ticks := pacedTickCount(quick)
		ticker := time.NewTicker(tickInterval)
		for i := 0; i < ticks; i++ {
			<-ticker.C
			sess.write(burstKey(i))
			adapter.inputTick()
			events++
		}
		ticker.Stop()

	case driveBurst:
		batches := batchCount(quick)
		lats := make([]float64, 0, batches)
		for b := 0; b < batches; b++ {
			t0 := time.Now()
			for i := 0; i < burstKeys; i++ {
				sess.write(burstKey(i))
				adapter.inputTick()
				events++
			}
			ok, gotNew := reader.waitQuietSince(t0, settleQuiet, settleTimeout)
			if !gotNew {
				res.NoOutput++
				failNote("batch %d produced no output after input", b+1)
			} else if !ok {
				failNote("batch %d did not settle within %s", b+1, settleTimeout)
			}
			if gotNew {
				lats = append(lats, latMs(reader.lastReadTime(), t0))
			}
		}
		res.BatchLatMs = lats

	case driveResize:
		resizes := resizeCount(quick)
		lats := make([]float64, 0, resizes)
		for n := 0; n < resizes; n++ {
			t0 := time.Now()
			tw, th := w, h
			if n%2 == 0 {
				tw, th = w+w/3, h+h/3
			}
			if err := sess.resize(tw, th); err != nil {
				failNote("resize %d failed: %v", n+1, err)
			}
			events++
			ok, gotNew := reader.waitQuietSince(t0, resizeQuiet, settleTimeout)
			if !gotNew {
				res.NoOutput++
				failNote("resize %d produced no output", n+1)
			} else if !ok {
				failNote("resize %d did not settle within %s", n+1, settleTimeout)
			}
			if gotNew {
				lats = append(lats, latMs(reader.lastReadTime(), t0))
			}
			if elapsed := time.Since(t0); elapsed < resizeEvery {
				time.Sleep(resizeEvery - elapsed)
			}
		}
		res.BatchLatMs = lats

	case driveRTT:
		steps := rttStepCount(quick)
		lats := make([]float64, 0, steps)
		for i := 0; i < steps; i++ {
			t0 := time.Now()
			sess.write(burstKey(i))
			adapter.inputTick()
			events++
			ok, gotNew := reader.waitQuietSince(t0, rttQuiet, settleTimeout)
			if !gotNew {
				res.NoOutput++
				failNote("rtt step %d produced no output after input", i+1)
			} else if !ok {
				failNote("rtt step %d did not settle within %s", i+1, settleTimeout)
			}
			if gotNew {
				lats = append(lats, latMs(reader.lastReadTime(), t0))
			}
		}
		res.BatchLatMs = lats
	}

	phase("final-settle")
	settleStart := time.Now()
	settled, gotNew := reader.waitQuietSince(settleStart, quietFor(sc), settleTimeout)
	res.SettleMs = time.Since(settleStart).Milliseconds()
	if !gotNew {
		failNote("no output observed after the drive phase (expected tail frames)")
	} else if !settled {
		failNote("final settle timed out after %s", settleTimeout)
	}

	// Wire truth is snapshotted before stopping the adapter so teardown
	// output cannot pollute the counters.
	res.Bytes, res.Bursts = reader.stats()
	res.FrameGapMs = reader.frameGaps()

	end := snapshotUsage()
	res.WallMs = time.Since(driveStart).Milliseconds()
	res.CPUUs = end.cpuMicros - base.cpuMicros
	res.AllocB = end.alloc - base.alloc
	res.Mallocs = end.mallocs - base.mallocs

	time.Sleep(idleWindow)
	idleEnd := snapshotUsage()
	res.IdleCPUUs = idleEnd.cpuMicros - end.cpuMicros

	res.Events = events
	phase("counts")
	paints, draws := adapter.counts()
	res.Paints = paints
	res.Draws = draws

	phase("adapter-stop")
	if err := adapter.stop(); err != nil {
		failNote("adapter stop: %v", err)
	}
	phase("pty-close")
	sess.close()
	reader.waitDrained()

	phase("return")
	res.Notes = notes
	return res
}

// latMs is the input-to-last-output latency of one unit. A stale last
// read maps to 0, but callers only invoke it after waitQuietSince
// reported a read at or after t0 (gotNew), so the 0 arm is a guard, not
// a reported result — units without output are counted in NoOutput
// instead of entering the latency distribution.
func latMs(lastRead, t0 time.Time) float64 {
	if lastRead.IsZero() || lastRead.Before(t0) {
		return 0
	}
	return float64(lastRead.Sub(t0).Microseconds()) / 1000
}

func writeChildError(path, adapterName, sceneName string, w, h, round int, quick bool, err error) {
	res := RunResult{
		Adapter: adapterName,
		Scene:   sceneName,
		Width:   w,
		Height:  h,
		Round:   round,
		Quick:   quick,
		Paints:  -1,
		Error:   err.Error(),
	}
	if writeErr := writeResultFile(path, &res); writeErr != nil {
		fmt.Fprintf(os.Stderr, "child: write error result %s: %v\n", path, writeErr)
	}
}
