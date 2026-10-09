package capability_test

import (
	"fmt"

	"github.com/GofMan5/lux/capability"
)

// ExamplePortable shows the fallback used for redirected output: a non-
// interactive, monochrome, no-mouse snapshot. Rendering against it is what
// keeps `tui > file.txt` honest — the same program degrades instead of
// emitting escape sequences a pipe cannot honor.
func ExamplePortable() {
	set := capability.Portable()
	fmt.Printf("terminal=%s interactive=%t color=%s mouse=%t\n",
		set.Terminal, set.Interactive, set.Color, set.Mouse)
	// Output:
	// terminal=dumb interactive=false color=Monochrome mouse=false
}

// ExampleSet shows an application adapting to negotiated features instead
// of guessing from a terminal brand: pick the color pipeline from the depth,
// opt into pixel mouse only when the terminal answered for it, and treat
// scroll regions as an optimization, never a requirement.
func ExampleSet() {
	negotiated := capability.Set{
		Terminal:         "xterm-256color",
		Interactive:      true,
		Unicode:          true,
		Color:            capability.TrueColor,
		Mouse:            true,
		PixelMouse:       true,
		KeyboardEnhanced: true,
		ScrollRegions:    true,
	}

	switch {
	case negotiated.Color >= capability.TrueColor:
		fmt.Println("colors: 24-bit pipeline")
	case negotiated.Color >= capability.ANSI256:
		fmt.Println("colors: 256-color palette")
	case negotiated.Color >= capability.ANSI16:
		fmt.Println("colors: 16-color palette")
	default:
		fmt.Println("colors: style by attribute, not hue")
	}

	fmt.Printf("mouse coordinates: %s\n", map[bool]string{true: "pixels", false: "cells"}[negotiated.PixelMouse])
	fmt.Printf("scroll strategy: %s\n", map[bool]string{true: "scroll regions", false: "repaint"}[negotiated.ScrollRegions])
	// Output:
	// colors: 24-bit pipeline
	// mouse coordinates: pixels
	// scroll strategy: scroll regions
}
