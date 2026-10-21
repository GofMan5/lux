// tea lane: bubbletea v2 with default options on the same pty, rendered
// through the idiomatic lipgloss view path (per-row styled strings with
// styled-run assembly for partial damage).
package main

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type teaLane struct {
	model  *teaModel
	prog   *tea.Program
	done   chan struct{}
	mu     sync.Mutex
	runErr error

	draws atomic.Int64
}

func newTeaLane() *teaLane {
	return &teaLane{done: make(chan struct{})}
}

// teaModelLane breaks the model<->lane type cycle.
type teaModelLane struct {
	draws *atomic.Int64
}

type teaModel struct {
	lane *teaModelLane
	sc   sceneID
	w, h int

	step int

	rawRows  []string // unstyled base rows at full width
	rowStyle []lipgloss.Style

	listRender []string // all 10k rows, pre-rendered
	egcRaw     []string
	egcRender  []string

	styleCache map[uint32]lipgloss.Style
	hlStyle    lipgloss.Style

	staticContent string // static/resize cache at current width
	staticWidth   int
}

func (l *teaLane) name() string { return "tea" }

func (l *teaLane) start(slave *os.File, sc sceneID, w, h int) error {
	model := &teaModel{
		lane: &teaModelLane{draws: &l.draws},
		sc:   sc,
		w:    w,
		h:    h,
	}
	model.precompute()

	prog := tea.NewProgram(model, tea.WithInput(slave), tea.WithOutput(slave))
	l.prog = prog
	l.model = model
	go func() {
		defer close(l.done)
		_, err := prog.Run()
		l.mu.Lock()
		l.runErr = err
		l.mu.Unlock()
	}()
	return nil
}

func (l *teaLane) inputTick() {}

// counts: tea coalesces paints to its renderer flush rate, so paints are
// not comparable — report -1 and let metrics divide by wire bursts.
func (l *teaLane) counts() (paints int64, draws int64) {
	return -1, l.draws.Load()
}

func (l *teaLane) resetCounts() {
	l.draws.Store(0)
}

// frameCPUs: tea coalesces paints behind its renderer flush rate, so
// there is no per-frame presentation callback to sample CPU at.
func (l *teaLane) frameCPUs() []float64 { return nil }

func (l *teaLane) framePreps() []float64 { return nil }

func (l *teaLane) stop() error {
	if l.prog != nil {
		l.prog.Quit()
		select {
		case <-l.done:
		case <-time.After(5 * time.Second):
			l.prog.Kill()
			<-l.done
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.runErr
}

func hexColor(rgb uint32) color.Color {
	return lipgloss.Color(fmt.Sprintf("#%06x", rgb))
}

// maxRowWidth is the width rows are precomputed at: the resize scene
// alternates between w and w+w/3, so rows must cover the wider phase.
func maxRowWidth(sc sceneID, w int) int {
	if sc == scResize {
		return w + w/3
	}
	return w
}

func (m *teaModel) precompute() {
	m.styleCache = make(map[uint32]lipgloss.Style)
	m.hlStyle = lipgloss.NewStyle().
		Foreground(hexColor(0xffffff)).
		Background(hexColor(0x202060))

	if m.sc == scList {
		m.listRender = make([]string, listRowCount)
		for index := range m.listRender {
			m.listRender[index] = lipgloss.NewStyle().
				Foreground(hexColor(rowColor(index))).
				Render(listRow(index, m.w))
		}
		return
	}
	if isRowScene(m.sc) || m.sc == scFull {
		width := maxRowWidth(m.sc, m.w)
		m.rawRows = make([]string, m.h)
		m.rowStyle = make([]lipgloss.Style, m.h)
		for y := 0; y < m.h; y++ {
			m.rawRows[y] = rowText(y, width)
			m.rowStyle[y] = lipgloss.NewStyle().Foreground(hexColor(rowColor(y)))
		}
	}
	if m.sc == scEGC {
		m.egcRaw = make([]string, m.h)
		m.egcRender = make([]string, m.h)
		for y := 0; y < m.h; y++ {
			m.egcRaw[y] = egcRow(y, m.w)
			m.egcRender[y] = lipgloss.NewStyle().
				Foreground(hexColor(rowColor(y))).
				Render(m.egcRaw[y])
		}
	}
}

func (m *teaModel) Init() tea.Cmd {
	return nil
}

func (m *teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.step++
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.sc == scStatic || m.sc == scResize {
			m.staticContent = ""
		}
	}
	return m, nil
}

func (m *teaModel) View() tea.View {
	m.lane.draws.Add(1)
	var content string
	switch m.sc {
	case scStatic, scResize:
		content = m.staticScene()
	case scOneCell, scRTT:
		content = m.singleOverrideScene()
	case scDamage:
		content = m.damageScene()
	case scInputBurst:
		content = m.burstScene()
	case scFull:
		content = m.fullScene()
	case scList:
		content = m.listScene()
	case scEGC:
		content = m.egcScene()
	}
	return tea.View{Content: content, AltScreen: true}
}

// clip trims an ASCII base row to the current terminal width.
func (m *teaModel) clip(row string) string {
	if len(row) > m.w {
		return row[:m.w]
	}
	return row
}

// cellStyle caches the lipgloss style for a 0xRRGGBB foreground.
func (m *teaModel) cellStyle(fg uint32) lipgloss.Style {
	if s, ok := m.styleCache[fg]; ok {
		return s
	}
	s := lipgloss.NewStyle().Foreground(hexColor(fg))
	m.styleCache[fg] = s
	return s
}

// buildRow renders one row as styled runs: the whole-row base style with
// each overridden cell rendered as its own styled run.
func (m *teaModel) buildRow(y int, class func(x int) (rune, lipgloss.Style, bool)) string {
	row := m.rawRows[y]
	base := m.rowStyle[y]
	w := min(m.w, len(row))

	var b strings.Builder
	runStart := 0
	flush := func(end int) {
		if end > runStart {
			b.WriteString(base.Render(row[runStart:end]))
		}
	}
	for x := 0; x < w; x++ {
		if ch, st, ok := class(x); ok {
			flush(x)
			b.WriteString(st.Render(string(ch)))
			runStart = x + 1
		}
	}
	flush(w)
	return b.String()
}

// staticScene is fully cached: identical frames exercise the diff path,
// which is the honest idle comparison against lux's no-change diff.
func (m *teaModel) staticScene() string {
	if m.staticContent != "" && m.staticWidth == m.w {
		return m.staticContent
	}
	lines := make([]string, len(m.rawRows))
	for y := range lines {
		lines[y] = m.rowStyle[y].Render(m.clip(m.rawRows[y]))
	}
	m.staticContent = strings.Join(lines, "\n")
	m.staticWidth = m.w
	return m.staticContent
}

func (m *teaModel) singleOverrideScene() string {
	ox, oy := m.w/3, m.h/3
	lines := make([]string, len(m.rawRows))
	for i := range lines {
		yy := i
		lines[i] = m.buildRow(i, func(x int) (rune, lipgloss.Style, bool) {
			if x == ox && yy == oy {
				ch, fg, has := cellOverride(m.sc, m.step, x, yy, m.w, m.h)
				if has {
					return rune(ch[0]), m.cellStyle(fg), true
				}
			}
			return 0, lipgloss.Style{}, false
		})
	}
	return strings.Join(lines, "\n")
}

func (m *teaModel) damageScene() string {
	lines := make([]string, len(m.rawRows))
	for y := range lines {
		yy := y
		lines[y] = m.buildRow(y, func(x int) (rune, lipgloss.Style, bool) {
			ch, fg, has := cellOverride(scDamage, m.step, x, yy, m.w, m.h)
			if !has {
				return 0, lipgloss.Style{}, false
			}
			return rune(ch[0]), m.cellStyle(fg), true
		})
	}
	return strings.Join(lines, "\n")
}

func (m *teaModel) burstScene() string {
	cx, cy := cursorCell(m.step, m.w), m.h/3
	lines := make([]string, len(m.rawRows))
	for i := range lines {
		yy := i
		lines[i] = m.buildRow(i, func(x int) (rune, lipgloss.Style, bool) {
			if x == cx && yy == cy {
				return '*', m.cellStyle(0xff3030), true
			}
			return 0, lipgloss.Style{}, false
		})
	}
	return strings.Join(lines, "\n")
}

func (m *teaModel) fullScene() string {
	lines := make([]string, len(m.rawRows))
	for y := range lines {
		yy := y
		lines[y] = m.buildRow(y, func(x int) (rune, lipgloss.Style, bool) {
			ch, fg, _ := cellOverride(scFull, m.step, x, yy, m.w, m.h)
			return rune(ch[0]), m.cellStyle(fg), true
		})
	}
	return strings.Join(lines, "\n")
}

func (m *teaModel) listScene() string {
	offset := listOffset(m.step, m.h)
	if offset+m.h > len(m.listRender) {
		offset = len(m.listRender) - m.h
	}
	return strings.Join(m.listRender[offset:offset+m.h], "\n")
}

func (m *teaModel) egcScene() string {
	hl := egcHighlightRow(m.step, m.h)
	lines := make([]string, len(m.egcRender))
	for y := range lines {
		if y == hl {
			lines[y] = m.hlStyle.Render(m.egcRaw[y])
		} else {
			lines[y] = m.egcRender[y]
		}
	}
	return strings.Join(lines, "\n")
}
