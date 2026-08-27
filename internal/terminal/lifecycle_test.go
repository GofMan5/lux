package terminal

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

// hangTimeout only bounds a test that would otherwise hang forever, so a loaded
// machine cannot turn a stall into a reported logic failure.
const hangTimeout = 10 * time.Second

type gatedWriter struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	data    []byte
	once    sync.Once
}

func (w *gatedWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, data...)
	return len(data), nil
}

func TestCanceledPresentNeverCommitsPrevious(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
	if err := backend.Present(ctx, render.NewBuffer(2, 1)); err == nil {
		t.Fatal("canceled Present succeeded")
	}
	if backend.previous != nil {
		t.Fatal("canceled Present committed a frame")
	}
}

func TestWriteAllContextStopsBetweenPartialWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelingWriter{cancel: cancel}
	err := writeAllContext(ctx, writer, []byte("abcdef"))
	if err == nil || len(writer.data) != 1 {
		t.Fatalf("write = %q, err=%v", writer.data, err)
	}
}

type cancelingWriter struct {
	cancel context.CancelFunc
	data   []byte
}

func (w *cancelingWriter) Write(data []byte) (int, error) {
	w.data = append(w.data, data[0])
	w.cancel()
	return 1, nil
}

func TestWriteAllContextRejectsZeroWrite(t *testing.T) {
	err := writeAllContext(context.Background(), zeroWriter{}, []byte("x"))
	if err != io.ErrShortWrite {
		t.Fatalf("error = %v", err)
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestGatedWriterFixture(t *testing.T) {
	writer := &gatedWriter{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- writeAllContext(context.Background(), writer, []byte("frame")) }()
	select {
	case <-writer.started:
	case <-time.After(hangTimeout):
		t.Fatal("writer did not start")
	}
	close(writer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFailedPresentDiscardsTheTerminalModel(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
	frame := render.NewBuffer(8, 2)
	render.NewCanvas(frame).Text(0, 0, "first", render.Style{})
	if err := backend.Present(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if backend.previous == nil {
		t.Fatal("a presented frame was not retained")
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	render.NewCanvas(frame).Text(0, 1, "second", render.Style{})
	if err := backend.Present(context.Background(), frame); err == nil {
		t.Fatal("writing to a closed file succeeded")
	}
	// The encoder mutates its model of the terminal, so a partial write must not
	// leave that model in use.
	if backend.previous != nil {
		t.Fatal("a failed write kept a stale terminal model")
	}
}

func TestCloseWritesItsRestoreWhenTheWriterIsIdle(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	backend := &Backend{opened: true, output: output, writer: output, features: capability.Set{Color: capability.TrueColor}}
	frame := render.NewBuffer(8, 2)
	render.NewCanvas(frame).Text(0, 0, "hello", render.Style{})
	if err := backend.Present(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(written, []byte(leaveTerminal)) {
		t.Fatalf("Close did not restore the terminal last: %q", written)
	}
	if bytes.Count(written, []byte(leaveTerminal)) != 1 {
		t.Fatalf("the restore sequence appears more than once: %q", written)
	}
}

func TestCloseGivesUpOnAWriterThatStopsDraining(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// Nothing drains the pipe, so a large frame fills it and blocks mid-write
	// while holding the write slot.
	t.Cleanup(func() { _ = reader.Close() })
	backend := &Backend{opened: true, output: writer, writer: writer, features: capability.Set{Color: capability.TrueColor}}
	presented := make(chan error, 1)
	frame := render.NewBuffer(400, 2000)
	render.NewCanvas(frame).Clear(render.Style{Foreground: render.RGBColor(1, 2, 3)})
	go func() { presented <- backend.Present(context.Background(), frame) }()

	deadline := time.Now().Add(hangTimeout)
	for !backend.writerBusy() {
		if time.Now().After(deadline) {
			t.Skip("the pipe accepted the whole frame, so no write was ever blocked")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan error, 1)
	start := time.Now()
	go func() { closed <- backend.Close() }()
	select {
	case <-closed:
	case <-time.After(hangTimeout):
		t.Fatal("Close hung behind a blocked write")
	}
	if elapsed := time.Since(start); elapsed > hangTimeout/2 {
		t.Fatalf("Close took %s to give up", elapsed)
	}
}
