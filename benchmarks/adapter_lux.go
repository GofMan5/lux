// lux lane: the lux program itself, default options, real pty as IO.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
)

type luxLane struct {
	model  *luxModel
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	runErr error

	paints atomic.Int64
	draws  atomic.Int64

	frameCPUMu sync.Mutex
	frameCPU   []float64 // per-frame process-CPU deltas (us), measured window
	lastCPU    float64   // CPU clock at the previous present (0 = no baseline yet)
	framePrep  []float64 // per-frame Update+View+diff/encode wall time (us), no flush
}

func newLuxLane() *luxLane {
	return &luxLane{done: make(chan struct{})}
}

// rowStyle is a precomputed row: the string plus its truecolor style.
type rowStyle struct {
	text  string
	style render.Style
}

type luxModel struct {
	lane *luxLane
	sc   sceneID
	w, h int

	step int

	rows    []rowStyle // base rows; the list scene keeps all 10k
	egcRows []rowStyle
	tallDoc string // one precomputed document, deliberately not virtualized

	cursorStyle    render.Style
	highlightStyle render.Style

	renderer *scene3d.Renderer
}

func (l *luxLane) name() string { return "lux" }

func (l *luxLane) start(slave *os.File, sc sceneID, w, h int) error {
	model := &luxModel{lane: l, sc: sc, w: w, h: h}
	model.precompute()
	l.model = model

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel

	go func() {
		defer close(l.done)
		err := lux.Run(ctx, model,
			lux.WithIO(slave, slave),
			lux.WithFallbackSize(w, h))
		l.mu.Lock()
		l.runErr = err
		l.mu.Unlock()
	}()
	return nil
}

func (l *luxLane) inputTick() {}

func (l *luxLane) counts() (paints int64, draws int64) {
	return l.paints.Load(), l.draws.Load()
}

func (l *luxLane) resetCounts() {
	l.paints.Store(0)
	l.draws.Store(0)
	l.frameCPUMu.Lock()
	l.frameCPU = l.frameCPU[:0]
	l.lastCPU = 0
	l.framePrep = l.framePrep[:0]
	l.frameCPUMu.Unlock()
}

// samplePrep records the runtime's own preparation time for one presented
// frame. Unlike the CPU delta it needs no baseline and excludes the flush,
// so it is the preparation-only budget figure, not process cost.
func (l *luxLane) samplePrep(metrics lux.Metrics) {
	if !metrics.BackendTiming {
		return
	}
	l.frameCPUMu.Lock()
	l.framePrep = append(l.framePrep, float64(metrics.PreparationTime)/float64(time.Microsecond))
	l.frameCPUMu.Unlock()
}

func (l *luxLane) framePreps() []float64 {
	l.frameCPUMu.Lock()
	defer l.frameCPUMu.Unlock()
	return append([]float64(nil), l.framePrep...)
}

// sampleFrameCPU runs inside the presentation callback of each frame:
// it reads the process CPU clock and records the delta since the
// previous present. That delta covers exactly one frame pipeline —
// input parse, update, view, compose, diff, encode, flush — plus any
// background work the process did between the two presents. The first
// sample after start or reset only establishes the baseline; there is
// no earlier present to subtract from yet.
func (l *luxLane) sampleFrameCPU() {
	now, ok := processCPUMicros()
	if !ok {
		return
	}
	l.frameCPUMu.Lock()
	defer l.frameCPUMu.Unlock()
	if l.lastCPU == 0 {
		l.lastCPU = now
		return
	}
	l.frameCPU = append(l.frameCPU, now-l.lastCPU)
	l.lastCPU = now
}

func (l *luxLane) frameCPUs() []float64 {
	l.frameCPUMu.Lock()
	defer l.frameCPUMu.Unlock()
	return append([]float64(nil), l.frameCPU...)
}

func (l *luxLane) stop() error {
	l.cancel()
	select {
	case <-l.done:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("lux.Run did not exit within 10s of cancellation")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.runErr != nil && !errors.Is(l.runErr, context.Canceled) {
		return l.runErr
	}
	return nil
}

func (m *luxModel) precompute() {
	m.cursorStyle = render.Style{Foreground: render.RGBColor(0xff, 0x30, 0x30)}
	m.highlightStyle = render.Style{Attrs: render.AttrReverse}

	if m.sc == scList {
		m.rows = make([]rowStyle, listRowCount)
		for index := range m.rows {
			m.rows[index] = rowStyle{
				text:  listRow(index, m.w),
				style: fgStyle(rowColor(index)),
			}
		}
	} else if isRowScene(m.sc) {
		width := maxRowWidth(m.sc, m.w)
		m.rows = make([]rowStyle, m.h)
		for y := range m.rows {
			m.rows[y] = rowStyle{
				text:  rowText(y, width),
				style: fgStyle(rowColor(y)),
			}
		}
	}
	if m.sc == scEGC {
		m.egcRows = make([]rowStyle, m.h)
		for y := range m.egcRows {
			m.egcRows[y] = rowStyle{
				text:  egcRow(y, m.w),
				style: fgStyle(rowColor(y)),
			}
		}
	}
	if m.sc == scTall {
		var doc strings.Builder
		doc.Grow(tallRowCount * (m.w + 1))
		for row := 0; row < tallRowCount; row++ {
			if row > 0 {
				doc.WriteByte('\n')
			}
			doc.WriteString(listRow(row, m.w))
		}
		m.tallDoc = doc.String()
	}
	if m.sc == scSubcell {
		m.renderer = scene3d.NewRenderer()
		m.renderer.DoubleSided = true
	}
}

func (m *luxModel) Update(e event.Event) lux.Command {
	switch e.Kind {
	case event.Key, event.Text:
		m.step++
	}
	return nil
}

func (m *luxModel) View(f *lux.Frame) {
	if f == nil || f.Canvas == nil {
		return
	}
	m.lane.draws.Add(1)
	f.OnPresentedWithMetrics(func(metrics lux.Metrics) {
		m.lane.paints.Add(1)
		m.lane.sampleFrameCPU()
		m.lane.samplePrep(metrics)
	})

	canvas := f.Canvas
	w, h := canvas.Size()
	switch m.sc {
	case scStatic, scResize:
		for y := 0; y < h && y < len(m.rows); y++ {
			canvas.Text(0, y, m.rows[y].text, m.rows[y].style)
		}

	case scOneCell, scRTT, scDamage, scInputBurst:
		m.drawRowsWithOverride(canvas, w, h)

	case scFull:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				ch, fg, _ := cellOverride(scFull, m.step, x, y, w, h)
				canvas.PutCell(x, y, render.Cell{Grapheme: ch, Width: 1, Style: fgStyle(fg)})
			}
		}

	case scList:
		offset := listOffset(m.step, h)
		for y := 0; y < h && offset+y < len(m.rows); y++ {
			row := m.rows[offset+y]
			canvas.Text(0, y, row.text, row.style)
		}

	case scEGC:
		hl := egcHighlightRow(m.step, h)
		for y := 0; y < h && y < len(m.egcRows); y++ {
			style := m.egcRows[y].style
			if y == hl {
				style = m.highlightStyle
			}
			canvas.Text(0, y, m.egcRows[y].text, style)
		}

	case scTall:
		offset := m.step % max(tallRowCount-h, 1)
		canvas.Text(0, -offset, m.tallDoc, m.cursorStyle)

	case scSubcell:
		m.renderer.Render(canvas, cubeScene(m.step))
	}
}

// drawRowsWithOverride paints base rows, then the scene's overridden cells
// on top — the idiomatic lux path for partial damage.
func (m *luxModel) drawRowsWithOverride(canvas *render.Canvas, w, h int) {
	for y := 0; y < h && y < len(m.rows); y++ {
		canvas.Text(0, y, m.rows[y].text, m.rows[y].style)
	}
	switch m.sc {
	case scOneCell, scRTT:
		ch, fg, has := cellOverride(m.sc, m.step, w/3, h/3, w, h)
		if has {
			canvas.Put(w/3, h/3, ch, fgStyle(fg))
		}
	case scDamage:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				ch, fg, has := cellOverride(scDamage, m.step, x, y, w, h)
				if has {
					canvas.Put(x, y, ch, fgStyle(fg))
				}
			}
		}
	case scInputBurst:
		x := cursorCell(m.step, w)
		ch, fg, has := cellOverride(scInputBurst, m.step, x, h/3, w, h)
		if has {
			canvas.Put(x, h/3, ch, fgStyle(fg))
		}
	}
}

// fgStyle builds a Style for a 0xRRGGBB value.
func fgStyle(fg uint32) render.Style {
	return render.Style{Foreground: rgbColor(fg)}
}

func rgbColor(rgb uint32) render.Color {
	return render.RGBColor(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb))
}

// cubeQuads are the cube's six faces as vertex index rings.
var cubeQuads = [6][4]int{
	{0, 1, 2, 3}, // back
	{4, 7, 6, 5}, // front
	{0, 4, 5, 1}, // bottom
	{3, 2, 6, 7}, // top
	{0, 3, 7, 4}, // left
	{1, 5, 6, 2}, // right
}

var cubeColors = [6]scene3d.Color{
	scene3d.RGB(0xd0, 0x60, 0x30),
	scene3d.RGB(0x30, 0xa0, 0x50),
	scene3d.RGB(0x40, 0x70, 0xd0),
	scene3d.RGB(0xc0, 0x40, 0xa0),
	scene3d.RGB(0xd0, 0xb0, 0x30),
	scene3d.RGB(0x30, 0xc0, 0xc0),
}

// cubeScene is the scene 10 payload: one shaded cube spinning around Y.
func cubeScene(step int) scene3d.Scene {
	const s = 1.0
	vertices := []scene3d.Vec3{
		scene3d.V3(-s, -s, -s), scene3d.V3(s, -s, -s),
		scene3d.V3(s, s, -s), scene3d.V3(-s, s, -s),
		scene3d.V3(-s, -s, s), scene3d.V3(s, -s, s),
		scene3d.V3(s, s, s), scene3d.V3(-s, s, s),
	}
	triangles := make([]scene3d.Triangle, 0, 12)
	for face, quad := range cubeQuads {
		triangles = append(triangles,
			scene3d.Triangle{A: quad[0], B: quad[1], C: quad[2], Color: cubeColors[face]},
			scene3d.Triangle{A: quad[0], B: quad[2], C: quad[3], Color: cubeColors[face]},
		)
	}
	rotation := scene3d.RotateY(float64(step) * 0.07).Mul(scene3d.RotateX(float64(step) * 0.03))
	return scene3d.Scene{
		Objects: []scene3d.Object{{
			Mesh:      scene3d.Mesh{Vertices: vertices, Triangles: triangles},
			Transform: rotation,
		}},
		Camera: scene3d.Camera{
			Position: scene3d.V3(0, 0, 4.2),
			Target:   scene3d.V3(0, 0, 0),
			Up:       scene3d.V3(0, 1, 0),
			FOVY:     scene3d.Degrees(50),
			Near:     0.1,
			Far:      100,
		},
		Light: scene3d.Light{
			Direction: scene3d.V3(-0.4, 0.7, 1).Normalize(),
			Ambient:   0.25,
			Diffuse:   0.75,
		},
	}
}
