// Parent mode: spawns one child process per (lane, scene, size, round) and
// aggregates the JSON results into report.md / benchstat.txt.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const totalRounds = 3

type job struct {
	lane  string
	sc    sceneID
	w, h  int
	round int
}

func main() {
	os.Exit(run())
}

func run() int {
	// Child mode is dispatched before flag parsing: "--child" is not a
	// defined flag and the child argv carries no flags.
	if len(os.Args) > 1 && os.Args[1] == "--child" {
		return runChildArgs(os.Args[2:])
	}

	quick := flag.Bool("quick", false, "smoke mode: 1 round, 120x40 only, shrunken pacing")
	outDir := flag.String("out", "results", "output directory")
	reportOnly := flag.Bool("report-only", false, "rebuild report.md and benchstat.txt from existing all-runs.json and machine.json without measuring")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: bench [-quick] [-out DIR]")
		fmt.Fprintln(os.Stderr, "       bench --child LANE SCENE W H ROUND QUICK OUT")
		fmt.Fprintln(os.Stderr, "       bench -report-only [-out DIR]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *reportOnly {
		return rebuildOutputs(*outDir)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rounds := totalRounds
	if *quick {
		rounds = 1
	}
	spec := gatherMachineSpec()

	rawDir := filepath.Join(*outDir, "raw")
	logDir := filepath.Join(*outDir, "logs")
	for _, d := range []string{rawDir, logDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", d, err)
			return 2
		}
	}

	sizes := [][2]int{{120, 40}, {240, 80}}
	if *quick {
		sizes = sizes[:1]
	}

	var jobs []job
	for r := 1; r <= rounds; r++ {
		for _, sc := range allScenesForRun() {
			for _, lane := range lanesForScene(sc) {
				for _, size := range sizes {
					jobs = append(jobs, job{lane, sc, size[0], size[1], r})
				}
			}
		}
	}

	results := make([]*RunResult, 0, len(jobs))
	failed := 0
	startAll := time.Now()
	for i, j := range jobs {
		fmt.Printf("[%d/%d] %s %s %dx%d r%d: ", i+1, len(jobs),
			j.lane, j.sc.name(), j.w, j.h, j.round)
		start := time.Now()
		res := spawnChild(self, j, *quick, rawDir, logDir)
		results = append(results, res)
		status := fmt.Sprintf("%.1fs", time.Since(start).Seconds())
		if res.Error != "" {
			failed++
			status += " ERROR: " + truncate(res.Error, 100)
		}
		fmt.Println(status)
	}

	if err := writeOutputs(*outDir, spec, results, *quick); err != nil {
		fmt.Fprintf(os.Stderr, "write outputs: %v\n", err)
		return 2
	}
	fmt.Printf("done: %d runs, %d failed, total %.1f min\n",
		len(results), failed, time.Since(startAll).Minutes())
	if failed > 0 {
		return 1
	}
	return 0
}

// rebuildOutputs regenerates the derived artifacts (report.md,
// benchstat.txt) from the recorded all-runs.json and machine.json. The raw
// measurements stay untouched, so report-formatting changes never require
// re-running the measurement phase.
func rebuildOutputs(outDir string) int {
	var results []*RunResult
	raw, err := os.ReadFile(filepath.Join(outDir, "all-runs.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read all-runs.json: %v\n", err)
		return 2
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		fmt.Fprintf(os.Stderr, "parse all-runs.json: %v\n", err)
		return 2
	}
	quick := false
	for _, r := range results {
		if r.Quick {
			quick = true
			break
		}
	}

	specRaw, err := os.ReadFile(filepath.Join(outDir, "machine.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read machine.json: %v\n", err)
		return 2
	}
	var spec machineSpec
	if err := json.Unmarshal(specRaw, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "parse machine.json: %v\n", err)
		return 2
	}

	aggs := aggregate(results)
	report := renderReport(spec, aggs, quick)
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(report), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		return 2
	}
	bench := renderBenchstat(spec, aggs)
	if err := os.WriteFile(filepath.Join(outDir, "benchstat.txt"), []byte(bench), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write benchstat: %v\n", err)
		return 2
	}
	fmt.Printf("rebuilt report.md and benchstat.txt from %d runs\n", len(results))
	return 0
}

// allScenesForRun is the comparable scene set plus the lux-only scene 10.
func allScenesForRun() []sceneID {
	scenes := comparableScenes()
	return append(scenes, scSubcell)
}

func lanesForScene(sc sceneID) []string {
	if sc == scSubcell {
		return []string{"lux"}
	}
	return laneNames
}

func runChildArgs(args []string) int {
	if len(args) != 7 {
		fmt.Fprintln(os.Stderr, "child: expected LANE SCENE W H ROUND QUICK OUT")
		return 2
	}
	w, err1 := strconv.Atoi(args[2])
	h, err2 := strconv.Atoi(args[3])
	round, err3 := strconv.Atoi(args[4])
	quick, err4 := strconv.ParseBool(args[5])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		fmt.Fprintln(os.Stderr, "child: bad numeric argument")
		return 2
	}
	return runChild(args[0], args[1], w, h, round, quick, args[6])
}

// spawnChild runs one measurement child as a session leader so it can own
// the pty slave as its controlling terminal.
func spawnChild(self string, j job, quick bool, rawDir, logDir string) *RunResult {
	base := fmt.Sprintf("%s-%s-%dx%d-r%d", j.lane, j.sc.name(), j.w, j.h, j.round)
	rawPath := filepath.Join(rawDir, base+".json")
	logPath := filepath.Join(logDir, base+".log")

	logFile, err := os.Create(logPath)
	if err != nil {
		return errorResult(j, quick, fmt.Sprintf("create log: %v", err))
	}
	defer logFile.Close()

	cmd := exec.Command(self, "--child",
		j.lane, j.sc.name(),
		strconv.Itoa(j.w), strconv.Itoa(j.h), strconv.Itoa(j.round),
		strconv.FormatBool(quick), rawPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		// The child still writes a JSON result on most failures.
		if res, rerr := readResultFile(rawPath); rerr == nil {
			return res
		}
		return errorResult(j, quick, fmt.Sprintf("child process: %v", err))
	}
	res, err := readResultFile(rawPath)
	if err != nil {
		return errorResult(j, quick, fmt.Sprintf("read result: %v", err))
	}
	return res
}

func errorResult(j job, quick bool, msg string) *RunResult {
	return &RunResult{
		Adapter: j.lane,
		Scene:   j.sc.name(),
		Width:   j.w,
		Height:  j.h,
		Round:   j.round,
		Quick:   quick,
		Paints:  -1,
		Error:   msg,
	}
}

func writeOutputs(outDir string, spec machineSpec, results []*RunResult, quick bool) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "all-runs.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	data, err = json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "machine.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	aggs := aggregate(results)
	report := renderReport(spec, aggs, quick)
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(report), 0o644); err != nil {
		return err
	}
	bench := renderBenchstat(spec, aggs)
	return os.WriteFile(filepath.Join(outDir, "benchstat.txt"), []byte(bench), 0o644)
}
