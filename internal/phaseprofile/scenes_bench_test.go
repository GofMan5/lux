package phaseprofile

import (
	"fmt"
	"strings"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// The light benchmark scenes (benchmarks/scene.go) in process, so their
// preparation cost can be profiled on any host: the comparative harness is
// Linux-only, the pipeline it measures is not.

const filler = "the quick brown fox jumps over the lazy dog 0123456789 "

const egcSoup = "汉字你好世界 👨‍👩‍👧‍👦 🇫🇷🇯🇵 🏳️‍🌈 🧑‍🚀 αβγ→λ ﬁ ①②③ "

type sceneModel struct {
	rows  []string
	style []render.Style
	list  bool
	egc   bool
	step  int
}

func newSceneModel(kind string, height int) *sceneModel {
	count := height
	if kind == "list" {
		count = 10000
	}
	m := &sceneModel{list: kind == "list", egc: kind == "egc"}
	for i := range count {
		text := fmt.Sprintf("%06d | ", i) + strings.Repeat(filler, 5)
		if m.egc {
			text = strings.Repeat(egcSoup, 4)
		}
		m.rows = append(m.rows, text)
		m.style = append(m.style, render.Style{Foreground: render.RGBColor(uint8(0x30+i%32), uint8(i*29), uint8(i*67))})
	}
	return m
}

func (m *sceneModel) Update(e event.Event) lux.Command {
	if e.Kind == event.Key {
		m.step++
	}
	return nil
}

func (m *sceneModel) View(f *lux.Frame) {
	_, h := f.Canvas.Size()
	offset := 0
	if m.list {
		offset = m.step % (len(m.rows) - h)
	}
	for y := 0; y < h && offset+y < len(m.rows); y++ {
		style := m.style[offset+y]
		if m.egc && y == (m.step*3)%h {
			style = render.Style{Attrs: render.AttrReverse}
		}
		f.Canvas.Text(0, y, m.rows[offset+y], style)
	}
}

func benchmarkScene(b *testing.B, kind string) {
	script := make([]event.Event, 200)
	for i := range script {
		script[i] = event.RuneKey('j', 0)
	}
	Benchmark(b, 120, 40, func() lux.Model { return newSceneModel(kind, 40) }, script)
}

func BenchmarkSceneList120x40(b *testing.B)   { benchmarkScene(b, "list") }
func BenchmarkSceneEGC120x40(b *testing.B)    { benchmarkScene(b, "egc") }
func BenchmarkSceneStatic120x40(b *testing.B) { benchmarkScene(b, "static") }

// BenchmarkSceneResize120x40 alternates 120x40 and 160x53 like the harness's
// resize scene, redrawing the static rows at every size.
func BenchmarkSceneResize120x40(b *testing.B) {
	script := make([]event.Event, 40)
	for i := range script {
		script[i] = event.ResizeTo(120+40*(1-i%2), 40+13*(1-i%2))
	}
	Benchmark(b, 120, 40, func() lux.Model { return newSceneModel("static", 53) }, script)
}
