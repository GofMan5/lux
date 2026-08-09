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
	case <-time.After(time.Second):
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
	old := newSurface(1, 1)
	latest := newSurface(2, 2)
	queue <- old
	if recycled := offerLatest(queue, latest); recycled != old {
		t.Fatal("old surface was not recycled")
	}
	if got := <-queue; got != latest {
		t.Fatal("latest surface was not queued")
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
	case <-time.After(time.Second):
		t.Fatal("present did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
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
		done <- run(ctx, cancel, model, settings, features)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
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
