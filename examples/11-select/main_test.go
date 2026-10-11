package main

import (
	"context"
	"strings"
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

func rowText(buffer *render.Buffer, y int) string {
	var row strings.Builder
	for x := 0; x < buffer.Bounds().W; x++ {
		row.WriteString(buffer.Cell(x, y).Grapheme)
	}
	return row.String()
}

func TestSelectPaintsClosedBoxesEchoAndFooter(t *testing.T) {
	app := newSelectApp(nil, nil)
	paint := func() *render.Buffer {
		frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(paintWidth, paintHeight)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
		app.View(frame)
		return frame.Canvas.Buffer()
	}

	first := paint()
	if !strings.Contains(rowText(first, 0), "LUX SELECT") {
		t.Fatalf("title missing: %q", rowText(first, 0))
	}
	for row, want := range map[int]string{
		styleRow:  "sharp",
		motionRow: "instant",
		sideRow:   "left",
		sortRow:   "newest",
		applyRow:  "Apply",
		echoRow:   "sharp   instant   left   newest",
	} {
		if !strings.Contains(rowText(first, row), want) {
			t.Fatalf("row %d missing %q: %q", row, want, rowText(first, row))
		}
	}
	if !strings.Contains(rowText(first, paintHeight-1), "esc cancel") {
		t.Fatalf("footer missing: %q", rowText(first, paintHeight-1))
	}
	// The sort popup is closed: the space it would flip into is empty.
	for row := sortRow - 8; row < sortRow; row++ {
		if strings.Contains(rowText(first, row), "archived") {
			t.Fatalf("row %d shows a popup option while closed: %q", row, rowText(first, row))
		}
	}
}

func TestSelectTinyTerminalGuard(t *testing.T) {
	app := newSelectApp(nil, nil)
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(30, 8)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	if painted := frame.Canvas.Buffer(); !strings.Contains(rowText(painted, 0), "enlarge terminal") {
		t.Fatalf("tiny guard missing: %q", rowText(painted, 0))
	}
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

func waitStateWhere(t *testing.T, states <-chan selectState, done <-chan error, match func(selectState) bool) selectState {
	t.Helper()
	deadline := time.After(hangTimeout)
	seen := 0
	var last selectState
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
			t.Fatalf("matching state timeout after %d states; last: open=%q focused=%q applied=%d status=%q values=%v; goroutine dump follows\n%s",
				seen, last.open, last.focused, last.applied, last.status, last.values, stack)
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

// waitCommittedWhere blocks until a frame whose View BUILT the matching
// state has been COMMITTED to the event tree — not merely painted. The
// test backend pushes its frame clone inside Present, BEFORE the writer
// runs the frame's OnPresented callbacks, so "a frame carrying the state
// is on the wire" does not prove the tree inputs route against matches
// it. The committed channel fixes that by construction: the beacon the
// app queues after End fires only once the writer has committed the very
// frame that built the state. Any wait immediately followed by a key or
// a click must gate here, never on paint.
func waitCommittedWhere(t *testing.T, app *selectApp, done <-chan error, match func(selectState) bool) selectState {
	t.Helper()
	deadline := time.After(hangTimeout)
	seen := 0
	var last selectState
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
			t.Fatalf("matching commit timeout after %d states; last: open=%q focused=%q applied=%d status=%q values=%v; goroutine dump follows\n%s",
				seen, last.open, last.focused, last.applied, last.status, last.values, stack)
		}
	}
}

// waitFrameWhere keeps reading frames until one carries the predicate —
// a pure paint-content assert. It never gates input routing; see
// waitCommittedWhere for that. Callers drain and provoke the repaint
// (F6 is bound nowhere; every event dirties the model) before it.
func waitFrameWhere(t *testing.T, backend *interactiveBackend, pred func(*render.Buffer) bool) *render.Buffer {
	t.Helper()
	for {
		frame := waitFrame(t, backend.frames)
		if pred(frame) {
			return frame
		}
	}
}

func newTestApp(t *testing.T) (*selectApp, *interactiveBackend, <-chan error) {
	t.Helper()
	backend := &interactiveBackend{events: make(chan event.Event, 32), frames: make(chan *render.Buffer, 32)}
	app := newSelectApp(make(chan selectState, 64), make(chan selectState, 64))
	done := make(chan error, 1)
	go func() { done <- lux.Run(context.Background(), app, lux.WithBackend(backend), lux.WithMaxFPS(240)) }()
	waitFrame(t, backend.frames)
	// The first frame auto-claims focus for the style box; waiting for
	// its COMMIT (not just its paint) is what lets every later event
	// route against a live tree.
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.focused == "style"
	})
	return app, backend, done
}

func press(backend *interactiveBackend, x, y int) {
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MousePress, Button: event.MouseLeft, X: x, Y: y}}
}

func release(backend *interactiveBackend, x, y int) {
	backend.events <- event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{Action: event.MouseRelease, Button: event.MouseLeft, X: x, Y: y}}
}

// focus tabs forward to the wanted dropdown. The first committed frame
// already claims focus for the style box (the first focusable region),
// so the tab count is the target's index in the settings slice. Zero
// tabs means newTestApp's committed gate already proved the target:
// no event is sent, so no newer beacon can exist.
func focus(t *testing.T, app *selectApp, backend *interactiveBackend, done <-chan error, id string) {
	t.Helper()
	target := -1
	for i := range app.settings {
		if app.settings[i].id == id {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("unknown focus target %q", id)
	}
	if target == 0 {
		return
	}
	for i := 0; i < target; i++ {
		backend.events <- event.KeyPress(event.KeyTab, 0)
	}
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.focused == id
	})
}

func TestSelectOpenNavigateCommitKeyboard(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	focus(t, app, backend, done, "style")

	// Enter opens the popup below the anchor: rows 4-7 carry
	// sharp/rounded/double/ascii, covering the Motion label row 5 and
	// the motion box row 6 while it is open.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool { return state.open == "style" })
	// Paint proof of the popup's placement and coverage, strictly after
	// the committed gate above — a visual check, not a routing one.
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, styleRow+2), "rounded") &&
			strings.Contains(rowText(painted, styleRow+4), "ascii") &&
			!strings.Contains(rowText(painted, motionRow-1), "Motion")
	})

	// Down moves the choice; Enter commits it and closes the popup,
	// handing the covered rows back.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "style" && state.values[0] == "rounded"
	})
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	state := waitStateWhere(t, app.observe, done, func(state selectState) bool {
		return state.open == "" && state.values[0] == "rounded"
	})
	if state.status != "style = rounded" {
		t.Fatalf("commit status missing: %q", state.status)
	}
	// The closed popup hands the covered rows back (paint check).
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, motionRow-1), "Motion") &&
			!strings.Contains(rowText(painted, styleRow+2), "rounded")
	})
}

func TestSelectEscapeRevertsUncommittedChoice(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	focus(t, app, backend, done, "sort")

	// Down opens the popup and moves the choice in one step; the revert
	// target is the pre-open choice, snapshotted before Apply.
	backend.events <- event.KeyPress(event.KeyDown, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "sort" && state.values[3] == "oldest"
	})

	backend.events <- event.KeyPress(event.KeyEscape, 0)
	// The status must be part of the match, not a follow-up assert: the
	// observe channel has accumulated every state since startup (the
	// intermediate gates read the committed beacon instead), and the
	// pre-Down states also satisfy open=="" with the default value.
	state := waitStateWhere(t, app.observe, done, func(state selectState) bool {
		return state.open == "" && state.values[3] == "newest" && state.status == "sort cancelled"
	})
	if state.focused != "sort" {
		t.Fatalf("cancel dropped focus: %+v", state)
	}
	// The popup's rows are gone from the screen (paint check).
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend, func(painted *render.Buffer) bool {
		return !strings.Contains(rowText(painted, sortRow-1), "hot")
	})
}

func TestSelectPopupFlipsAboveAndClickCommits(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	focus(t, app, backend, done, "sort")

	// At the bottom edge the popup cannot fit below (18+1+8 > 24), so it
	// flips above the anchor: options fill rows 10-17, the anchor row
	// stays the closed box, and the popup covers what it overlaps.
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool { return state.open == "sort" })
	// Paint proof of the flip: options fill rows 10-17 above the anchor.
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, sortRow-8), "newest") &&
			strings.Contains(rowText(painted, sortRow-1), "hot") &&
			strings.Contains(rowText(painted, sortRow), "newest")
	})

	// A click on a flipped-popup row picks on the press and commits on
	// the release: row 12 is option index 2 ("name").
	press(backend, 6, 12)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "sort" && state.values[3] == "name"
	})
	release(backend, 6, 12)
	state := waitStateWhere(t, app.observe, done, func(state selectState) bool {
		return state.open == "" && state.values[3] == "name"
	})
	if state.status != "sort = name" {
		t.Fatalf("commit status missing: %q", state.status)
	}
}

func TestSelectOutsideClickDismissesWithoutClickThrough(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	focus(t, app, backend, done, "sort")
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool { return state.open == "sort" })

	// The press on the Apply button while the popup is open must become
	// the popup's Dismiss — revert, and the button must NOT fire.
	press(backend, 4, applyRow)
	state := waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "" && state.values[3] == "newest"
	})
	if state.status != "sort cancelled" {
		t.Fatalf("dismiss status missing: %q", state.status)
	}
	// The release of the dismissed press is equally inert.
	release(backend, 4, applyRow)
	time.Sleep(50 * time.Millisecond)
	select {
	case state := <-app.observe:
		if state.applied != 0 {
			t.Fatalf("the dismissed click reached the button: applied=%d", state.applied)
		}
	default:
	}

	// A full click with nothing open reaches the button.
	press(backend, 4, applyRow)
	release(backend, 4, applyRow)
	state = waitStateWhere(t, app.observe, done, func(state selectState) bool {
		return state.applied == 1
	})
	if state.status != "applied" {
		t.Fatalf("apply status missing: %q", state.status)
	}
}

func TestSelectTabTrapsWhilePopupOpen(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	focus(t, app, backend, done, "style")
	backend.events <- event.KeyPress(event.KeyEnter, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool { return state.open == "style" })

	// Tab cannot leave the popup: the modal scope's single focusable
	// region is the select itself, so focus stays and the popup stays.
	backend.events <- event.KeyPress(event.KeyTab, 0)
	state := waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.focused == "style"
	})
	if state.open != "style" || state.applied != 0 {
		t.Fatalf("tab changed the open state: %+v", state)
	}

	// Escape closes, focus lands back on the select, and the next Tab
	// moves on to the next dropdown — the trap released cleanly. The
	// beacon above is what makes the Tab safe: it routes against the
	// released tree, not the still-committed modal.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "" && state.focused == "style"
	})
	backend.events <- event.KeyPress(event.KeyTab, 0)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.focused == "motion"
	})

	// With nothing open, Escape quits — the popup never stole the
	// application binding.
	backend.events <- event.KeyPress(event.KeyEscape, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
	case <-time.After(hangTimeout):
		stack := make([]byte, 1<<20)
		stack = stack[:runtime.Stack(stack, true)]
		t.Fatalf("escape did not quit; goroutine dump follows\n%s", stack)
	}
}

func TestSelectMouseOnlyFlowOnRightColumn(t *testing.T) {
	app, backend, done := newTestApp(t)
	defer close(backend.events)

	// The side select on the right column, used without any prior focus:
	// the press focuses it, the release opens the popup below.
	press(backend, 36, sideRow)
	release(backend, 36, sideRow)
	waitCommittedWhere(t, app, done, func(state selectState) bool { return state.open == "side" })
	// Paint proof the popup opened below the anchor.
	drainFrames(backend.frames)
	backend.events <- event.KeyPress(event.KeyF6, 0)
	waitFrameWhere(t, backend, func(painted *render.Buffer) bool {
		return strings.Contains(rowText(painted, sideRow+1), "left")
	})

	// Clicking the second option commits it.
	press(backend, 38, sideRow+2)
	waitCommittedWhere(t, app, done, func(state selectState) bool {
		return state.open == "side" && state.values[2] == "right"
	})
	release(backend, 38, sideRow+2)
	state := waitStateWhere(t, app.observe, done, func(state selectState) bool {
		return state.open == "" && state.values[2] == "right"
	})
	if state.status != "side = right" {
		t.Fatalf("commit status missing: %q", state.status)
	}
}
