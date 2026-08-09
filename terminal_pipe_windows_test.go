//go:build windows

package lux

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

type fullFrameModel struct{}

func (fullFrameModel) Update(event.Event) Command { return nil }
func (fullFrameModel) View(frame *Frame) {
	frame.Canvas.Fill(frame.Canvas.Bounds(), "X", render.Style{})
}

func TestDefaultBackendCancellationInterruptsFullPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, fullFrameModel{}, WithIO(nil, writer), WithFallbackSize(500, 200))
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run remained blocked on a saturated output pipe")
	}
}
