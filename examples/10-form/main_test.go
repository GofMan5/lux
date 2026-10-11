package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"runtime"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// hangTimeout bounds a test that would otherwise wait forever; a loaded
// machine should never turn a stall into a false failure. A starved shared
// CI runner can freeze the process for tens of seconds while sibling
// package binaries run, so the guard absorbs runner starvation and still
// fails a real hang long before go test's package timeout.
const hangTimeout = 45 * time.Second

// The layout contract the paint assertions depend on: at 64x24 the form
// paints label / input / hint-or-error rows for each field, a read-only
// endpoint preview, a connect button, a status line, and a footer.
const (
	paintWidth   = 64
	paintHeight  = 24
	nameRow      = 3
	nameHintRow  = 4
	hostRow      = 6
	hostHintRow  = 7
	portRow      = 9
	portHintRow  = 10
	notesRow     = 12
	notesHintRow = 13
	endpointRow  = 15
	buttonRow    = 18
	statusRow    = 22
)

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

func TestFormPaintsFieldsValidationAndInFlight(t *testing.T) {
	app := newFormApp(nil, nil, nil)
	app.width, app.height = paintWidth, paintHeight
	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(paintWidth, paintHeight)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	first := paint()
	for row, want := range map[int]string{
		2:           "Name",
		nameRow:     "profile name",
		nameHintRow: "shown in the profile list",
		5:           "Host",
		hostRow:     "host or address",
		8:           "Port",
		notesRow:    "",
		11:          "Notes",
		endpointRow: "endpoint",
		buttonRow:   "Connect",
	} {
		if want == "" {
			continue
		}
		if !strings.Contains(rowText(first, row), want) {
			t.Fatalf("row %d missing %q: %q", row, want, rowText(first, row))
		}
	}
	if strings.Contains(rowText(first, nameHintRow), "required") {
		t.Fatalf("error shown before any submit: %q", rowText(first, nameHintRow))
	}

	// Submitting an empty form paints every required-field error in place
	// of the hints. The nil source is never touched: validation fails first.
	app.submit()
	invalid := paint()
	for _, row := range []int{nameHintRow, hostHintRow, portHintRow} {
		if !strings.Contains(rowText(invalid, row), "required") {
			t.Fatalf("row %d missing validation error: %q", row, rowText(invalid, row))
		}
	}
	if strings.Contains(rowText(invalid, notesHintRow), "required") {
		t.Fatalf("optional notes must not demand a value: %q", rowText(invalid, notesHintRow))
	}

	// A valid form enters the connecting state: fields dim, the button
	// relabels, the endpoint preview shows the composed address.
	app.fields[0].input.SetValue("dev")
	app.fields[1].input.SetValue("relay")
	app.fields[2].input.SetValue("5900")
	app.refreshEndpoint()
	app.submit()
	connecting := paint()
	if !strings.Contains(rowText(connecting, endpointRow), "relay:5900") {
		t.Fatalf("endpoint preview missing: %q", rowText(connecting, endpointRow))
	}
	if !strings.Contains(rowText(connecting, buttonRow), "connecting") {
		t.Fatalf("in-flight button label missing: %q", rowText(connecting, buttonRow))
	}
	if !strings.Contains(rowText(connecting, statusRow), "connecting to relay:5900") {
		t.Fatalf("in-flight status missing: %q", rowText(connecting, statusRow))
	}
	if connecting.Cell(4, nameRow).Style.Attrs&render.AttrDim == 0 {
		t.Fatalf("field not dimmed while connecting")
	}

	// A failed connect is data, not a crash: the notice explains it and
	// the form returns to the editable state with the draft intact.
	app.applyConnect(connectResult{attempt: app.attempt, err: errors.New("dial tcp: timeout")})
	failed := paint()
	if !strings.Contains(rowText(failed, statusRow), "connect failed") {
		t.Fatalf("failure notice missing: %q", rowText(failed, statusRow))
	}
	if !strings.Contains(rowText(failed, buttonRow), "Connect") {
		t.Fatalf("button must return from connecting: %q", rowText(failed, buttonRow))
	}

	// A successful connect saves the draft: the notice carries the
	// endpoint and the elapsed time.
	app.applyConnect(connectResult{attempt: app.attempt, elapsed: 40 * time.Millisecond})
	connected := paint()
	if !strings.Contains(rowText(connected, statusRow), "connected to relay:5900 in 40ms") {
		t.Fatalf("success notice missing: %q", rowText(connected, statusRow))
	}
}

func TestFormTinyTerminalGuard(t *testing.T) {
	app := newFormApp(nil, nil, nil)
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	if painted := frame.Canvas.Buffer(); !strings.Contains(rowText(painted, 0), "enlarge terminal") {
		t.Fatalf("tiny guard missing: %q", rowText(painted, 0))
	}
}

// connectStep is one scripted Connect answer: after the delay, the step
// yields its error or success.
type connectStep struct {
	delay time.Duration
	err   error
}

// scriptedFormSource is the deterministic stand-in for the network: steps
// are consumed in order (the last one repeats), every call's profile is
// logged, and the reported elapsed time is the scripted delay so the
// success notice stays reproducible.
type scriptedFormSource struct {
	mu    sync.Mutex
	steps []connectStep
	calls int
	log   []profile
}

func newScriptedFormSource(steps ...connectStep) *scriptedFormSource {
	if len(steps) == 0 {
		panic("scriptedFormSource needs at least one step")
	}
	return &scriptedFormSource{steps: steps}
}

func (s *scriptedFormSource) Connect(ctx context.Context, p profile) (time.Duration, error) {
	s.mu.Lock()
	s.log = append(s.log, p)
	step := s.steps[min(s.calls, len(s.steps)-1)]
	s.calls++
	s.mu.Unlock()
	if step.delay > 0 {
		timer := time.NewTimer(step.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return step.delay, step.err
}

func (s *scriptedFormSource) profiles() []profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]profile(nil), s.log...)
}

type interactiveBackend struct {
	events chan event.Event
	frames chan *render.Buffer
}

func (b *interactiveBackend) Open(context.Context) (capability.Set, error) {
	return capability.Set{Interactive: true, Unicode: true, Color: capability.TrueColor}, nil
}
func (b *interactiveBackend) NextEvent(ctx context.Context) (event.Event, error) {
	select {
	case next := <-b.events:
		return next, nil
	case <-ctx.Done():
		return event.Event{}, ctx.Err()
	}
}
func (b *interactiveBackend) Present(_ context.Context, frame *render.Buffer) error {
	select {
	case b.frames <- frame.Clone():
	default:
	}
	return nil
}
func (b *interactiveBackend) Size() (int, int, error) { return paintWidth, paintHeight, nil }
func (*interactiveBackend) Close() error              { return nil }

func waitStateWhere(t *testing.T, states <-chan formState, done <-chan error, match func(formState) bool) formState {
	t.Helper()
	deadline := time.After(hangTimeout)
	seen := 0
	var last formState
	for {
		select {
		case state := <-states:
			seen++
			last = state
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the state arrived: %v", err)
		case <-deadline:
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			t.Fatalf("matching state timeout after %d states; last: dirty=%v connecting=%v ask=%v attempts=%d focused=%q notice=%q endpoint=%q values=%v errors=%v; goroutine dump follows\n%s",
				seen, last.dirty, last.connecting, last.ask, last.attempts, last.focused, last.notice, last.endpoint, last.values, last.errors, stack)
		}
	}
}

func waitFrame(t *testing.T, frames <-chan *render.Buffer) *render.Buffer {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(hangTimeout):
		stack := make([]byte, 1<<20)
		stack = stack[:runtime.Stack(stack, true)]
		t.Fatalf("frame timeout; goroutine dump follows\n%s", stack)
		return nil
	}
}

// drainFrames discards the backlog so the next waitFrame observes the
// frame the test just provoked, not an old phase.
func drainFrames(frames <-chan *render.Buffer) {
	for {
		select {
		case <-frames:
		default:
			return
		}
	}
}

// waitCommittedWhere waits until a frame that painted the matched state
// is COMMITTED to the event tree — the beacon View queues after ui.End
// fires only after the writer ran that frame's commit, so any keystroke
// or click sent afterwards routes against the matched state's tree. A
// wire frame never proves that: the backend pushes its clone inside
// Present, BEFORE the writer runs the callbacks, so "a frame carrying
// the state is on the wire" can predate the commit entirely. Beacons
// are consumed in order, so after a gate matches, the next beacon is
// exactly the next input's state — a predicate only has to miss the
// states between the previous gate's match and the target.
func waitCommittedWhere(t *testing.T, app *formApp, done <-chan error, match func(formState) bool) formState {
	t.Helper()
	deadline := time.After(hangTimeout)
	seen := 0
	var last formState
	for {
		select {
		case state := <-app.committed:
			seen++
			last = state
			if match(state) {
				return state
			}
		case err := <-done:
			t.Fatalf("run ended before the commit arrived: %v", err)
		case <-deadline:
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			t.Fatalf("matching commit timeout after %d states; last: dirty=%v connecting=%v ask=%v attempts=%d focused=%q notice=%q endpoint=%q values=%v errors=%v; goroutine dump follows\n%s",
				seen, last.dirty, last.connecting, last.ask, last.attempts, last.focused, last.notice, last.endpoint, last.values, last.errors, stack)
		}
	}
}

// paintWhere drains the frame backlog, provokes one repaint with F6
// (bound nowhere), and waits until a frame matching pred arrives. This
// is a paint assert only: it proves content reaches the wire, never
// that the tree is committed — input routing gates on the committed
// beacon. The model already holds the state the test observed, so
// every frame painted from the forced repaint on carries it; the loop
// keeps reading until one does. A wrong expectation hangs into
// waitFrame's dump, exactly like the single-shot wait did.
func paintWhere(t *testing.T, backend *interactiveBackend, pred func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	for {
		frame := waitFrame(t, backend.frames)
		if pred(frame) {
			return frame
		}
	}
}

func typeRunes(backend *interactiveBackend, text string) {
	for _, r := range text {
		backend.events <- event.RuneKey(r, 0)
	}
}

func ctrlS(backend *interactiveBackend) {
	backend.events <- event.RuneKey('s', event.ModCtrl)
}

func TestFormEndToEndValidationSubmitAndConnect(t *testing.T) {
	// The dial delays are a budget, not realism: the test must reliably
	// observe the in-flight phase (state and painted frame) before the
	// result lands. Under coverage instrumentation and parallel package
	// load the runner can be starved for tens of milliseconds, and a
	// short delay lets the result coalesce with the submit event — the
	// connecting View would never paint and the waits would hang.
	source := newScriptedFormSource(
		connectStep{delay: 400 * time.Millisecond, err: errors.New("dial tcp 203.0.113.7:5900: connection refused")},
		connectStep{delay: 400 * time.Millisecond},
	)
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newFormApp(source, make(chan formState, 64), make(chan formState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)

	// The state above arrived from inside the first View, before the
	// writer's present callbacks committed the regions: a keystroke that
	// beats the first commit is dropped on an empty focus table. The
	// first frame's beacon — the auto-claimed focus on the name field —
	// fires only after that frame's tree is committed.
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.focused == "name"
	})

	// Typing into the focused name field: the first frame focuses it.
	typeRunes(backend, "dev")
	state := waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.values[0] == "dev" && state.dirty
	})
	if state.errors[0] != "" {
		t.Fatalf("name must not hold an error before submit: %q", state.errors[0])
	}

	// Submitting with host and port empty paints their errors, clears the
	// notice, and moves focus to the first invalid field.
	ctrlS(backend)
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.errors[1] != "" && state.focused == "host"
	})
	if state.errors[2] == "" {
		t.Fatalf("port error missing: %+v", state)
	}
	if state.errors[0] != "" || state.connecting || state.attempts != 0 {
		t.Fatalf("unexpected state after invalid submit: %+v", state)
	}
	// The hint row renders the error in place of the hint (paint check
	// only; the routing gate above already proved the tree).
	paintWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, hostHintRow), "required")
	})

	// Typing the host clears its own error inline; the port error survives.
	typeRunes(backend, "relay")
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.values[1] == "relay" && state.errors[1] == ""
	})
	if state.errors[2] == "" {
		t.Fatalf("port error must survive the host fix: %+v", state)
	}

	// Tab to the port and fix it: the endpoint preview follows the edits.
	backend.events <- event.KeyPress(event.KeyTab, 0)
	typeRunes(backend, "5900")
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.values[2] == "5900" && state.errors[2] == "" && state.endpoint == "relay:5900" && state.focused == "port"
	})

	// Submit: the connect goes in flight. A second Ctrl+S during the
	// flight is swallowed by the single-flight guard.
	ctrlS(backend)
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.connecting && state.attempts == 1
	})
	ctrlS(backend)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.connecting && state.attempts == 1
	})
	if !strings.Contains(state.notice, "connecting to relay:5900") {
		t.Fatalf("in-flight notice missing: %q", state.notice)
	}
	// The button relabels in flight (paint check only).
	paintWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, buttonRow), "connecting")
	})

	// The scripted dial fails: the run survives, the notice explains, and
	// the draft stays dirty (a connect is not proven until it succeeds).
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return !state.connecting && strings.Contains(state.notice, "connect failed") && state.dirty
	})

	// The failure is on screen: the status row renders the notice (paint
	// check only — the gate above already proved the tree the pointer
	// must route against has the failure state committed).
	paintWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, statusRow), "connect failed")
	})

	// Retry through the button: a mouse press focuses it, the release
	// activates it, and only the release reaches submit.
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: 10, Y: buttonRow}}
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: 10, Y: buttonRow}}
	waitStateWhere(t, app.observe, done, func(state formState) bool {
		return state.connecting && state.attempts == 2
	})

	// The second attempt succeeds: the draft becomes the saved baseline.
	// The committed gate matters here too: Escape must route against the
	// tree that already knows the form is clean, or the unsaved-changes
	// guard would ask instead of quitting.
	state = waitCommittedWhere(t, app, done, func(state formState) bool {
		return !state.connecting && strings.Contains(state.notice, "connected to relay:5900") && !state.dirty
	})

	// A clean form quits without any ask.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("escape did not quit the clean form")
	}

	profiles := source.profiles()
	want := profile{name: "dev", host: "relay", port: "5900"}
	if len(profiles) != 2 || profiles[0] != want || profiles[1] != want {
		t.Fatalf("connect log = %+v; want two attempts with the full profile", profiles)
	}
}

func TestFormEndToEndUnsavedChangesGuard(t *testing.T) {
	source := newScriptedFormSource(connectStep{delay: 10 * time.Millisecond})
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newFormApp(source, make(chan formState, 64), make(chan formState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()

	waitFrame(t, backend.frames)
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.focused == "name"
	})

	// One edit makes the draft dirty; quitting now must ask first.
	typeRunes(backend, "d")
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.values[0] == "d" && state.dirty
	})
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.ask
	})
	// The ask renders as its own status line (paint check only). Frames
	// predating the ask show the plain " unsaved " status word, which
	// would also match a substring check; the ask line carries the full
	// question, so the predicate pins that.
	paintWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, statusRow), "unsaved changes")
	})

	// 'n' keeps editing: the run continues, the draft survives. The
	// predicate rides the committed channel on purpose: on the observe
	// backlog the same match is stale — every pre-ask state also has
	// !ask and values[0]=="d" — but the ask gate above consumed the
	// backlog up through the ask beacon, so the next beacon is 'n's
	// own frame.
	backend.events <- event.RuneKey('n', 0)
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return !state.ask && state.values[0] == "d"
	})

	// Escape asks again; 'y' discards and quits.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitCommittedWhere(t, app, done, func(state formState) bool {
		return state.ask
	})
	backend.events <- event.RuneKey('y', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(hangTimeout):
		t.Fatal("'y' did not discard and quit")
	}

	if profiles := source.profiles(); len(profiles) != 0 {
		t.Fatalf("connect log = %+v; want none: the guard quit before any submit", profiles)
	}
}
