// Package lux is a responsive, animated, terminal-independent TUI runtime.
package lux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/internal/terminal"
	"github.com/lux-tui/lux/render"
)

// Model is updated and rendered serially on the runtime goroutine. View draws
// an immediate frame; persistent state belongs in the model.
type Model interface {
	Update(event.Event) Command
	View(*Frame)
}

// Command performs optional asynchronous work and returns one event. Commands
// receive the application context and should honor its cancellation.
type Command func(context.Context) event.Event

// Emit turns a value into an immediate command.
func Emit(next event.Event) Command { return func(context.Context) event.Event { return next } }

// Quit asks the runtime to restore the terminal and return.
func Quit() Command { return Emit(event.Event{Kind: event.Quit}) }

// Metrics describes the current presentation path without affecting state.
type Metrics struct {
	TargetFPS     int
	PresentTime   time.Duration
	DroppedFrames uint64
}

// Frame is valid only for the duration of Model.View.
type Frame struct {
	Canvas        *render.Canvas
	Now           time.Time
	Delta         time.Duration
	Capabilities  capability.Set
	Metrics       Metrics
	ReducedMotion bool

	nextFrame time.Time
}

// Size returns the drawable viewport in cells.
func (f *Frame) Size() (int, int) {
	if f == nil || f.Canvas == nil {
		return 0, 0
	}
	return f.Canvas.Size()
}

// RequestFrame schedules another animation frame at the runtime's current
// adaptive rate. Multiple requests coalesce.
func (f *Frame) RequestFrame() { f.RequestFrameAt(f.Now) }

// RequestFrameAfter schedules another animation frame after delay.
func (f *Frame) RequestFrameAfter(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	f.RequestFrameAt(f.Now.Add(delay))
}

// RequestFrameAt keeps the earliest requested deadline.
func (f *Frame) RequestFrameAt(deadline time.Time) {
	if f == nil {
		return
	}
	if f.nextFrame.IsZero() || deadline.Before(f.nextFrame) {
		f.nextFrame = deadline
	}
}

// Option configures Run.
type Option func(*config) error

// WithBackend supplies a custom terminal, PTY, SSH, test, or recording adapter.
func WithBackend(backend Backend) Option {
	return func(config *config) error {
		if backend == nil {
			return errors.New("lux: nil backend")
		}
		config.backend = backend
		return nil
	}
}

// WithIO changes the files used by the default terminal adapter.
func WithIO(input, output *os.File) Option {
	return func(config *config) error { config.input, config.output = input, output; return nil }
}

// WithMaxFPS caps animation and event-driven paints. The adaptive scheduler
// may lower it when terminal presentation consumes most of a frame budget.
func WithMaxFPS(fps int) Option {
	return func(config *config) error {
		if fps < 1 || fps > 240 {
			return fmt.Errorf("lux: max FPS must be between 1 and 240, got %d", fps)
		}
		config.maxFPS = fps
		return nil
	}
}

// WithSizeLimit bounds memory use when an untrusted PTY reports absurd sizes.
func WithSizeLimit(width, height int) Option {
	return func(config *config) error {
		if width < 1 || height < 1 {
			return errors.New("lux: size limits must be positive")
		}
		config.maxWidth, config.maxHeight = width, height
		return nil
	}
}

// WithFallbackSize sets the viewport used by redirected/plain backends.
func WithFallbackSize(width, height int) Option {
	return func(config *config) error {
		if width < 1 || height < 1 {
			return errors.New("lux: fallback size must be positive")
		}
		config.fallbackWidth, config.fallbackHeight = width, height
		return nil
	}
}

// WithReducedMotion lets applications replace movement with static or short
// transitions. Terminals have no portable OS preference query.
func WithReducedMotion(reduced bool) Option {
	return func(config *config) error { config.reducedMotion = reduced; return nil }
}

type config struct {
	backend                       Backend
	input, output                 *os.File
	maxFPS, maxWidth, maxHeight   int
	fallbackWidth, fallbackHeight int
	reducedMotion                 bool
}

func defaultConfig() config {
	return config{input: os.Stdin, output: os.Stdout, maxFPS: 60, maxWidth: 500, maxHeight: 200, fallbackWidth: 80, fallbackHeight: 24}
}

// Run owns terminal lifecycle until the model quits, the context is canceled,
// or an error occurs. Terminal restoration is attempted on every return path.
func Run(ctx context.Context, model Model, options ...Option) (result error) {
	if model == nil {
		return errors.New("lux: nil model")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	settings := defaultConfig()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&settings); err != nil {
			return err
		}
	}
	if settings.backend == nil {
		settings.backend = terminal.New(settings.input, settings.output)
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	features, err := settings.backend.Open(runCtx)
	if err != nil {
		return err
	}
	closed := false
	var closeErr error
	closeBackend := func() error {
		if closed {
			return closeErr
		}
		closed = true
		closeErr = settings.backend.Close()
		return closeErr
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = fmt.Errorf("lux: model panic: %v\n%s", recovered, debug.Stack())
		}
		result = errors.Join(result, closeBackend())
	}()
	return run(runCtx, cancel, model, settings, features, closeBackend)
}

type surface struct {
	buffer *render.Buffer
	canvas *render.Canvas
}

func newSurface(width, height int) *surface {
	buffer := render.NewBuffer(width, height)
	return &surface{buffer: buffer, canvas: render.NewCanvas(buffer)}
}

func (s *surface) resize(width, height int) {
	if oldWidth, oldHeight := s.buffer.Size(); oldWidth == width && oldHeight == height {
		return
	}
	s.buffer.Resize(width, height)
	s.canvas = render.NewCanvas(s.buffer)
}

type presentResult struct {
	surface  *surface
	duration time.Duration
	err      error
}

func run(ctx context.Context, cancel context.CancelCauseFunc, model Model, settings config, features capability.Set, closeBackend func() error) (result error) {
	defer cancel(nil)

	width, height, err := settings.backend.Size()
	if err != nil {
		width, height = settings.fallbackWidth, settings.fallbackHeight
	}
	width, height = clampSize(width, height, settings)

	present := make(chan *surface, 1)
	presented := make(chan presentResult, 3)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			var next *surface
			select {
			case <-ctx.Done():
				return
			case next = <-present:
			}
			started := time.Now()
			err := settings.backend.Present(ctx, next.buffer)
			select {
			case presented <- presentResult{surface: next, duration: time.Since(started), err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		cancel(nil)
		result = errors.Join(result, closeBackend())
		<-writerDone
	}()

	events := make(chan event.Event, 256)
	go readEvents(ctx, settings.backend, events)
	commands := make(chan event.Event, 64)

	// One surface is composed, one may wait in the latest-frame mailbox, and
	// one may be in flight. No additional frame memory is created under lag.
	surfaces := []*surface{newSurface(width, height), newSurface(width, height), newSurface(width, height)}
	current, free := surfaces[0], surfaces[1:]
	pacer := newPacer(settings.maxFPS)
	metrics := Metrics{TargetFPS: pacer.fps}

	startCommand := func(command Command) {
		if command == nil {
			return
		}
		executeCommand(ctx, command, commands)
	}

	startCommand(model.Update(event.ResizeTo(width, height)))
	dirty := true
	var requested, lastFrame time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()
	timerChannel := timer.C
	dropped := uint64(0)

	arm := func(now time.Time) {
		var due time.Time
		if dirty {
			due = now
		} else if !requested.IsZero() {
			due = requested
		} else {
			stopTimer(timer)
			timerChannel = nil
			return
		}
		minimum := lastFrame.Add(pacer.period())
		if due.Before(minimum) {
			due = minimum
		}
		stopTimer(timer)
		timer.Reset(max(due.Sub(now), 0))
		timerChannel = timer.C
	}

	for {
		arm(time.Now())
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case reported := <-presented:
			free = append(free, reported.surface)
			metrics.PresentTime = reported.duration
			if reported.err != nil {
				cancel(reported.err)
				return reported.err
			}
			if pacer.observe(time.Now(), reported.duration) {
				metrics.TargetFPS = pacer.fps
			}
		case next := <-events:
			if next.Kind == event.Error {
				return next.Err
			}
			if done, err := applyEvent(model, next, settings, &width, &height, &dirty, startCommand); done || err != nil {
				return err
			}
		case next := <-commands:
			if next.Kind == event.Error {
				return next.Err
			}
			if done, err := applyEvent(model, next, settings, &width, &height, &dirty, startCommand); done || err != nil {
				return err
			}
		case now := <-timerChannel:
			timerChannel = nil
			if !dirty {
				delta := time.Duration(0)
				if !lastFrame.IsZero() {
					delta = now.Sub(lastFrame)
				}
				startCommand(model.Update(event.Event{Kind: event.Frame, At: now, Delta: delta}))
				dirty = true
			}
			requested = time.Time{}
			if width == 0 || height == 0 {
				dirty = false
				continue
			}
			current.resize(width, height)
			current.buffer.Reset()
			delta := time.Duration(0)
			if !lastFrame.IsZero() {
				delta = now.Sub(lastFrame)
			}
			frame := Frame{Canvas: current.canvas, Now: now, Delta: delta, Capabilities: features, Metrics: metrics, ReducedMotion: settings.reducedMotion}
			model.View(&frame)
			requested, lastFrame, dirty = frame.nextFrame, now, false
			if !features.Interactive {
				requested = time.Time{}
			}
			metrics.DroppedFrames = dropped

			recycled := offerLatest(present, current)
			if !features.Interactive {
				select {
				case reported := <-presented:
					if reported.err != nil {
						cancel(reported.err)
					}
					return reported.err
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			}
			if recycled != nil {
				dropped++
				current = recycled
				continue
			}
			if len(free) == 0 {
				// Three surfaces guarantee one drawing, one queued, one in flight.
				// A completed surface may be waiting even if select chose the timer.
				select {
				case reported := <-presented:
					if reported.err != nil {
						cancel(reported.err)
						return reported.err
					}
					metrics.PresentTime = reported.duration
					pacer.observe(time.Now(), reported.duration)
					current = reported.surface
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			} else {
				current, free = free[len(free)-1], free[:len(free)-1]
			}
		}
	}
}

func applyEvent(model Model, next event.Event, settings config, width, height *int, dirty *bool, startCommand func(Command)) (bool, error) {
	if next.Kind == event.Quit {
		return true, nil
	}
	if next.Kind == event.Resize {
		next.Width, next.Height = clampSize(next.Width, next.Height, settings)
		if next.Width == *width && next.Height == *height {
			return false, nil
		}
		*width, *height = next.Width, next.Height
	}
	startCommand(model.Update(next))
	*dirty = true
	return false, nil
}

func clampSize(width, height int, settings config) (int, int) {
	return min(max(width, 0), settings.maxWidth), min(max(height, 0), settings.maxHeight)
}

func executeCommand(ctx context.Context, command Command, results chan<- event.Event) {
	go runCommand(ctx, command, results)
}

func runCommand(ctx context.Context, command Command, results chan<- event.Event) {
	defer func() {
		if recovered := recover(); recovered != nil {
			select {
			case results <- event.Event{Kind: event.Error, Err: fmt.Errorf("lux: command panic: %v\n%s", recovered, debug.Stack())}:
			case <-ctx.Done():
			}
		}
	}()
	next := command(ctx)
	if next.Kind == event.None {
		return
	}
	select {
	case results <- next:
	case <-ctx.Done():
	}
}

func readEvents(ctx context.Context, backend Backend, events chan<- event.Event) {
	for {
		next, err := backend.NextEvent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			next = event.Event{Kind: event.Error, Err: err}
		}
		select {
		case events <- next:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func offerLatest(queue chan *surface, next *surface) *surface {
	for {
		select {
		case queue <- next:
			return nil
		default:
		}
		select {
		case stale := <-queue:
			queue <- next
			return stale
		default:
			// The writer consumed between selects; retry the non-blocking send.
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

type pacer struct {
	maximum, fps int
	fastSince    time.Time
}

func newPacer(maximum int) pacer { return pacer{maximum: maximum, fps: maximum} }

func (p pacer) period() time.Duration { return time.Second / time.Duration(max(p.fps, 1)) }

func (p *pacer) observe(now time.Time, present time.Duration) bool {
	if present*10 > p.period()*7 && p.fps > 10 {
		previous := p.fps
		switch {
		case p.fps > 30:
			p.fps = 30
		case p.fps > 20:
			p.fps = 20
		default:
			p.fps = 10
		}
		p.fps = min(p.fps, p.maximum)
		p.fastSince = time.Time{}
		return p.fps != previous
	}
	if present*10 < p.period()*4 && p.fps < p.maximum {
		if p.fastSince.IsZero() {
			p.fastSince = now
		} else if now.Sub(p.fastSince) >= 2*time.Second {
			previous := p.fps
			switch {
			case p.fps < 20:
				p.fps = min(20, p.maximum)
			case p.fps < 30:
				p.fps = min(30, p.maximum)
			default:
				p.fps = p.maximum
			}
			p.fastSince = now
			return p.fps != previous
		}
	} else {
		p.fastSince = time.Time{}
	}
	return false
}
