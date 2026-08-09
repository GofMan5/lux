package layout

import (
	"testing"

	"github.com/lux-tui/lux/render"
)

func TestFlexShrinksAndGrows(t *testing.T) {
	constraints := []Constraint{{Basis: 20, Min: 8, Grow: 1, Shrink: 1}, {Basis: 40, Min: 12, Grow: 2, Shrink: 2}}
	areas := Flex(make([]render.Rect, 2), render.Rect{W: 100, H: 10}, Horizontal, 2, constraints...)
	if areas[0].W+areas[1].W+2 != 100 || areas[1].W <= areas[0].W {
		t.Fatalf("bad growth: %#v", areas)
	}
	areas = Flex(areas, render.Rect{W: 30, H: 10}, Horizontal, 2, constraints...)
	if areas[0].W+areas[1].W+2 != 30 || areas[0].W < 8 || areas[1].W < 12 {
		t.Fatalf("bad shrink: %#v", areas)
	}
}

func TestClassifyHonorsHeight(t *testing.T) {
	if got := Classify(render.Rect{W: 200, H: 2}); got != Tiny {
		t.Fatalf("got %v", got)
	}
}
