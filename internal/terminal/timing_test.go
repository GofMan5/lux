package terminal

import (
	"context"
	"os"
	"testing"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/render"
)

func TestPresentWithTimingSeparatesPreparationFromFlush(t *testing.T) {
	output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open dev null: %v", err)
	}
	defer func() { _ = output.Close() }()

	previous := render.NewBuffer(800, 512)
	frame := render.NewBuffer(800, 512)
	render.NewCanvas(frame).Put(0, 0, "changed", render.Style{})
	backend := &Backend{
		writer:   output,
		opened:   true,
		features: capability.Set{Color: capability.TrueColor},
		previous: previous,
	}

	timing, err := backend.PresentWithTiming(context.Background(), frame)
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	if timing.DiffEncode <= 0 {
		t.Fatalf("preparation timing = %#v, want diff+encode work", timing)
	}
	if timing.Flush < 0 || timing.DiffEncode < 0 || timing.Encode < 0 {
		t.Fatalf("negative phase timing: %#v", timing)
	}

	unchanged, err := backend.PresentWithTiming(context.Background(), frame)
	if err != nil {
		t.Fatalf("present unchanged: %v", err)
	}
	if unchanged.Flush != 0 {
		t.Fatalf("unchanged frame flush = %s, want zero", unchanged.Flush)
	}
}
