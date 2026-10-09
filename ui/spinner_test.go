package ui

import (
	"testing"
	"time"

	"github.com/GofMan5/lux/render"
)

var spinNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func TestSpinnerStateAgeIsHonest(t *testing.T) {
	var fresh SpinnerState
	if got := fresh.Age(spinNow); got != 0 {
		t.Fatalf("not-started Age = %v; want 0", got)
	}
	running := SpinnerState{Started: spinNow.Add(-150 * time.Millisecond)}
	if got := running.Age(spinNow); got != 150*time.Millisecond {
		t.Fatalf("Age = %v; want 150ms", got)
	}
	// Clock skew (now before start) clamps to 0 instead of going negative.
	skewed := SpinnerState{Started: spinNow.Add(time.Second)}
	if got := skewed.Age(spinNow); got != 0 {
		t.Fatalf("skewed Age = %v; want 0", got)
	}
	var nilState *SpinnerState
	if got := nilState.Age(spinNow); got != 0 {
		t.Fatalf("nil Age = %v; want 0", got)
	}
}

func TestSpinnerFrameIsAbsoluteTime(t *testing.T) {
	tests := []struct {
		age, duration time.Duration
		frames        int
		want          int
	}{
		{0, 100 * time.Millisecond, 4, 0},
		{99 * time.Millisecond, 100 * time.Millisecond, 4, 0},
		{100 * time.Millisecond, 100 * time.Millisecond, 4, 1},
		{250 * time.Millisecond, 100 * time.Millisecond, 4, 2},
		{400 * time.Millisecond, 100 * time.Millisecond, 4, 0},
		{0, 0, 4, 0},
		{time.Second, 100 * time.Millisecond, 0, 0},
	}
	for _, test := range tests {
		if got := spinnerFrame(test.age, test.duration, test.frames); got != test.want {
			t.Fatalf("spinnerFrame(%v, %v, %d) = %d; want %d", test.age, test.duration, test.frames, got, test.want)
		}
	}
	// The absolute-time contract: the same age paints the same glyph whatever
	// the presenter's frame rate — only whole elapsed frames advance it.
	if spinnerFrame(345*time.Millisecond, 100*time.Millisecond, 4) != 3 {
		t.Fatal("345ms must be frame 3: time, not frames, drives the orbit")
	}
}

func TestSpinnerDrawsGlyphByAge(t *testing.T) {
	buffer := render.NewBuffer(8, 4)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// The default orbit is ASCII at 100ms per frame: | / - \.
	ages := []time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond}
	want := []string{"|", "/", "-"}
	for i, age := range ages {
		state := SpinnerState{Started: spinNow.Add(-age)}
		ctx.Spinner(canvas, render.Rect{X: 0, Y: i, W: 1, H: 1}, &state, spinNow, SpinnerStyle{})
		if got := bufferRow(buffer, i); got != want[i] {
			t.Fatalf("age %v glyph = %q; want %q", age, got, want[i])
		}
	}
	// A not-started spinner stays on its resting glyph.
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 3, W: 1, H: 1}, &SpinnerState{}, spinNow, SpinnerStyle{})
	if got := bufferRow(buffer, 3); got != "|" {
		t.Fatalf("resting glyph = %q; want |", got)
	}
}

func TestSpinnerDrawsCustomFramesAndWidth(t *testing.T) {
	buffer := render.NewBuffer(6, 2)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	// Braille frames honor their own duration; 30ms of 40ms is still frame 0.
	state := SpinnerState{Started: spinNow.Add(-30 * time.Millisecond)}
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 0, W: 1, H: 1}, &state, spinNow, SpinnerStyle{
		Frames: SpinnerBraille, FrameDuration: 40 * time.Millisecond,
	})
	if got := bufferRow(buffer, 0); got != "⠋" {
		t.Fatalf("braille glyph = %q; want ⠋", got)
	}
	// A zero duration falls back to 100ms: 150ms is frame 1.
	later := SpinnerState{Started: spinNow.Add(-150 * time.Millisecond)}
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 1, W: 1, H: 1}, &later, spinNow, SpinnerStyle{
		Frames: SpinnerArc,
	})
	if got := bufferRow(buffer, 1); got != "◓" {
		t.Fatalf("arc glyph = %q; want ◓", got)
	}
	// A wider bounds leaves the cells to the right alone (default ASCII orbit,
	// age 30ms is still the resting glyph).
	buffer = render.NewBuffer(4, 1)
	canvas = render.NewCanvas(buffer)
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 0, W: 4, H: 1}, &state, spinNow, SpinnerStyle{})
	if got := bufferRow(buffer, 0); got != "|" {
		t.Fatalf("wide bounds glyph = %q; want | and no fill", got)
	}
	// Degenerate geometry and a nil canvas draw nothing.
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 0, W: 0, H: 1}, &state, spinNow, SpinnerStyle{})
	ctx.Spinner(nil, render.Rect{X: 0, Y: 0, W: 1, H: 1}, &state, spinNow, SpinnerStyle{})
	ctx.Spinner(canvas, render.Rect{X: 9, Y: 0, W: 1, H: 1}, &state, spinNow, SpinnerStyle{})
}

func TestSpinnerNilStateDrawsRestingGlyph(t *testing.T) {
	buffer := render.NewBuffer(2, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	ctx.Spinner(canvas, render.Rect{X: 0, Y: 0, W: 1, H: 1}, nil, spinNow, SpinnerStyle{})
	if got := bufferRow(buffer, 0); got != "|" {
		t.Fatalf("nil state glyph = %q; want |", got)
	}
}

func BenchmarkSpinnerDraw(b *testing.B) {
	// Display-only widget: no region, so the loop is pure painting anyway.
	buffer := render.NewBuffer(80, 1)
	canvas := render.NewCanvas(buffer)
	var ctx Context
	now := spinNow
	state := SpinnerState{Started: now.Add(-137 * time.Millisecond)}
	style := SpinnerStyle{Frames: SpinnerBraille}
	b.ReportAllocs()
	for range b.N {
		ctx.Spinner(canvas, render.Rect{X: 0, Y: 0, W: 1, H: 1}, &state, now, style)
	}
}
