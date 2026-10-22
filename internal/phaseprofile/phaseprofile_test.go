package phaseprofile

import (
	"context"
	"fmt"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

type counter struct{ n int }

func (c *counter) Update(next event.Event) lux.Command {
	if next.Kind == event.Key {
		c.n++
	}
	return nil
}

func (c *counter) View(frame *lux.Frame) {
	frame.Canvas.Text(0, 0, fmt.Sprint("count ", c.n), render.Style{})
}

func TestRunMeasuresEveryScriptedFrameWithoutFlush(t *testing.T) {
	script := make([]event.Event, 10)
	for i := range script {
		script[i] = event.RuneKey('a', 0)
	}
	model := &counter{}
	result, err := Run(context.Background(), model, 120, 40, script)
	if err != nil {
		t.Fatal(err)
	}
	if model.n != len(script) {
		t.Fatalf("model saw %d keys, want %d", model.n, len(script))
	}
	// The initial frame plus one per event: pacing must not coalesce.
	if len(result.Frames) < len(script)+1 {
		t.Fatalf("presented %d frames, want at least %d", len(result.Frames), len(script)+1)
	}
	for i, m := range result.Frames {
		if !m.BackendTiming || m.FlushTime != 0 || m.PreparationTime != m.UpdateTime+m.ViewTime+m.DiffEncodeTime+m.EncodeTime {
			t.Fatalf("frame %d metrics = %+v, want timed, flush-free preparation sum", i, m)
		}
	}
	if result.Bytes == 0 {
		t.Fatal("profile encoded no bytes")
	}
}

func TestPercentileNearestRank(t *testing.T) {
	var r Result
	for i := 1; i <= 20; i++ {
		r.Frames = append(r.Frames, lux.Metrics{PreparationTime: time.Duration(i)})
	}
	for p, want := range map[float64]time.Duration{50: 10, 95: 19, 99: 20, 100: 20} {
		if got := r.Percentile(p, Preparation); got != want {
			t.Fatalf("p%v = %v, want %v", p, got, want)
		}
	}
	if got := (Result{}).Percentile(95, Preparation); got != 0 {
		t.Fatalf("empty p95 = %v, want 0", got)
	}
}

func TestClockStepIsPositiveAndBounded(t *testing.T) {
	if step := clockStep(); step <= 0 || step > 100*time.Millisecond {
		t.Fatalf("clock step = %v, want (0, 100ms]", step)
	}
}

// leaky retains one KiB per key, the shape of an unbounded history.
type leaky struct{ kept [][]byte }

func (l *leaky) Update(next event.Event) lux.Command {
	if next.Kind == event.Key {
		l.kept = append(l.kept, make([]byte, 1024))
	}
	return nil
}

func (l *leaky) View(frame *lux.Frame) {
	frame.Canvas.Text(0, 0, fmt.Sprint(len(l.kept)), render.Style{})
}

func TestSoakSamplesEveryCycleAndSeesGrowth(t *testing.T) {
	cycle := []event.Event{event.RuneKey('a', 0), event.ResizeTo(100, 30), event.RuneKey('b', 0), event.ResizeTo(120, 40)}
	model := &leaky{}
	samples, err := Soak(context.Background(), model, 120, 40, cycle, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 40 || len(model.kept) != 80 {
		t.Fatalf("samples=%d keys=%d, want 40 and 80", len(samples), len(model.kept))
	}
	if grown := int64(samples[39].Heap) - int64(samples[4].Heap); grown < 35*2*1024 {
		t.Fatalf("leak of 2 KiB/cycle grew live heap only %d B over 35 cycles", grown)
	}
	t.Setenv("LUX_SOAK", "1")
	CheckBounded(t, 120, 40, &counter{}, cycle, 40, 4, 64<<10)
}
