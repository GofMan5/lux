//go:build race

package e2e

// childRaceFlags mirrors the test binary's own instrumentation into the
// child app build: when the suite runs under `go test -race`, the child is
// built with -race too, so the real TUI process is race-instrumented under
// the pty (keystrokes, resizes, paste, suspend/resume), not just the test
// code that drives it. A detected race makes the child exit with status 66
// after printing the report to its stderr — the pty, whose drain the
// wait-status assertions and output snapshots carry into the failure — so
// races inside the running application surface as a RED, not as silence.
const childRaceFlag = "-race"
