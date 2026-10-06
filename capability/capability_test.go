package capability

import "testing"

// TestPortableIsConservative checks that Portable() advertises nothing beyond
// what a dumb, redirected sink can honor: the "dumb" terminal, monochrome
// color, no optional features, and plain cell graphics.
func TestPortableIsConservative(t *testing.T) {
	got := Portable()

	if got.Terminal != "dumb" {
		t.Errorf("Portable().Terminal = %q, want %q", got.Terminal, "dumb")
	}
	if got.Color != Monochrome {
		t.Errorf("Portable().Color = %v, want %v", got.Color, Monochrome)
	}
	for _, flag := range []struct {
		name string
		set  bool
	}{
		{"Interactive", got.Interactive},
		{"CursorAddressing", got.CursorAddressing},
		{"Mouse", got.Mouse},
		{"Focus", got.Focus},
		{"BracketedPaste", got.BracketedPaste},
		{"ScrollRegions", got.ScrollRegions},
		{"SynchronizedOutput", got.SynchronizedOutput},
		{"Hyperlinks", got.Hyperlinks},
	} {
		if flag.set {
			t.Errorf("Portable().%s = true, want false", flag.name)
		}
	}
	if got.Graphics != GraphicsCells {
		t.Errorf("Portable().Graphics = %v, want %v", got.Graphics, GraphicsCells)
	}
}

// TestColorDepthOrdering asserts the numeric ordering of the ColorDepth
// constants. backend.go relies on Monochrome < ANSI16 < ANSI256 < TrueColor
// so a platform floor (e.g. "256color") can promote a Set without ever
// demoting a stronger default.
func TestColorDepthOrdering(t *testing.T) {
	ordered := []ColorDepth{Monochrome, ANSI16, ANSI256, TrueColor}
	for i := 1; i < len(ordered); i++ {
		lo, hi := ordered[i-1], ordered[i]
		if lo >= hi {
			t.Errorf("color depth ordering broken: %v (%d) is not < %v (%d)", lo, lo, hi, hi)
		}
	}
}

// TestScrollRegionsIndependent pins ScrollRegions and SynchronizedOutput as
// separate capabilities: enabling one must never imply the other.
func TestScrollRegionsIndependent(t *testing.T) {
	scrollOnly := Set{ScrollRegions: true}
	if !scrollOnly.ScrollRegions {
		t.Error("Set{ScrollRegions: true}.ScrollRegions = false, want true")
	}
	if scrollOnly.SynchronizedOutput {
		t.Error("Set{ScrollRegions: true}.SynchronizedOutput = true, want false")
	}

	syncOnly := Set{SynchronizedOutput: true}
	if syncOnly.ScrollRegions {
		t.Error("Set{SynchronizedOutput: true}.ScrollRegions = true, want false")
	}
	if !syncOnly.SynchronizedOutput {
		t.Error("Set{SynchronizedOutput: true}.SynchronizedOutput = false, want true")
	}
}

// TestGraphicsCellsIsDefaultFallback checks that a zero-value Set renders
// with cells, since GraphicsCells is the always-available fallback.
func TestGraphicsCellsIsDefaultFallback(t *testing.T) {
	var s Set
	if s.Graphics != GraphicsCells {
		t.Errorf("zero-value Set.Graphics = %v, want GraphicsCells (%v)", s.Graphics, GraphicsCells)
	}
}
