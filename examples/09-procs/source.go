package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// processRow is one line of the explorer: the fields a source fills are
// exactly the ones the app renders. Sources leave a metric at its zero
// value when the operating system does not report it cheaply, instead of
// inventing a number.
type processRow struct {
	pid   int
	name  string
	cpu   float64 // percent on darwin, cumulative cpu ticks on linux, zero on windows
	mem   int64   // resident bytes
	state string  // session name on windows, run state elsewhere
}

// processSource isolates the operating system behind two calls. The app
// only ever sees this seam: List receives the run's context and the
// current filter, and returns rows that already match that filter. Kill
// asks the system to terminate one process gently: taskkill posts
// WM_CLOSE on Windows and Unix sends SIGTERM, both of which are requests
// the target may ignore, so a non-zero exit is data for the UI, not a
// fatal error, because the transport (the terminal) is still healthy.
// A source must be safe to call from a Command goroutine and honor
// context cancellation.
type processSource interface {
	List(ctx context.Context, filter string) ([]processRow, error)
	Kill(ctx context.Context, pid int) error
}

// newProcessSource picks the adapter for the host. The choice is a switch
// over runtime.GOOS rather than build tags, so the example builds
// everywhere and an unsupported system fails loudly instead of quietly
// showing nothing.
func newProcessSource() (processSource, error) {
	switch runtime.GOOS {
	case "windows":
		return &tasklistSource{}, nil
	case "linux":
		return &procfsSource{}, nil
	case "darwin":
		return &psSource{}, nil
	}
	return nil, fmt.Errorf("no process source for %s", runtime.GOOS)
}

// matchesFilter reports whether a row survives the current filter: a
// case-insensitive substring of the process name. Filtering happens inside
// the source so the seam carries the query, and the app's row count is
// the source's answer, not a UI-side re-interpretation.
func matchesFilter(name, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// tasklistSource lists processes through tasklist's CSV output. Windows
// reports the image name, pid, session name, and memory there; cpu usage
// is not part of it, so the cpu column stays zero rather than guessing.
type tasklistSource struct{}

func (s *tasklistSource) List(ctx context.Context, filter string) ([]processRow, error) {
	cmd := exec.CommandContext(ctx, "tasklist", "/fo", "csv", "/nh")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("tasklist: %w", err)
	}
	records, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w", err)
	}
	rows := make([]processRow, 0, len(records))
	for _, record := range records {
		// Image Name, PID, Session Name, Session#, Mem Usage.
		if len(record) < 5 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}
		if !matchesFilter(record[0], filter) {
			continue
		}
		rows = append(rows, processRow{
			pid:   pid,
			name:  record[0],
			state: strings.ToLower(strings.TrimSpace(record[2])),
			mem:   parseMemUsage(record[4]),
		})
	}
	return rows, nil
}

// Kill ends one process through taskkill without /F: the graceful
// WM_CLOSE request, never a forced termination.
func (s *tasklistSource) Kill(ctx context.Context, pid int) error {
	return runKillCommand(ctx, "taskkill", "/PID", strconv.Itoa(pid))
}

// runKillCommand runs one termination command and turns a non-zero exit
// into an error that carries the tool's own words, so the notice line
// explains why the process survived instead of a bare exit status.
func runKillCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, firstLine(out))
	}
	return nil
}

// firstLine trims a command's output to its first non-empty line: a
// notice line has room for one sentence.
func firstLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// parseMemUsage reads tasklist's memory field, shaped like "9,216 K": the
// digits and grouping separators vary by locale, so every non-digit is
// dropped before the value is scaled from kilobytes to bytes.
func parseMemUsage(field string) int64 {
	var digits strings.Builder
	for _, r := range field {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	kb, err := strconv.ParseInt(digits.String(), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

// procfsSource walks /proc once and reads each numeric directory's stat
// file. Linux reports cpu as utime+stime clock ticks and memory as
// resident pages; a real percentage needs two samples, so the column
// carries the honest cumulative ticks instead.
type procfsSource struct{}

func (s *procfsSource) List(ctx context.Context, filter string) ([]processRow, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("/proc: %w", err)
	}
	rows := make([]processRow, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a process directory
		}
		row, err := readProcStat(pid)
		if err != nil {
			continue // the process exited mid-walk; skipping is correct
		}
		if !matchesFilter(row.name, filter) {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Kill ends one process with SIGTERM, the polite default a shell sends.
func (s *procfsSource) Kill(ctx context.Context, pid int) error {
	return runKillCommand(ctx, "kill", "-TERM", strconv.Itoa(pid))
}

// readProcStat parses /proc/<pid>/stat. The comm field is parenthesized
// and may itself contain spaces and parentheses, so the parser scans to
// the last closing parenthesis before splitting the remainder.
func readProcStat(pid int) (processRow, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return processRow{}, err
	}
	line := strings.TrimSpace(string(raw))
	open := strings.IndexByte(line, '(')
	closing := strings.LastIndexByte(line, ')')
	if open < 0 || closing < open {
		return processRow{}, fmt.Errorf("stat %d: no comm field", pid)
	}
	// The remainder starts at stat field 3 (state), so rest[i] is stat
	// field i+3: utime is field 14 -> rest[11], stime field 15 ->
	// rest[12], rss field 24 -> rest[21].
	rest := strings.Fields(line[closing+1:])
	if len(rest) < 22 {
		return processRow{}, fmt.Errorf("stat %d: short line", pid)
	}
	utime, err := strconv.ParseInt(rest[11], 10, 64)
	if err != nil {
		return processRow{}, fmt.Errorf("stat %d: utime %q", pid, rest[11])
	}
	stime, err := strconv.ParseInt(rest[12], 10, 64)
	if err != nil {
		return processRow{}, fmt.Errorf("stat %d: stime %q", pid, rest[12])
	}
	rss, err := strconv.ParseInt(rest[21], 10, 64)
	if err != nil {
		return processRow{}, fmt.Errorf("stat %d: rss %q", pid, rest[21])
	}
	return processRow{
		pid:   pid,
		name:  line[open+1 : closing],
		cpu:   float64(utime + stime),
		mem:   rss * int64(os.Getpagesize()),
		state: procState(rest[0]),
	}, nil
}

// procState maps the /proc state letter onto a stable word; unknown
// letters pass through instead of hiding data.
func procState(letter string) string {
	switch letter {
	case "R":
		return "running"
	case "S", "D":
		return "sleep"
	case "Z":
		return "zombie"
	case "T", "t":
		return "stopped"
	}
	return letter
}

// psSource lists processes through ps: pid, cpu percent, resident
// kilobytes, run state, and the command path.
type psSource struct{}

func (s *psSource) List(ctx context.Context, filter string) ([]processRow, error) {
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,pcpu=,rss=,state=,comm=")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ps: %w", err)
	}
	rows := make([]processRow, 0, 128)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			cpu = 0
		}
		kb, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			kb = 0
		}
		name := strings.Join(fields[4:], " ")
		if !matchesFilter(name, filter) {
			continue
		}
		rows = append(rows, processRow{
			pid:   pid,
			name:  name,
			cpu:   cpu,
			mem:   kb * 1024,
			state: strings.ToLower(fields[3]),
		})
	}
	return rows, nil
}

// Kill ends one process with SIGTERM, the polite default a shell sends.
func (s *psSource) Kill(ctx context.Context, pid int) error {
	return runKillCommand(ctx, "kill", "-TERM", strconv.Itoa(pid))
}
