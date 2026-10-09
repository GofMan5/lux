package lux

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// annexBackend records the order the runtime touches a backend that
// implements AnnexWriter: every Present and every WriteAnnex lands in one
// journal, so the tests can pin the discipline the byte-truth lanes
// cannot — the annex follows its own frame on the same goroutine, and a
// frame that fails takes its annex with it.
type annexBackend struct {
	fakeBackend
	mu      sync.Mutex
	journal []string
	annexes [][]byte
}

func newAnnexBackend(width, height int) *annexBackend {
	return &annexBackend{fakeBackend: fakeBackend{events: make(chan event.Event, 32), presented: make(chan *render.Buffer, 32), width: width, height: height}}
}

func (b *annexBackend) Present(ctx context.Context, frame *render.Buffer) error {
	err := b.fakeBackend.Present(ctx, frame)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.journal = append(b.journal, "present:err")
	} else {
		b.journal = append(b.journal, "present")
	}
	return err
}

func (b *annexBackend) WriteAnnex(ctx context.Context, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.journal = append(b.journal, "annex")
	b.annexes = append(b.annexes, append([]byte(nil), data...))
	return nil
}

// annexModel draws one frame and queues one of each sideband — progress,
// notification, clipboard — or none of them, per construction. It quits
// on the first escape press.
type annexModel struct {
	testModel
	progress, notify, clipboard bool
}

func (m *annexModel) View(frame *Frame) {
	m.testModel.View(frame)
	frame.Canvas.Text(1, 0, "annex", render.Style{})
	if m.progress {
		frame.SetProgress(ProgressNormal, 42)
	}
	if m.notify {
		frame.Notify("Done", "the build passed")
	}
	if m.clipboard {
		frame.CopyToClipboard("copied text")
	}
}

func runAnnexModel(t *testing.T, backend Backend, model *annexModel, options ...Option) error {
	t.Helper()
	// quitOn is set before Run starts: testModel reads it under its own
	// mutex from Update goroutines, so the write must precede them all.
	model.testModel.quitOn = event.Key
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), model, append([]Option{WithBackend(backend), WithMaxFPS(240)}, options...)...)
	}()
	if recorder, ok := backend.(*annexBackend); ok {
		select {
		case <-recorder.presented:
		case <-time.After(hangTimeout):
			t.Fatal("initial frame timeout")
		}
	} else {
		select {
		case <-backend.(*fakeBackend).presented:
		case <-time.After(hangTimeout):
			t.Fatal("initial frame timeout")
		}
	}
	backendFeed(backend, event.KeyPress(event.KeyEscape, 0))
	return <-done
}

func backendFeed(backend Backend, next event.Event) {
	switch typed := backend.(type) {
	case *annexBackend:
		typed.events <- next
	case *fakeBackend:
		typed.events <- next
	}
}

// TestAnnexRidesTheFrame pins the core seam: a View that queues sideband
// bytes gets them written after its own frame's Present — never before
// it, never interleaved with another frame — and exactly once. The three
// features queue together into one annex write, because they share one
// frame.
func TestAnnexRidesTheFrame(t *testing.T) {
	backend := newAnnexBackend(40, 4)
	model := &annexModel{progress: true, notify: true, clipboard: true}
	if err := runAnnexModel(t, backend, model, WithProgress(true), WithNotifications(true), WithClipboard(true)); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.journal) != 2 || backend.journal[0] != "present" || backend.journal[1] != "annex" {
		t.Fatalf("journal = %v, want [present annex]", backend.journal)
	}
	if len(backend.annexes) != 1 {
		t.Fatalf("annex writes = %d, want 1", len(backend.annexes))
	}
	want := "\x1b]9;4;2;42\a" + "\x1b]777;notify;Done;the build passed\x1b\\" + "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("copied text")) + "\x1b\\"
	if string(backend.annexes[0]) != want {
		t.Fatalf("annex = %q, want %q", backend.annexes[0], want)
	}
}

// TestAnnexZeroFrameQueuesNothing pins the gate at the Frame level: the
// zero Frame every widget test constructs has all three flags off, so
// SetProgress, Notify, and CopyToClipboard queue nothing — a build that
// never opts in is byte-identical to a build without the features.
func TestAnnexZeroFrameQueuesNothing(t *testing.T) {
	frame := &Frame{}
	frame.SetProgress(ProgressNormal, 42)
	frame.Notify("Done", "the build passed")
	frame.CopyToClipboard("copied text")
	if len(frame.annex) != 0 {
		t.Fatalf("the zero Frame queued annex bytes: %q", frame.annex)
	}
}

// TestAnnexWithoutCapabilityIsIgnored pins the optional-interface half of
// the seam: a backend that does not implement AnnexWriter — the fake
// backend here — runs an armed model without a single error or write;
// the requests are ignored exactly as a terminal without the protocols
// ignores the bytes.
func TestAnnexWithoutCapabilityIsIgnored(t *testing.T) {
	backend := newFakeBackend(40, 4)
	model := &annexModel{progress: true, notify: true, clipboard: true}
	if err := runAnnexModel(t, backend, model, WithProgress(true), WithNotifications(true), WithClipboard(true)); err != nil {
		t.Fatal(err)
	}
	if model.frames != 1 {
		t.Fatalf("frames = %d, want the single first frame", model.frames)
	}
}

// TestAnnexGateOffDropsEveryKind pins each option independently: with
// only one of the three armed, the frame queues only that sideband, so a
// partial opt-in is not an all-or-nothing switch.
func TestAnnexGateOffDropsEveryKind(t *testing.T) {
	cases := []struct {
		name    string
		options []Option
		want    string
	}{
		{name: "progress alone", options: []Option{WithProgress(true)}, want: "\x1b]9;4;2;42\a"},
		{name: "notifications alone", options: []Option{WithNotifications(true)}, want: "\x1b]777;notify;Done;the build passed\x1b\\"},
		{name: "clipboard alone", options: []Option{WithClipboard(true)}, want: "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("copied text")) + "\x1b\\"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			backend := newAnnexBackend(40, 4)
			model := &annexModel{progress: true, notify: true, clipboard: true}
			if err := runAnnexModel(t, backend, model, test.options...); err != nil {
				t.Fatal(err)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.annexes) != 1 || string(backend.annexes[0]) != test.want {
				t.Fatalf("annex = %q, want %q", backend.annexes, test.want)
			}
		})
	}
}

// TestAnnexDroppedWithFailedFrame pins the transaction: a frame whose
// Present fails never writes its annex — the sideband belongs to a frame
// the terminal never received, and a toast about a frame that is not on
// the screen is a lie.
func TestAnnexDroppedWithFailedFrame(t *testing.T) {
	backend := newAnnexBackend(40, 4)
	backend.fakeBackend.presentErr = errors.New("present failed")
	model := &annexModel{progress: true}
	model.testModel.quitOn = event.Key
	// No present ever succeeds, so there is nothing to wait for: the run
	// ends on the presentation error itself.
	err := Run(context.Background(), model, WithBackend(backend), WithMaxFPS(240), WithProgress(true))
	if err == nil {
		t.Fatal("Run must surface the presentation error")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.annexes) != 0 {
		t.Fatalf("a failed frame wrote its annex: %q", backend.annexes)
	}
	if len(backend.journal) != 1 || backend.journal[0] != "present:err" {
		t.Fatalf("journal = %v, want [present:err]", backend.journal)
	}
}
