// Package lux is a responsive, animated, terminal-independent TUI runtime.
package lux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"
	"unicode"

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
	presented []func()

	// annex carries the control sideband bytes View queued through
	// SetProgress, Notify, and CopyToClipboard, and the three flags gate
	// those methods against the WithProgress, WithNotifications, and
	// WithClipboard choices. The zero Frame every test constructs has
	// them false, so the methods queue nothing — a build that never opts
	// in is byte-identical to a build without the features.
	annex         []byte
	progress      bool
	notifications bool
	clipboard     bool
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

// OnPresented runs callback after this exact frame is successfully written.
// Dropped and failed frames never run it. Callbacks must be brief and safe to
// execute on the presentation goroutine.
func (f *Frame) OnPresented(callback func()) {
	if f == nil || callback == nil {
		return
	}
	f.presented = append(f.presented, callback)
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

// WithKeyReleases controls whether key release events reach the model.
// Terminals that support the kitty keyboard protocol report releases for
// every non-text key; by default the run loop filters them so every model
// sees the same press and auto-repeat stream a legacy terminal produces.
// Auto-repeat events pass through regardless — key repeat is input, not
// release noise.
func WithKeyReleases(enabled bool) Option {
	return func(config *config) error { config.keyReleases = enabled; return nil }
}

// WithPixelMouse opts into pixel-precision mouse coordinates. Terminals that
// support kitty mode 1016 then report pointer position in pixels, pointer
// leaves as dedicated events, and the extra back/forward mouse buttons; on
// every other terminal the option is inert and coordinates stay cell-based.
// The default is off because pixel reports change the shape of the mouse
// event stream models must handle: PixelX/PixelY gain real values, X/Y
// derive from the pixel position, and a leave arrives as a motion event
// with no button and off-screen coordinates (-1).
func WithPixelMouse(enabled bool) Option {
	return func(config *config) error { config.pixelMouse = enabled; return nil }
}

// WithHyperlinks opts into OSC 8 hyperlinks. Links attached to cells through
// Canvas.PutCell then reach terminals that support the sequences; on every
// other terminal the text renders unchanged, and models that never attach a
// link see no difference at all. The default is off because some terminals
// still print or ignore the escape sequences rather than treat them as text.
func WithHyperlinks(enabled bool) Option {
	return func(config *config) error { config.hyperlinks = enabled; return nil }
}

// WithTitle sets the terminal window and tab title while the program runs
// and restores the previous title on exit. The title is written once, in
// the same terminal write as the enter sequences — not per frame — using
// OSC 0 bracketed by the XTWINOPS title stack (CSI 22;0 t before, CSI 23;0
// after), so terminals with the stack get their title back exactly and
// terminals without it keep the program's title, the documented trade.
// Control runes in the title are stripped before it is written: a BEL or
// ESC would end the OSC string early and leak the rest as commands. An
// empty title opts out. The option applies to the terminal backend Run
// constructs for itself; a custom backend owns its own title policy.
func WithTitle(title string) Option {
	return func(config *config) error { config.title = title; return nil }
}

// WithWidthPolicy sets how the terminal measures ambiguous graphemes: East
// Asian Ambiguous characters one or two cells wide, emoji presentation
// selectors honored or ignored, emoji one or two cells wide. The default
// policy (a modern, Western-locale terminal) changes nothing. The policy
// rides the frame canvas, so every canvas operation — drawing and measuring
// alike — applies it uniformly; free render.TextWidth and render.GraphemeWidth
// stay on the default policy. AmbiguousWidth must be 1 or 2.
//
// An explicit policy suppresses Unicode core mode (DEC private mode 2027):
// the application has claimed the width contract for this stream, so the
// backend no longer asks the terminal to cluster on its behalf — an explicit
// claim and mode 2027 would both steer the same measurement, and the explicit
// claim wins.
func WithWidthPolicy(policy render.WidthPolicy) Option {
	return func(config *config) error {
		if policy.AmbiguousWidth != 1 && policy.AmbiguousWidth != 2 {
			return fmt.Errorf("lux: ambiguous width must be 1 or 2, got %d", policy.AmbiguousWidth)
		}
		config.widthPolicy = policy
		return nil
	}
}

// WithSuspend controls whether the runtime claims Ctrl+Z for job control.
// The default is on: in a terminal, Ctrl+Z is the suspend reflex every
// classic program honors, so the runtime runs the full handshake — the
// model sees Suspend, the terminal is handed back to the shell, the process
// group stops, and on the shell's fg the model sees Resume and the screen
// repaints — without the application writing any signal code. Passing false
// lets Ctrl+Z reach the model as an ordinary key event instead, for
// applications that want the key for themselves; on platforms without job
// control (the Windows console) the option is inert either way and the key
// always reaches the model.
func WithSuspend(enabled bool) Option {
	return func(config *config) error { config.suspend = enabled; return nil }
}

type config struct {
	backend                       Backend
	input, output                 *os.File
	maxFPS, maxWidth, maxHeight   int
	fallbackWidth, fallbackHeight int
	reducedMotion                 bool
	keyReleases                   bool
	pixelMouse                    bool
	hyperlinks                    bool
	suspend                       bool
	title                         string
	widthPolicy                   render.WidthPolicy
	progress                      bool
	notifications                 bool
	clipboard                     bool
}

func defaultConfig() config {
	return config{input: os.Stdin, output: os.Stdout, maxFPS: 60, maxWidth: 500, maxHeight: 200, fallbackWidth: 80, fallbackHeight: 24, suspend: true}
}

// defaultBackendOptions turns app settings into backend options for the
// backend Run constructs when the application did not provide its own. The
// width policy is the one claim that forbids mode 2027: an explicit policy
// means the application owns the width contract for this stream, so the
// backend opts out of the terminal's Unicode core mode even on terminals
// that answered the probe. The title always rides along — an empty string
// is the backend's no-op, so plain streams and default builds stay
// byte-identical. Everything else follows the settings directly.
func defaultBackendOptions(settings config) []terminal.Option {
	options := []terminal.Option{terminal.PixelMouse(settings.pixelMouse), terminal.Hyperlinks(settings.hyperlinks), terminal.Title(settings.title), terminal.Progress(settings.progress)}
	if settings.widthPolicy != (render.WidthPolicy{}) {
		options = append(options, terminal.UnicodeCore(false))
	}
	return options
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
		settings.backend = terminal.New(settings.input, settings.output, defaultBackendOptions(settings)...)
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
	buffer      *render.Buffer
	canvas      *render.Canvas
	widthPolicy render.WidthPolicy
	presented   []func()
	// annex holds the control sideband bytes queued by the View that drew
	// this surface. It rides the surface so an annex belongs to exactly
	// one frame — a dropped frame drops its annex, and a replaced frame
	// in the latest-frame mailbox is the one whose annex survives.
	annex []byte
}

func newSurface(width, height int, policy render.WidthPolicy) *surface {
	buffer := render.NewBuffer(width, height)
	return &surface{buffer: buffer, canvas: render.NewCanvasWithPolicy(buffer, policy), widthPolicy: policy}
}

func (s *surface) resize(width, height int) {
	if oldWidth, oldHeight := s.buffer.Size(); oldWidth == width && oldHeight == height {
		return
	}
	s.buffer.Resize(width, height)
	s.canvas = render.NewCanvasWithPolicy(s.buffer, s.widthPolicy)
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
		defer func() {
			if recovered := recover(); recovered != nil {
				err := fmt.Errorf("lux: presentation callback panic: %v\n%s", recovered, debug.Stack())
				if current := context.Cause(ctx); current != nil {
					err = errors.Join(current, err)
				}
				cancel(err)
				select {
				case presented <- presentResult{err: err}:
				default:
				}
			}
			close(writerDone)
		}()
		for {
			var next *surface
			select {
			case <-ctx.Done():
				return
			case next = <-present:
			}
			started := time.Now()
			err := settings.backend.Present(ctx, next.buffer)
			if err == nil && len(next.annex) > 0 {
				// The annex writes only after its frame is on the wire:
				// the sideband belongs to the frame that queued it, so a
				// failed or cancelled presentation drops it, and the
				// write shares the presentation goroutine — serializing
				// with frames on the output instead of racing them. A
				// backend without the AnnexWriter capability ignores the
				// request, the same way a terminal without the protocol
				// ignores the bytes.
				if writer, ok := settings.backend.(AnnexWriter); ok {
					err = writer.WriteAnnex(ctx, next.annex)
				}
			}
			if err == nil {
				for _, callback := range next.presented {
					callback()
				}
			}
			clear(next.presented)
			next.presented = next.presented[:0]
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
	surfaces := []*surface{newSurface(width, height, settings.widthPolicy), newSurface(width, height, settings.widthPolicy), newSurface(width, height, settings.widthPolicy)}
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
			if !settings.keyReleases && next.Kind == event.Key && next.Key.Action == event.ActionRelease {
				// A release carries no activation: models written against
				// legacy terminals would see every binding fire twice.
				// Opting in via WithKeyReleases restores the raw stream.
				continue
			}
			if settings.suspend && isSuspendKey(next) {
				if done, err := suspendForKey(ctx, model, settings, next, &width, &height, &dirty, startCommand); done || err != nil {
					return err
				}
				continue
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
			clear(current.presented)
			current.presented = current.presented[:0]
			delta := time.Duration(0)
			if !lastFrame.IsZero() {
				delta = now.Sub(lastFrame)
			}
			frame := Frame{Canvas: current.canvas, Now: now, Delta: delta, Capabilities: features, Metrics: metrics, ReducedMotion: settings.reducedMotion, progress: settings.progress, notifications: settings.notifications, clipboard: settings.clipboard}
			model.View(&frame)
			current.presented = append(current.presented[:0], frame.presented...)
			current.annex = append(current.annex[:0], frame.annex...)
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
				clear(recycled.presented)
				recycled.presented = recycled.presented[:0]
				// The displaced surface never reached the terminal, so its
				// sideband never happened either; the surviving queued
				// surface carries the newer annex. One-shot requests
				// therefore belong in View every time — the annex is the
				// state, and the newest state is what lands.
				recycled.annex = recycled.annex[:0]
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

// suspendTransport is the optional job-control handshake a backend can
// offer the runtime. It is deliberately not part of Backend: test fakes,
// plain streams, and SSH adapters have no process group to stop, and the
// run loop reaches it through a type assertion so those backends simply
// never suspend through this path.
type suspendTransport interface {
	// CanSuspend reports whether a handshake could start right now; the
	// answer must be knowable before any Suspend event is delivered.
	CanSuspend() bool
	// Suspend hands the terminal over, stops the process group, and blocks
	// until the shell continues it. False means the handshake never
	// started and the key should fall through to the model.
	Suspend(context.Context) (bool, error)
}

// isSuspendKey reports whether an event is the Ctrl+Z press the runtime
// claims for job control. The rune arrives spelled two ways — a legacy
// terminal sends the raw byte 0x1A, which the parser decodes as rune(byte
// + '@') = 'Z' uppercase, while the kitty keyboard protocol and the
// Windows console report the typed 'z' — so both spellings match. Presses
// and auto-repeats both qualify — a held Ctrl+Z is one suspend intent —
// and Shift does not disqualify, because a legacy terminal reports
// Ctrl+Shift+Z as the same 0x1A byte; releases are excluded so a
// WithKeyReleases stream cannot suspend twice on one press.
func isSuspendKey(next event.Event) bool {
	if next.Kind != event.Key || next.Key.Code != event.KeyRune || unicode.ToLower(next.Key.Rune) != 'z' {
		return false
	}
	return next.Key.Mod&event.ModCtrl != 0 &&
		next.Key.Mod&(event.ModAlt|event.ModSuper) == 0 &&
		next.Key.Action != event.ActionRelease
}

// suspendForKey runs the job-control handshake for one Ctrl+Z press, or —
// when the backend cannot suspend — delivers the key as the ordinary event
// it always was. The ordering is the contract: the model pauses first, so
// the leave sequences that make the terminal ordinary land on a model that
// is already suspended; the handshake then runs synchronously on the run
// loop's goroutine, which is safe precisely because the process is about
// to stop — no frame composes, no command finishes, and the reader
// goroutine is frozen in a read the kernel will not complete until the
// shell continues the group. Keystrokes that land in the stop window wait
// in the terminal's kernel buffer and replay after the continue.
//
// The Resume event is delivered from here rather than from the backend, so
// the model's suspend bookkeeping and the run loop's repaint dirtying stay
// one ordering: event, handshake, event. The backend's diff baseline is
// already dropped by its own resume path, so the first frame after resume
// is a full repaint of a screen that went through the shell's hands.
//
// An error ends the run: a hangup or failed re-entry leaves the terminal
// in a state only the deferred Close can make safe, and the leave Close
// writes on a latched-but-failed suspend is the documented idempotent
// belt-and-braces.
func suspendForKey(ctx context.Context, model Model, settings config, key event.Event, width, height *int, dirty *bool, startCommand func(Command)) (bool, error) {
	handshake, ok := settings.backend.(suspendTransport)
	if !ok || !handshake.CanSuspend() {
		// Plain streams, pipes, test fakes, Windows consoles: Ctrl+Z is
		// just a key, and models keep seeing exactly the stream a legacy
		// terminal produces.
		return applyEvent(model, key, settings, width, height, dirty, startCommand)
	}
	if done, err := applyEvent(model, event.Event{Kind: event.Suspend}, settings, width, height, dirty, startCommand); done || err != nil {
		return done, err
	}
	suspended, err := handshake.Suspend(ctx)
	if err != nil {
		return true, err
	}
	if !suspended {
		// CanSuspend promised a handshake and something raced the promise
		// — a backend closing underneath the press. The key falls through
		// so the model at least sees its input; the closing error is
		// already on its way through the events queue.
		return applyEvent(model, key, settings, width, height, dirty, startCommand)
	}
	return applyEvent(model, event.Event{Kind: event.Resume}, settings, width, height, dirty, startCommand)
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
