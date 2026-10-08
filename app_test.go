package lux

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// hangTimeout only bounds a test that would otherwise hang forever. Making it
// generous costs a passing run nothing and stops a loaded CI machine from
// reporting a stall as a logic failure.
const hangTimeout = 10 * time.Second

type fakeBackend struct {
	mu         sync.Mutex
	events     chan event.Event
	presented  chan *render.Buffer
	delay      time.Duration
	opened     bool
	closed     bool
	width      int
	height     int
	presentErr error
}

func newFakeBackend(width, height int) *fakeBackend {
	return &fakeBackend{events: make(chan event.Event, 32), presented: make(chan *render.Buffer, 32), width: width, height: height}
}

func (b *fakeBackend) Open(context.Context) (capability.Set, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opened = true
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}

func (b *fakeBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}

func (b *fakeBackend) Present(ctx context.Context, frame *render.Buffer) error {
	if b.delay > 0 {
		select {
		case <-time.After(b.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.presentErr != nil {
		return b.presentErr
	}
	b.presented <- frame.Clone()
	return nil
}

func (b *fakeBackend) Size() (int, int, error) { return b.width, b.height, nil }

func (b *fakeBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

type testModel struct {
	mu      sync.Mutex
	width   int
	height  int
	frames  int
	updates []event.Kind
	quitOn  event.Kind
	animate int
}

func (m *testModel) Update(next event.Event) Command {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, next.Kind)
	if next.Kind == event.Resize {
		m.width, m.height = next.Width, next.Height
	}
	if next.Kind == event.Frame && m.animate > 0 {
		m.animate--
	}
	if next.Kind == m.quitOn {
		return Quit()
	}
	return nil
}

func (m *testModel) View(frame *Frame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.frames++
	frame.Canvas.Text(0, 0, "frame", render.Style{})
	if m.animate > 0 {
		frame.RequestFrame()
	}
}

// TestDefaultBackendOptions pins the app-side wiring of the backend Run
// constructs for itself: the base set always carries the pixel-mouse and
// hyperlink opt-ins from the settings, and an explicit text width policy
// appends exactly one suppression — the UnicodeCore(false) override — so an
// explicit width claim and mode 2027 never both steer the same stream. The
// identity of the base options belongs to the terminal package's own tests;
// here the observable contract is the shape: two defaults, three with a
// policy, and the third only when the policy is nonzero.
func TestDefaultBackendOptions(t *testing.T) {
	base := defaultBackendOptions(config{})
	if len(base) != 2 {
		t.Fatalf("default options = %d entries, want the pixel-mouse and hyperlink defaults", len(base))
	}
	explicit := defaultBackendOptions(config{widthPolicy: render.WidthPolicy{AmbiguousWidth: 2}})
	if len(explicit) != 3 {
		t.Fatalf("explicit-policy options = %d entries, want the defaults plus the Unicode core suppression", len(explicit))
	}
	// The settings still steer the base pair: a zero policy with the app
	// defaults is the only zero-claim shape, and the pixel/hyperlink
	// toggles are carried by the same pair the policy test counts above.
	settings := defaultConfig()
	settings.widthPolicy = render.WidthPolicy{AmbiguousWidth: 2}
	withPolicy := defaultBackendOptions(settings)
	if len(withPolicy) != len(explicit) {
		t.Fatalf("settings-driven options = %d entries, want %d", len(withPolicy), len(explicit))
	}
	settings.widthPolicy = render.WidthPolicy{}
	if without := defaultBackendOptions(settings); len(without) != len(base) {
		t.Fatalf("zero-policy settings options = %d entries, want %d", len(without), len(base))
	}
}

func TestRunRendersAndRestores(t *testing.T) {
	backend := newFakeBackend(20, 5)
	model := &testModel{quitOn: event.Key}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, WithBackend(backend), WithMaxFPS(240)) }()
	select {
	case frame := <-backend.presented:
		if width, height := frame.Size(); width != 20 || height != 5 {
			t.Fatalf("frame size = %dx%d", width, height)
		}
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.opened || !backend.closed {
		t.Fatalf("lifecycle open=%v close=%v", backend.opened, backend.closed)
	}
}

// keyLogModel records every key event that reaches Update and quits on the
// first escape that is not a release, so a test can assert exactly which
// key actions survived the run loop.
type keyLogModel struct {
	mu   sync.Mutex
	keys []event.KeyEvent
}

func (m *keyLogModel) Update(next event.Event) Command {
	if next.Kind != event.Key {
		return nil
	}
	m.mu.Lock()
	m.keys = append(m.keys, next.Key)
	release := next.Key.Code == event.KeyEscape && next.Key.Action == event.ActionRelease
	m.mu.Unlock()
	if next.Key.Code == event.KeyEscape && !release {
		return Quit()
	}
	return nil
}

func (m *keyLogModel) View(frame *Frame) {
	frame.Canvas.Text(0, 0, "frame", render.Style{})
}

func TestRunFiltersKeyReleasesByDefault(t *testing.T) {
	backend := newFakeBackend(20, 5)
	model := &keyLogModel{}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, WithBackend(backend), WithMaxFPS(240)) }()
	select {
	case <-backend.presented:
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	// The release precedes the press in the FIFO event channel, so by the
	// time the press quit the run, the release was already seen or skipped.
	backend.events <- event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'q', Action: event.ActionRelease}}
	backend.events <- event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'q', Action: event.ActionRepeat, Repeat: 1}}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.keys) != 2 {
		t.Fatalf("model saw %d keys, want the repeat and the press only: %+v", len(model.keys), model.keys)
	}
	if model.keys[0].Action != event.ActionRepeat || model.keys[0].Rune != 'q' {
		t.Fatalf("first delivered key = %+v, want a repeat of q", model.keys[0])
	}
	if model.keys[1].Code != event.KeyEscape || model.keys[1].Action != event.ActionPress {
		t.Fatalf("second delivered key = %+v, want the escape press", model.keys[1])
	}
}

func TestRunWithKeyReleasesDeliversReleases(t *testing.T) {
	backend := newFakeBackend(20, 5)
	model := &keyLogModel{}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), model, WithBackend(backend), WithKeyReleases(true), WithMaxFPS(240))
	}()
	select {
	case <-backend.presented:
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	backend.events <- event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: 'q', Mod: event.ModCtrl, Action: event.ActionRelease}}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.keys) != 2 {
		t.Fatalf("model saw %d keys, want release and press: %+v", len(model.keys), model.keys)
	}
	if model.keys[0].Action != event.ActionRelease || model.keys[0].Mod != event.ModCtrl || model.keys[0].Repeat != 0 {
		t.Fatalf("first delivered key = %+v, want a ctrl+q release with zero repeats", model.keys[0])
	}
	if model.keys[1].Code != event.KeyEscape || model.keys[1].Action != event.ActionPress {
		t.Fatalf("second delivered key = %+v, want the escape press", model.keys[1])
	}
}

func TestRunSkipsStaleAnimationFrames(t *testing.T) {
	backend := newFakeBackend(20, 5)
	backend.delay = 30 * time.Millisecond
	model := &testModel{animate: 20, quitOn: event.Key}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), model, WithBackend(backend), WithMaxFPS(240)) }()
	time.Sleep(100 * time.Millisecond)
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.frames >= 20 {
		t.Fatalf("renderer replayed every stale frame: %d", model.frames)
	}
}

func TestRunClosesAfterPresentError(t *testing.T) {
	backend := newFakeBackend(10, 3)
	backend.presentErr = errors.New("write failed")
	err := Run(context.Background(), &testModel{}, WithBackend(backend))
	if !errors.Is(err, backend.presentErr) {
		t.Fatalf("error = %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.closed {
		t.Fatal("backend not closed")
	}
}

func TestPacerSlowsImmediatelyAndRecoversWithHysteresis(t *testing.T) {
	pacer := newPacer(60)
	now := time.Now()
	if !pacer.observe(now, 15*time.Millisecond) || pacer.fps != 30 {
		t.Fatalf("slow pacer = %d", pacer.fps)
	}
	if pacer.observe(now.Add(time.Second), time.Millisecond) || pacer.fps != 30 {
		t.Fatalf("recovered early = %d", pacer.fps)
	}
	if !pacer.observe(now.Add(4*time.Second), time.Millisecond) || pacer.fps != 60 {
		t.Fatalf("did not recover = %d", pacer.fps)
	}
}

func TestOfferLatestKeepsNewest(t *testing.T) {
	queue := make(chan *surface, 1)
	old := newSurface(1, 1, render.WidthPolicy{})
	latest := newSurface(2, 2, render.WidthPolicy{})
	queue <- old
	if recycled := offerLatest(queue, latest); recycled != old {
		t.Fatal("old surface was not recycled")
	}
	if got := <-queue; got != latest {
		t.Fatal("latest surface was not queued")
	}
}

func TestDroppedSurfaceCallbacksAreReleased(t *testing.T) {
	queue := make(chan *surface, 1)
	stale := newSurface(1, 1, render.WidthPolicy{})
	stale.presented = append(stale.presented, func() {})
	queue <- stale
	latest := newSurface(1, 1, render.WidthPolicy{})
	recycled := offerLatest(queue, latest)
	clear(recycled.presented)
	recycled.presented = recycled.presented[:0]
	if len(recycled.presented) != 0 || cap(recycled.presented) > 0 && recycled.presented[:cap(recycled.presented)][0] != nil {
		t.Fatal("dropped callback remained reachable")
	}
}

func TestPresentationHookRunsOnlyForPresentedFrame(t *testing.T) {
	frame := &Frame{}
	called := 0
	frame.OnPresented(func() { called++ })
	surface := newSurface(1, 1, render.WidthPolicy{})
	surface.presented = append(surface.presented, frame.presented...)
	for _, callback := range surface.presented {
		callback()
	}
	if called != 1 {
		t.Fatalf("hook calls = %d", called)
	}
	dropped := newSurface(1, 1, render.WidthPolicy{})
	dropped.presented = append(dropped.presented, func() { called++ })
	dropped.presented = dropped.presented[:0]
	if called != 1 {
		t.Fatal("dropped frame hook ran")
	}
}

type callbackPanicModel struct{}

func (callbackPanicModel) Update(event.Event) Command { return nil }
func (callbackPanicModel) View(frame *Frame) {
	frame.OnPresented(func() { panic("callback boom") })
}

func TestPresentationCallbackPanicReturnsErrorAndRestores(t *testing.T) {
	backend := newFakeBackend(10, 3)
	err := Run(context.Background(), callbackPanicModel{}, WithBackend(backend))
	if err == nil || !strings.Contains(err.Error(), "presentation callback panic: callback boom") {
		t.Fatalf("error = %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.closed {
		t.Fatal("backend was not restored")
	}
}

type blockingBackend struct {
	*fakeBackend
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newBlockingBackend() *blockingBackend {
	return &blockingBackend{fakeBackend: newFakeBackend(10, 3), started: make(chan struct{}), closed: make(chan struct{})}
}

func (b *blockingBackend) Present(ctx context.Context, _ *render.Buffer) error {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.closed:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingBackend) Close() error {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return b.fakeBackend.Close()
}

func TestRunCancellationDoesNotWaitForBlockedPresent(t *testing.T) {
	backend := newBlockingBackend()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, &testModel{}, WithBackend(backend)) }()
	select {
	case <-backend.started:
	case <-time.After(hangTimeout):
		t.Fatal("present did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("Run remained blocked in Present")
	}
}

func TestRunPlainBackendRendersOnceAndReturns(t *testing.T) {
	backend := newFakeBackend(12, 4)
	model := &testModel{animate: 10}
	done := make(chan error, 1)
	go func() {
		features := capability.Portable()
		settings := defaultConfig()
		settings.backend = backend
		ctx, cancel := context.WithCancelCause(context.Background())
		done <- run(ctx, cancel, model, settings, features, backend.Close)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("plain renderer did not return after its snapshot")
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.frames != 1 {
		t.Fatalf("plain frames = %d, want 1", model.frames)
	}
}

type panicModel struct{}

func (panicModel) Update(event.Event) Command { return nil }
func (panicModel) View(*Frame)                { panic("boom") }

func TestRunRestoresAfterModelPanic(t *testing.T) {
	backend := newFakeBackend(10, 3)
	err := Run(context.Background(), panicModel{}, WithBackend(backend))
	if err == nil || !strings.Contains(err.Error(), "model panic: boom") {
		t.Fatalf("panic error = %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.closed {
		t.Fatal("backend was not closed after model panic")
	}
}

func TestRunJoinsPanicAndFirstCloseError(t *testing.T) {
	restoreErr := errors.New("restore failed")
	backend := &closeErrorBackend{fakeBackend: newFakeBackend(10, 3), err: restoreErr}
	err := Run(context.Background(), panicModel{}, WithBackend(backend))
	if err == nil || !strings.Contains(err.Error(), "model panic: boom") || !errors.Is(err, restoreErr) {
		t.Fatalf("combined error = %v", err)
	}
}

type closeErrorBackend struct {
	*fakeBackend
	err  error
	once sync.Once
}

func (b *closeErrorBackend) Close() error {
	var result error
	b.once.Do(func() {
		_ = b.fakeBackend.Close()
		result = b.err
	})
	return result
}

func TestRunReturnsFirstCloseError(t *testing.T) {
	restoreErr := errors.New("restore failed")
	backend := &closeErrorBackend{fakeBackend: newFakeBackend(10, 3), err: restoreErr}
	model := &testModel{quitOn: event.Resize}
	if err := Run(context.Background(), model, WithBackend(backend)); !errors.Is(err, restoreErr) {
		t.Fatalf("close error = %v", err)
	}
}

func TestWithWidthPolicyValidation(t *testing.T) {
	for _, invalid := range []int{0, 3, -5} {
		backend := newFakeBackend(10, 3)
		err := Run(context.Background(), &testModel{},
			WithBackend(backend),
			WithWidthPolicy(render.WidthPolicy{AmbiguousWidth: invalid}))
		if err == nil || !strings.Contains(err.Error(), "ambiguous width") {
			t.Fatalf("AmbiguousWidth=%d: error = %v; want ambiguous width rejection", invalid, err)
		}
		backend.mu.Lock()
		opened := backend.opened
		backend.mu.Unlock()
		if opened {
			t.Fatalf("AmbiguousWidth=%d: option error returned after backend Open", invalid)
		}
	}
}

// policyModel records the policy-visible measurements of the frame canvas and
// quits on the first key press.
type policyModel struct {
	mu            sync.Mutex
	textWidth     int
	graphemeWidth int
	quitOn        event.Kind
}

func (m *policyModel) Update(next event.Event) Command {
	if next.Kind == m.quitOn {
		return Quit()
	}
	return nil
}

func (m *policyModel) View(frame *Frame) {
	frame.Canvas.Text(0, 0, "±±", render.Style{})
	m.mu.Lock()
	defer m.mu.Unlock()
	m.textWidth = frame.Canvas.TextWidth("±±")
	m.graphemeWidth = frame.Canvas.GraphemeWidth("±")
}

func TestWidthPolicyReachesCanvas(t *testing.T) {
	backend := newFakeBackend(10, 3)
	model := &policyModel{quitOn: event.Key}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), model, WithBackend(backend),
			WithWidthPolicy(render.WidthPolicy{AmbiguousWidth: 2}))
	}()
	select {
	case frame := <-backend.presented:
		if got := frame.Cell(0, 0); got.Grapheme != "±" || got.Width != 2 {
			t.Errorf("frame Cell(0,0) = %#v; want wide ±", got)
		}
		if got := frame.Cell(1, 0); got.Width != 0 {
			t.Errorf("frame Cell(1,0) = %#v; want continuation cell", got)
		}
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.textWidth != 4 || model.graphemeWidth != 2 {
		t.Fatalf("canvas measurements = %d, %d; want 4, 2", model.textWidth, model.graphemeWidth)
	}
}

func TestWidthPolicyDefaultIsLegacy(t *testing.T) {
	// Without WithWidthPolicy the canvas measures the legacy widths.
	backend := newFakeBackend(10, 3)
	model := &policyModel{quitOn: event.Key}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), model, WithBackend(backend))
	}()
	select {
	case frame := <-backend.presented:
		if got := frame.Cell(0, 0); got.Grapheme != "±" || got.Width != 1 {
			t.Errorf("frame Cell(0,0) = %#v; want narrow ±", got)
		}
	case <-time.After(hangTimeout):
		t.Fatal("initial frame timeout")
	}
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.textWidth != 2 || model.graphemeWidth != 1 {
		t.Fatalf("canvas measurements = %d, %d; want 2, 1", model.textWidth, model.graphemeWidth)
	}
}
