// adapter interface: one implementation per TUI framework.
package main

import (
	"fmt"
	"os"
)

// adapter boots one TUI framework on a pty slave. The harness feeds input
// by writing bytes to the pty master, so input parsing, resizes, and
// output all traverse the real terminal paths for every lane.
type adapter interface {
	name() string

	// start boots the framework with default options on the slave.
	start(slave *os.File, sc sceneID, w, h int) error

	// inputTick follows each harness key write. lux/tea redraw from parsed
	// input; tview queues its idiomatic update+draw.
	inputTick()

	// counts returns lane-true paints (-1 if unavailable) and view/draws.
	counts() (paints int64, draws int64)

	// resetCounts aligns counters with the measured wire/usage window.
	resetCounts()

	// frameCPUs returns per-present process CPU deltas (us), nil if unavailable.
	frameCPUs() []float64

	// framePreps returns per-present preparation wall time (us) excluding
	// flush, nil if the lane cannot separate it.
	framePreps() []float64

	stop() error
}

func newAdapter(name string) (adapter, error) {
	switch name {
	case "lux":
		return newLuxLane(), nil
	case "tea":
		return newTeaLane(), nil
	case "tview":
		return newTviewLane(), nil
	}
	return nil, fmt.Errorf("unknown adapter %q (want lux, tea, or tview)", name)
}

var laneNames = []string{"lux", "tea", "tview"}
