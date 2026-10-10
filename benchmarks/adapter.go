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

	// inputTick is called right after the harness writes one key to the
	// master. Lanes whose redraw follows input directly (lux, tea) no-op:
	// their event loop drives the redraw from the parsed key. tview queues
	// an update+draw, which is the idiomatic redraw path for that lane.
	inputTick()

	// counts returns lane-true paints (-1 when the lane cannot count
	// them) and view/draw invocations.
	counts() (paints int64, draws int64)

	// resetCounts zeroes the paint/draw counters after the warmup phase
	// so they cover the same window as the wire and usage counters,
	// which are also reset right after warmup.
	resetCounts()

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
