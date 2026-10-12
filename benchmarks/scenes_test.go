package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/GofMan5/lux"
	"github.com/GofMan5/lux/render"
)

func TestSelectScenes(t *testing.T) {
	all, err := selectScenes("")
	if err != nil || len(all) != 11 {
		t.Fatalf("default scenes = %v, %v; want all 11", all, err)
	}
	got, err := selectScenes("tall, egc,tall")
	if err != nil || !reflect.DeepEqual(got, []sceneID{scTall, scEGC}) {
		t.Fatalf("selected scenes = %v, %v", got, err)
	}
	if _, err := selectScenes("tall,typo"); err == nil {
		t.Fatal("unknown scene must fail rather than silently skip work")
	}
	if _, err := selectScenes(","); err == nil {
		t.Fatal("empty scene names must fail")
	}
	if got := lanesForScene(scTall); !reflect.DeepEqual(got, []string{"lux"}) {
		t.Fatalf("tall lanes = %v, want lux only", got)
	}
}

func TestTallSceneDrawsScrolledDocument(t *testing.T) {
	m := &luxModel{lane: newLuxLane(), sc: scTall, w: 20, h: 3, step: 17}
	m.precompute()
	if got := strings.Count(m.tallDoc, "\n"); got != tallRowCount-1 {
		t.Fatalf("document newlines = %d, want %d", got, tallRowCount-1)
	}
	for _, step := range []int{0, 17, tallRowCount - m.h - 1, tallRowCount - m.h, tallRowCount + 1} {
		m.step = step
		canvas := render.NewCanvas(render.NewBuffer(m.w, m.h))
		m.View(&lux.Frame{Canvas: canvas})
		offset := step % (tallRowCount - m.h)
		for y := 0; y < m.h; y++ {
			want := listRow(offset+y, m.w)
			for x := 0; x < m.w; x++ {
				if got := canvas.Buffer().Cell(x, y).Grapheme; got != want[x:x+1] {
					t.Fatalf("step %d cell(%d,%d) = %q, want %q", step, x, y, got, want[x:x+1])
				}
			}
		}
	}
}
