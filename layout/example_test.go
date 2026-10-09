package layout_test

import (
	"fmt"

	"github.com/GofMan5/lux/layout"
	"github.com/GofMan5/lux/render"
)

// ExampleFlex lays out three children across 31 cells with one-cell gaps:
// the fixed panel keeps its basis, the two growing panels share the rest
// proportionally to their Grow weights. The destination slice is reused, so
// responsive layout on the frame path allocates nothing.
func ExampleFlex() {
	into := make([]render.Rect, 3)
	area := render.Rect{W: 31, H: 5}
	rects := layout.Flex(into, area, layout.Horizontal, 1,
		layout.Constraint{Basis: 10, Max: 10}, // sidebar: fixed
		layout.Constraint{Basis: 8, Grow: 1},  // main: grows
		layout.Constraint{Basis: 4, Grow: 2},  // aside: grows twice as fast
	)
	for _, rect := range rects {
		fmt.Printf("x=%d w=%d h=%d\n", rect.X, rect.W, rect.H)
	}
	// Output:
	// x=0 w=10 h=5
	// x=11 w=11 h=5
	// x=23 w=8 h=5
}

// ExampleClassify maps viewport sizes onto disclosure levels. Both axes
// count: a wide but one-line terminal collapses to Tiny, and width alone
// never upgrades a short viewport.
func ExampleClassify() {
	sizes := []render.Rect{
		{W: 100, H: 1},
		{W: 40, H: 30},
		{W: 80, H: 24},
		{W: 120, H: 40},
	}
	for _, size := range sizes {
		fmt.Println(layout.Classify(size))
	}
	// Output:
	// Tiny
	// Compact
	// Medium
	// Wide
}
