package terminal

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/render"
)

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
	backend := &Backend{opened: true, output: output, features: capability.Set{Color: capability.TrueColor}}
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
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	close(writer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
