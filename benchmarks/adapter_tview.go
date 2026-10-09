// tview lane: tview/tcell on the same pty through a custom Tty shim, drawn
// through the lane's natural per-cell SetContent API.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
	"golang.org/x/sys/unix"
)

type tviewLane struct {
	app    *tview.Application
	prim   *benchPrim
	tty    *ptyTty
	done   chan struct{}
	mu     sync.Mutex
	runErr error

	draws   atomic.Int64
	paints  atomic.Int64
	stalls  atomic.Int64
	stopped atomic.Bool
}

func newTviewLane() *tviewLane {
	return &tviewLane{done: make(chan struct{})}
}

// ptyTty adapts the pty slave to the tcell Tty interface. It mirrors
// tcell's own devTty split: ioctls (termios, window size) go through the
// original slave descriptor, while reads and writes go through a separate
// os.NewFile wrapper over a dup of the slave fd. That wrapper registers
// with the runtime poller in non-blocking mode — calling Fd() on the
// original file would silently restore blocking mode and make every read
// deadline useless — so Drain() can actually wake a blocked reader when
// tcell tears the screen down.
type ptyTty struct {
	rw *os.File // poller-registered non-blocking dup, used for Read/Write
	fd int      // cached raw fd of the slave, used for ioctls only

	mu        sync.Mutex
	old       *unix.Termios
	sigwinch  chan os.Signal
	resizeCbs []func()
}

func newPtyTty(slave *os.File) (*ptyTty, error) {
	// Fd() may put the descriptor back into blocking mode; that is fine
	// because this handle is used for ioctls only from now on.
	fd := int(slave.Fd())
	dup, err := syscall.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("dup slave fd: %w", err)
	}
	// os.NewFile only enters non-blocking mode for sockets, so make sure
	// the dup itself is non-blocking: reads then surface EAGAIN to Go's
	// poller, where a read deadline can wake them.
	if flags, ferr := unix.FcntlInt(uintptr(dup), unix.F_GETFL, 0); ferr == nil {
		if _, serr := unix.FcntlInt(uintptr(dup), unix.F_SETFL, flags|unix.O_NONBLOCK); serr != nil {
			return nil, fmt.Errorf("set O_NONBLOCK on dup fd: %w", serr)
		}
	}
	rw := os.NewFile(uintptr(dup), "pty-slave-rw")
	if rw == nil {
		return nil, fmt.Errorf("os.NewFile for dup fd %d", dup)
	}
	return &ptyTty{rw: rw, fd: fd}, nil
}

func (t *ptyTty) Start() error {
	// Clear any read deadline left by a previous Drain/Stop cycle.
	_ = t.rw.SetReadDeadline(time.Time{})
	termios, err := unix.IoctlGetTermios(t.fd, unix.TCGETS)
	if err != nil {
		return fmt.Errorf("ioctl TCGETS: %w", err)
	}
	t.mu.Lock()
	t.old = termios
	t.mu.Unlock()

	raw := *termios
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(t.fd, unix.TCSETS, &raw); err != nil {
		return fmt.Errorf("ioctl TCSETS: %w", err)
	}

	t.sigwinch = make(chan os.Signal, 8)
	signal.Notify(t.sigwinch, syscall.SIGWINCH)
	go func() {
		for range t.sigwinch {
			t.mu.Lock()
			cbs := make([]func(), len(t.resizeCbs))
			copy(cbs, t.resizeCbs)
			t.mu.Unlock()
			for _, cb := range cbs {
				cb()
			}
		}
	}()
	return nil
}

func (t *ptyTty) Stop() error {
	// Wake any reader blocked in Read (mirrors tcell's devTty.Stop).
	_ = t.rw.SetReadDeadline(time.Now())
	t.mu.Lock()
	ch := t.sigwinch
	t.sigwinch = nil
	old := t.old
	t.old = nil
	t.mu.Unlock()
	if ch != nil {
		signal.Stop(ch)
		close(ch)
	}
	if old != nil {
		if err := unix.IoctlSetTermios(t.fd, unix.TCSETS, old); err != nil {
			return err
		}
	}
	return nil
}

func (t *ptyTty) Read(p []byte) (int, error)  { return t.rw.Read(p) }
func (t *ptyTty) Write(p []byte) (int, error) { return t.rw.Write(p) }

// Drain wakes a reader blocked in Read with an already-expired deadline, so
// tcell's input loop can exit before Stop is called. This is exactly the
// workaround tcell's own devTty performs on /dev/tty.
func (t *ptyTty) Drain() error {
	_ = t.rw.SetReadDeadline(time.Now())
	termios, err := unix.IoctlGetTermios(t.fd, unix.TCGETS)
	if err != nil {
		return nil
	}
	raw := *termios
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	_ = unix.IoctlSetTermios(t.fd, unix.TCSETS, &raw)
	return nil
}

// NotifyResize registers cb, or unregisters every handler when cb is nil
// (the semantics tcell relies on during shutdown).
func (t *ptyTty) NotifyResize(cb func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cb == nil {
		t.resizeCbs = nil
		return
	}
	t.resizeCbs = append(t.resizeCbs, cb)
}

func (t *ptyTty) WindowSize() (tcell.WindowSize, error) {
	ws, err := unix.IoctlGetWinsize(t.fd, unix.TIOCGWINSZ)
	if err != nil {
		return tcell.WindowSize{}, err
	}
	return tcell.WindowSize{Width: int(ws.Col), Height: int(ws.Row)}, nil
}

// Close releases the dup this adapter created. The pty pair itself stays
// open: its lifecycle belongs to the benchmark session.
func (t *ptyTty) Close() error { return t.rw.Close() }

// benchPrim is the root primitive: tview's Box plus scene drawing.
type benchPrim struct {
	*tview.Box
	lane *tviewLane
	sc   sceneID
	w, h int

	step int

	rowRunes  [][]rune
	rowColor  []tcell.Color
	listRunes [][]rune
	listColor []tcell.Color
	egcRows   [][]egcCell
	egcColor  []tcell.Color

	colorCache map[uint32]tcell.Color
	hlStyle    tcell.Style
}

// egcCell is one grapheme cluster positioned by its own display width.
type egcCell struct {
	main      rune
	combining []rune
	w         int
}

func (l *tviewLane) name() string { return "tview" }

func (l *tviewLane) start(slave *os.File, sc sceneID, w, h int) error {
	tty, err := newPtyTty(slave)
	if err != nil {
		return fmt.Errorf("pty tty: %w", err)
	}
	screen, err := tcell.NewTerminfoScreenFromTty(tty)
	if err != nil {
		tty.Close()
		return fmt.Errorf("terminfo screen: %w", err)
	}
	l.tty = tty
	l.prim = newBenchPrim(l, sc, w, h)

	app := tview.NewApplication()
	app.SetScreen(screen)
	app.SetRoot(l.prim, true)
	l.app = app

	go func() {
		defer close(l.done)
		if err := app.Run(); err != nil {
			l.mu.Lock()
			l.runErr = err
			l.mu.Unlock()
		}
	}()
	return nil
}

// inputTick queues an update+draw per input event: tview's redraw is
// driven by queued updates, not by raw input arrival. QueueUpdateDraw
// blocks until the draw completes, which serializes bursts honestly —
// but if the application died it would block forever, so the wait is
// time-bounded and stalls are counted.
func (l *tviewLane) inputTick() {
	if l.stopped.Load() || l.app == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.app.QueueUpdateDraw(func() { l.prim.advance() })
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if l.stalls.Add(1) == 1 {
			buf := make([]byte, 32<<10)
			n := runtime.Stack(buf, true)
			fmt.Fprintf(os.Stderr, "tview: first input tick stall, goroutine dump:\n%s\n", buf[:n])
		}
		select {
		case <-l.done:
			// The application goroutine ended; more ticks are pointless.
			l.mu.Lock()
			err := l.runErr
			l.mu.Unlock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "tview: application ended early: %v\n", err)
			}
			l.stopped.Store(true)
		default:
		}
	}
}

func (l *tviewLane) counts() (paints int64, draws int64) {
	return l.paints.Load(), l.draws.Load()
}

func (l *tviewLane) stop() error {
	l.stopped.Store(true)
	if l.app != nil {
		// Watchdog: if Stop wedges, dump goroutines to stderr for diagnosis.
		go func() {
			t := time.After(5 * time.Second)
			select {
			case <-t:
				buf := make([]byte, 48<<10)
				n := runtime.Stack(buf, true)
				fmt.Fprintf(os.Stderr, "tview: app.Stop() still blocked after 5s, goroutine dump:\n%s\n", buf[:n])
			}
		}()
		l.app.Stop()
	}
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("tview application did not stop within 5s (stalled input ticks: %d)", l.stalls.Load())
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.runErr != nil {
		return fmt.Errorf("tview run: %w (stalled input ticks: %d)", l.runErr, l.stalls.Load())
	}
	if n := l.stalls.Load(); n > 0 {
		return fmt.Errorf("tview stalled on %d input ticks", n)
	}
	return nil
}

func newBenchPrim(lane *tviewLane, sc sceneID, w, h int) *benchPrim {
	p := &benchPrim{Box: tview.NewBox(), lane: lane, sc: sc, w: w, h: h}
	p.Box.SetBorder(false)
	p.colorCache = make(map[uint32]tcell.Color)
	p.hlStyle = tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(0xff, 0xff, 0xff)).
		Background(tcell.NewRGBColor(0x20, 0x20, 0x60))

	width := maxRowWidth(sc, w)
	if sc == scList {
		p.listRunes = make([][]rune, listRowCount)
		p.listColor = make([]tcell.Color, listRowCount)
		for index := range p.listRunes {
			p.listRunes[index] = []rune(listRow(index, w))
			p.listColor[index] = rgbTcell(rowColor(index))
		}
	} else if isRowScene(sc) {
		p.rowRunes = make([][]rune, h)
		p.rowColor = make([]tcell.Color, h)
		for y := 0; y < h; y++ {
			p.rowRunes[y] = []rune(rowText(y, width))
			p.rowColor[y] = rgbTcell(rowColor(y))
		}
	}
	if sc == scEGC {
		p.egcRows = make([][]egcCell, h)
		p.egcColor = make([]tcell.Color, h)
		for y := 0; y < h; y++ {
			p.egcRows[y] = egcCellsForRow(y, width)
			p.egcColor[y] = rgbTcell(rowColor(y))
		}
	}
	return p
}

func rgbTcell(rgb uint32) tcell.Color {
	return tcell.NewRGBColor(int32(rgb>>16), int32(rgb>>8), int32(rgb))
}

func (p *benchPrim) advance() { p.step++ }

func (p *benchPrim) colorOf(fg uint32) tcell.Color {
	if c, ok := p.colorCache[fg]; ok {
		return c
	}
	c := rgbTcell(fg)
	p.colorCache[fg] = c
	return c
}

// Draw paints the scene with per-cell SetContent — tview's natural API.
func (p *benchPrim) Draw(screen tcell.Screen) {
	p.Box.Draw(screen)
	p.lane.draws.Add(1)
	p.lane.paints.Add(1)

	x0, y0, w, h := p.GetInnerRect()
	switch p.sc {
	case scStatic, scResize:
		p.drawRows(screen, x0, y0, w, h, nil)

	case scOneCell, scRTT:
		p.drawRows(screen, x0, y0, w, h, func(x, y int) (rune, tcell.Color, bool) {
			if x == w/3 && y == h/3 {
				ch, fg, has := cellOverride(p.sc, p.step, x, y, w, h)
				if has {
					return rune(ch[0]), p.colorOf(fg), true
				}
			}
			return 0, 0, false
		})

	case scDamage:
		p.drawRows(screen, x0, y0, w, h, func(x, y int) (rune, tcell.Color, bool) {
			ch, fg, has := cellOverride(scDamage, p.step, x, y, w, h)
			if !has {
				return 0, 0, false
			}
			return rune(ch[0]), p.colorOf(fg), true
		})

	case scInputBurst:
		cx, cy := cursorCell(p.step, w), h/3
		p.drawRows(screen, x0, y0, w, h, func(x, y int) (rune, tcell.Color, bool) {
			if x == cx && y == cy {
				return '*', p.colorOf(0xff3030), true
			}
			return 0, 0, false
		})

	case scFull:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				ch, fg, _ := cellOverride(scFull, p.step, x, y, w, h)
				screen.SetContent(x0+x, y0+y, rune(ch[0]), nil,
					tcell.StyleDefault.Foreground(p.colorOf(fg)))
			}
		}

	case scList:
		offset := listOffset(p.step, h)
		if offset+h > len(p.listRunes) {
			offset = len(p.listRunes) - h
		}
		for y := 0; y < h; y++ {
			runes := p.listRunes[offset+y]
			fg := p.listColor[offset+y]
			for x := 0; x < w && x < len(runes); x++ {
				screen.SetContent(x0+x, y0+y, runes[x], nil,
					tcell.StyleDefault.Foreground(fg))
			}
		}

	case scEGC:
		hl := egcHighlightRow(p.step, h)
		for y := 0; y < h && y < len(p.egcRows); y++ {
			style := tcell.StyleDefault.Foreground(p.egcColor[y])
			if y == hl {
				style = p.hlStyle
			}
			x := 0
			for _, cell := range p.egcRows[y] {
				if x >= w {
					break
				}
				screen.SetContent(x0+x, y0+y, cell.main, cell.combining, style)
				x += cell.w
			}
		}
	}
}

func (p *benchPrim) drawRows(screen tcell.Screen, x0, y0, w, h int,
	override func(x, y int) (rune, tcell.Color, bool)) {
	for y := 0; y < h && y < len(p.rowRunes); y++ {
		runes := p.rowRunes[y]
		fg := p.rowColor[y]
		for x := 0; x < w && x < len(runes); x++ {
			if override != nil {
				if ch, color, ok := override(x, y); ok {
					screen.SetContent(x0+x, y0+y, ch, nil,
						tcell.StyleDefault.Foreground(color))
					continue
				}
			}
			screen.SetContent(x0+x, y0+y, runes[x], nil,
				tcell.StyleDefault.Foreground(fg))
		}
	}
}

func egcCellsForRow(y, w int) []egcCell {
	var cells []egcCell
	it := uniseg.NewGraphemes(egcRow(y, w))
	for it.Next() {
		runes := []rune(it.Str())
		cells = append(cells, egcCell{main: runes[0], combining: runes[1:], w: it.Width()})
	}
	return cells
}
