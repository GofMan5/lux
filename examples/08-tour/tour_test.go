package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
)

// hangTimeout only bounds a test that would otherwise hang forever; a loaded
// CI machine may be slow, but not ten-seconds slow.
const hangTimeout = 10 * time.Second

// probeBackend is the smallest backend that lets the real Run loop drive the
// tour: buffered events in, every presented buffer out, fixed size.
type probeBackend struct {
	events    chan event.Event
	presented chan *render.Buffer
	width     int
	height    int
	closeOnce sync.Once
}

func newProbeBackend(width, height int) *probeBackend {
	return &probeBackend{
		events:    make(chan event.Event, 32),
		presented: make(chan *render.Buffer, 256),
		width:     width,
		height:    height,
	}
}

func (b *probeBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}

func (b *probeBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}

func (b *probeBackend) Present(ctx context.Context, buffer *render.Buffer) error {
	select {
	case b.presented <- buffer.Clone():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *probeBackend) Size() (int, int, error) { return b.width, b.height, nil }

func (b *probeBackend) Close() error {
	b.closeOnce.Do(func() { close(b.events) })
	return nil
}

// countFrames drains presented buffers for the window and returns how many
// arrived. A settled app presents none; an app that keeps requesting frames
// keeps producing them.
func (b *probeBackend) countFrames(window time.Duration) int {
	count := 0
	deadline := time.After(window)
	for {
		select {
		case <-b.presented:
			count++
		case <-deadline:
			return count
		}
	}
}

// TestStaticTabsSettleIdle pins the idle contract: on a tab whose content
// never moves, the tour must not ask for another frame once the repaint is
// done. Before the frame-request fix every tab repaints forever at the cap.
func TestStaticTabsSettleIdle(t *testing.T) {
	backend := newProbeBackend(100, 30)
	app := newTour()
	app.page = 0
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	// Warm-up: the open sequence repaints a couple of times; then the text
	// tab must go quiet.
	backend.countFrames(150 * time.Millisecond)
	if frames := backend.countFrames(400 * time.Millisecond); frames > 2 {
		t.Fatalf("static text tab kept presenting: %d frames in 400ms of idle", frames)
	}

	// A key press switches to another static tab: one repaint, then quiet.
	backend.events <- event.RuneKey('2', 0)
	backend.countFrames(150 * time.Millisecond)
	if frames := backend.countFrames(400 * time.Millisecond); frames > 2 {
		t.Fatalf("static color tab kept presenting: %d frames in 400ms of idle", frames)
	}

	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("q never quit the tour")
	}
}

// TestRetargetContinuityFromMidFlight pins the retarget contract: both
// motions restart from where the eye currently is. The spring must sample
// its live position (not its old target) as the new From and carry its
// velocity; the tween must restart from its live sample, not a stale cache.
func TestRetargetContinuityFromMidFlight(t *testing.T) {
	app := newTour()
	start := time.Now()
	app.spring = motion.Spring{From: 0, To: 1, Velocity: 0, Start: start, Response: 250 * time.Millisecond}
	app.tween = motion.Tween{From: 0, To: 1, Start: start, Duration: 900 * time.Millisecond, Ease: motion.Linear}

	mid := start.Add(120 * time.Millisecond)
	springPos, springVel := app.spring.Value(mid)
	tweenPos, _ := app.tween.Value(mid)
	if springPos <= 0 || springPos >= 1 {
		t.Fatalf("setup: spring not mid-flight at mid (pos %v)", springPos)
	}
	if springVel <= 0 {
		t.Fatalf("setup: spring not moving at mid (vel %v)", springVel)
	}

	app.retarget(mid)

	if app.spring.From != springPos {
		t.Errorf("spring From = %v, want sampled position %v (retarget must not jump)", app.spring.From, springPos)
	}
	if app.spring.Velocity != springVel {
		t.Errorf("spring Velocity = %v, want carried %v", app.spring.Velocity, springVel)
	}
	if app.spring.To != 0 {
		t.Errorf("spring To = %v, want flipped target 0", app.spring.To)
	}
	if !app.spring.Start.Equal(mid) {
		t.Errorf("spring Start = %v, want %v (continuity at the retarget instant)", app.spring.Start, mid)
	}
	if app.tween.From != tweenPos {
		t.Errorf("tween From = %v, want sampled value %v", app.tween.From, tweenPos)
	}
}

// motionTweenParked returns a tween mid-flight whose duration exceeds the
// test window, so the tick loop never auto-restarts it.
func motionTweenParked(start time.Time) motion.Tween {
	return motion.Tween{From: 0, To: 1, Start: start, Duration: 600 * time.Second, Ease: motion.Linear}
}

// TestMotionDemoKeysSurviveTabFocus pins the page-local key contract: while
// the tab strip owns focus, Space on the motion tab must still retarget the
// demos instead of re-activating the selected tab. Runs the real loop so
// regions commit through the presented frame; assertions read state only
// after Run has exited, so there is no data race.
func TestMotionDemoKeysSurviveTabFocus(t *testing.T) {
	backend := newProbeBackend(100, 30)
	app := newTour()
	app.page = 4
	app.tabs.Selected = 4
	// Park the demos mid-flight: a tween that cannot finish inside the test
	// keeps tick's auto-restart out of the picture, so the only thing that
	// can flip spring.To (1 -> 0) is the Space press itself.
	start := time.Now()
	app.tween = motionTweenParked(start)
	app.spring = motion.Spring{From: 0, To: 1, Velocity: 0, Start: start, Response: 250 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	// Warm-up: the open sequence repaints and commits the tab strip.
	backend.countFrames(150 * time.Millisecond)
	// Tab moves focus to the tab strip — the only focusable region there.
	backend.events <- event.KeyPress(event.KeyTab, 0)
	backend.countFrames(100 * time.Millisecond)
	// The real backend stamps At on every event it decodes
	// (internal/terminal/backend.go); do the same so the retarget's motion
	// samples see the press's real instant, not the zero time.
	space := event.RuneKey(' ', 0)
	space.At = time.Now()
	backend.events <- space
	backend.countFrames(100 * time.Millisecond)

	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("q never quit the tour")
	}

	if app.page != 4 {
		t.Fatalf("page = %d, want 4: space re-activated the tab strip", app.page)
	}
	if app.spring.To != 0 {
		t.Fatalf("spring.To = %v, want 0: space re-activated the tab strip instead of retargeting the motion demo", app.spring.To)
	}
}

// TestWantsFrameMatrix pins the idle contract for every tab: static tabs
// never ask for frames, animated tabs do, ReducedMotion freezes decoration
// but never the determinate progress sideband, and the widget tab asks
// only for the exact glyph flip of its spinner.
func TestWantsFrameMatrix(t *testing.T) {
	now := time.Now()

	static := map[int]bool{0: true, 1: true, 2: true, 3: true, 7: true, 8: true}
	for page := range static {
		app := newTour()
		app.page = page
		if _, moving := app.wantsFrame(now); moving {
			t.Fatalf("page %d: static tab wants frames", page)
		}
	}

	app := newTour()
	app.page = 4
	if _, moving := app.wantsFrame(now); !moving {
		t.Fatal("motion page: want continuous frames")
	}
	app.reduced = true
	if _, moving := app.wantsFrame(now); moving {
		t.Fatal("motion page under reduced motion: want no frames")
	}

	app = newTour()
	app.page = 5
	if _, moving := app.wantsFrame(now); !moving {
		t.Fatal("scene page: want continuous frames")
	}
	app.paused = true
	if _, moving := app.wantsFrame(now); moving {
		t.Fatal("paused scene: want no frames")
	}
	app.paused, app.reduced = false, true
	if _, moving := app.wantsFrame(now); moving {
		t.Fatal("scene page under reduced motion: want no frames")
	}

	app = newTour()
	app.page = 6
	app.sync.Started = time.Time{} // no running spinner, no moving gauge
	if _, moving := app.wantsFrame(now); moving {
		t.Fatal("widget tab with a settled gauge and a stopped spinner: want no frames")
	}
	app.target, app.gauge = .5, .1
	if _, moving := app.wantsFrame(now); !moving {
		t.Fatal("widget tab with a moving gauge: want frames")
	}
	app.target, app.gauge = .5, .5
	app.sync.Started = now.Add(-205 * time.Millisecond)
	delay, moving := app.wantsFrame(now)
	if !moving {
		t.Fatal("widget tab with a running spinner: want a frame request")
	}
	// 205ms of spinner age under an 80ms cadence: the next glyph flip is
	// due in 80 - 205%80 = 35ms exactly, not "as soon as possible".
	if want := 35 * time.Millisecond; delay != want {
		t.Fatalf("spinner delay = %v, want %v", delay, want)
	}
	app.slow.Checked = true
	app.sync.Started = now.Add(-205 * time.Millisecond)
	delay, moving = app.wantsFrame(now)
	// slow duration: 240ms; 205ms of age → next flip due in 240-205 = 35ms.
	duration := 240 * time.Millisecond
	age := 205 * time.Millisecond
	wantSlow := duration - age%duration
	if !moving || delay != wantSlow {
		t.Fatalf("slow spinner delay = %v (moving=%v), want %v", delay, moving, wantSlow)
	}
	app.reduced = true
	if _, moving := app.wantsFrame(now); moving {
		t.Fatal("widget tab under reduced motion: want no frames")
	}

	// The determinate progress sideband is work state, not decoration: it
	// drives frames from any tab, reduced motion included.
	for _, page := range []int{0, 4, 5, 6} {
		app := newTour()
		app.page = page
		app.progressMode = 1
		app.reduced = true
		if _, moving := app.wantsFrame(now); !moving {
			t.Fatalf("page %d with determinate progress: want frames", page)
		}
	}
}

// TestReducedMotionFreezesDecorationButNotProgress runs the real loop with
// the reduced-motion setting on: the scene must stop rotating (angle stays
// at its starting value, the gauge snaps to target) while the determinate
// progress sideband keeps ticking, because work state is not decoration.
func TestReducedMotionFreezesDecorationButNotProgress(t *testing.T) {
	backend := newProbeBackend(100, 30)
	app := newTour()
	app.page = 5
	app.tabs.Selected = 5
	app.progressMode, app.progress = 1, 0
	done := make(chan error, 1)
	go func() {
		done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240), lux.WithReducedMotion(true))
	}()

	// Frames flow only because the progress sideband keeps asking; without
	// it a reduced-motion app would present exactly one idle frame.
	frames := backend.countFrames(400 * time.Millisecond)
	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("q never quit the tour")
	}

	if !app.reduced {
		t.Fatal("View never latched frame.ReducedMotion into app.reduced")
	}
	if app.angle != 0 {
		t.Fatalf("angle = %v under reduced motion, want 0: decoration must freeze", app.angle)
	}
	if app.gauge != app.target {
		t.Fatalf("gauge = %v, want it snapped to target %v under reduced motion", app.gauge, app.target)
	}
	if app.progress <= 0 || app.progress >= 1 {
		t.Fatalf("progress = %v, want strictly between 0 and 1: the sideband must keep ticking", app.progress)
	}
	if frames < 3 {
		t.Fatalf("presented %d frames in 400ms, want the progress sideband to keep asking", frames)
	}
}

// TestPastePreviewTruncation pins the feed's paste preview: short pastes
// arrive verbatim, long ones show their first runes plus the total count.
func TestPastePreviewTruncation(t *testing.T) {
	short := "hello"
	if got := pastePreview(short); got != short {
		t.Fatalf("short paste preview = %q, want %q", got, short)
	}
	exact := "012345678901234567890123456789012345678901234567" // 48 runes
	if got := pastePreview(exact); got != exact {
		t.Fatalf("48-rune paste preview = %q, want it verbatim", got)
	}
	long := []rune{}
	for i := 0; i < 200; i++ {
		long = append(long, 'x')
	}
	got := pastePreview(string(long))
	want := fmt.Sprintf("%s… (%d runes total)", string(long[:48]), len(long))
	if got != want {
		t.Fatalf("long paste preview = %q, want %q", got, want)
	}
}

// lastFrame drains presented buffers for the window and returns the newest
// one, so an assertion can judge exactly what the user last saw.
func (b *probeBackend) lastFrame(window time.Duration) *render.Buffer {
	var last *render.Buffer
	deadline := time.After(window)
	for {
		select {
		case frame := <-b.presented:
			last = frame
		case <-deadline:
			return last
		}
	}
}

// bufferRow flattens one buffer row into trimmed text.
func bufferRow(buffer *render.Buffer, y int) string {
	if buffer == nil {
		return ""
	}
	width, height := buffer.Size()
	if y < 0 || y >= height {
		return ""
	}
	runes := make([]rune, 0, width)
	for x := 0; x < width; x++ {
		runes = append(runes, []rune(buffer.Cell(x, y).Grapheme)...)
	}
	return strings.TrimSpace(string(runes))
}

// bufferContains reports whether any row carries the needle.
func bufferContains(buffer *render.Buffer, needle string) bool {
	if buffer == nil {
		return false
	}
	_, height := buffer.Size()
	for y := 0; y < height; y++ {
		if strings.Contains(bufferRow(buffer, y), needle) {
			return true
		}
	}
	return false
}

func bufferContainsRow(buffer *render.Buffer, y int, needle string) bool {
	return strings.Contains(bufferRow(buffer, y), needle)
}

// bufferFindRow returns the row carrying the needle, or -1.
func bufferFindRow(buffer *render.Buffer, needle string) int {
	if buffer == nil {
		return -1
	}
	_, height := buffer.Size()
	for y := 0; y < height; y++ {
		if strings.Contains(bufferRow(buffer, y), needle) {
			return y
		}
	}
	return -1
}

// TestCompactWidgetsPageKeepsControlsReachable pins the compact widgets page
// (80x24): the two-column layout does not fit at that height, so the page
// must stack its panels at full width inside one page viewport. Every
// control stays reachable: the initial view already shows the form controls
// the squeezed layout dropped, the wheel scrolls the page down to the table
// and the virtual log, the arrow keys scroll back up once the viewport owns
// focus, and the footer row survives the whole trip.
func TestCompactWidgetsPageKeepsControlsReachable(t *testing.T) {
	backend := newProbeBackend(80, 24)
	app := newTour()
	app.page = 6
	app.tabs.Selected = 6
	app.sync.Started = time.Time{} // settled spinner: no continuous repaints
	done := make(chan error, 1)
	go func() {
		done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240))
	}()

	// Warm-up: the open sequence repaints a few times, then the settled page
	// goes quiet; the newest buffer is the first thing the user sees.
	start := backend.lastFrame(150 * time.Millisecond)
	if start == nil {
		t.Fatal("no frame presented during warm-up")
	}
	if !bufferContains(start, "slow spinner") {
		t.Fatal("compact view lost the checkbox: the stacked form panel must keep every control on screen")
	}

	// Wheel over the viewport's left border column: no widget owns column 1,
	// so every notch goes to the page scroller instead of an inner control.
	for i := 0; i < 12; i++ {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			X: 1, Y: 3, Action: event.MouseWheel, Button: event.MouseWheelDown,
		}}
	}
	bottom := backend.lastFrame(200 * time.Millisecond)
	if bottom == nil {
		t.Fatal("no frame presented after the wheel")
	}
	if !bufferContains(bottom, "feature table") {
		t.Fatal("wheel never reached the table: the page scroller must reach the stacked page bottom")
	}
	if !bufferContains(bottom, "virtual log (120 rows)") {
		t.Fatal("wheel never reached the virtual log demo")
	}
	if !bufferContainsRow(bottom, 23, "[1-9] tabs") {
		t.Fatal("footer row was painted over by the scrolling page")
	}

	// Keyboard: Tab lands focus on the page viewport (right after the tab
	// strip, before the widgets), and PageUp walks the offset back to the top.
	backend.events <- event.KeyPress(event.KeyTab, 0)
	backend.countFrames(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		backend.events <- event.KeyPress(event.KeyPageUp, 0)
	}
	top := backend.lastFrame(200 * time.Millisecond)
	if top == nil {
		t.Fatal("no frame presented after PageUp")
	}
	if !bufferContains(top, "slow spinner") {
		t.Fatal("keyboard scrolling could not bring the stacked page back to the top")
	}

	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("q never quit the tour")
	}
}

// TestWidgetsPageSortsFeatureTable pins the whole sort path end to end on the
// compact widgets page (80x24): twelve wheels reveal the stacked feature
// table, a click on the CAPABILITY header must reach the table (header and
// last row presses used to be dropped by the router below the content), and
// the tour must serve the request by re-ordering its own rows — ascending
// first, then descending on the second click.
func TestWidgetsPageSortsFeatureTable(t *testing.T) {
	backend := newProbeBackend(80, 24)
	app := newTour()
	app.page = 6
	app.tabs.Selected = 6
	app.sync.Started = time.Time{}
	done := make(chan error, 1)
	go func() {
		done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240))
	}()

	if backend.lastFrame(150 * time.Millisecond) == nil {
		t.Fatal("no frame presented during warm-up")
	}
	for i := 0; i < 12; i++ {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			X: 1, Y: 3, Action: event.MouseWheel, Button: event.MouseWheelDown,
		}}
	}
	scrolled := backend.lastFrame(200 * time.Millisecond)
	if scrolled == nil {
		t.Fatal("no frame presented after the wheel")
	}
	header := bufferFindRow(scrolled, "CAPABILITY")
	if header < 0 {
		t.Fatal("feature table header is not on screen after scrolling")
	}
	clickHeader := func() *render.Buffer {
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			X: 4, Y: header, Action: event.MousePress, Button: event.MouseLeft,
		}}
		backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
			X: 4, Y: header, Action: event.MouseRelease, Button: event.MouseLeft,
		}}
		return backend.lastFrame(200 * time.Millisecond)
	}

	ascending := clickHeader()
	if ascending == nil {
		t.Fatal("no frame presented after the first header click")
	}
	if !bufferContainsRow(ascending, header+1, "allocation steady state") {
		t.Fatalf("ascending sort: first row = %q", bufferRow(ascending, header+1))
	}
	if !bufferContainsRow(ascending, header, "▲") {
		t.Fatalf("ascending sort: header = %q", bufferRow(ascending, header))
	}

	descending := clickHeader()
	if descending == nil {
		t.Fatal("no frame presented after the second header click")
	}
	if !bufferContainsRow(descending, header+1, "word wrap authority") {
		t.Fatalf("descending sort: first row = %q", bufferRow(descending, header+1))
	}
	if !bufferContainsRow(descending, header, "▼") {
		t.Fatalf("descending sort: header = %q", bufferRow(descending, header))
	}

	backend.events <- event.RuneKey('q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("q never quit the tour")
	}
}
