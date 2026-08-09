package motion

import (
	"math"
	"testing"
	"time"
)

func TestTweenSkipsLateFrames(t *testing.T) {
	start := time.Now()
	tween := Tween{From: 10, To: 20, Start: start, Duration: time.Second, Ease: InOutCubic}
	if value, done := tween.Value(start.Add(2 * time.Second)); value != 20 || !done {
		t.Fatalf("late tween = %v, %v", value, done)
	}
	if value, _ := tween.Value(start.Add(500 * time.Millisecond)); math.Abs(value-15) > 1e-9 {
		t.Fatalf("midpoint = %v", value)
	}
}

func TestSpringConverges(t *testing.T) {
	start := time.Now()
	spring := Spring{From: 0, To: 1, Start: start, Response: 300 * time.Millisecond}
	position, velocity := spring.Value(start.Add(2 * time.Second))
	if math.Abs(position-1) > .001 || math.Abs(velocity) > .01 {
		t.Fatalf("spring did not converge: position=%v velocity=%v", position, velocity)
	}
}
