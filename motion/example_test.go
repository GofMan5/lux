package motion_test

import (
	"fmt"
	"time"

	"github.com/lux-tui/lux/motion"
)

// ExampleTween samples an eased tween against absolute time: before it
// starts, halfway through, and after it ends. Late frames read the value
// for "now", so the printed samples are what a slow machine would actually
// paint — no intermediate positions are replayed. The done flag flips only
// at the full duration; a tween that has not started reports its From value.
func ExampleTween() {
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	tween := motion.Tween{
		From:     0,
		To:       100,
		Start:    start,
		Duration: 2 * time.Second,
		Ease:     motion.InOutCubic,
	}
	for _, at := range []time.Duration{
		-1 * time.Second,
		1 * time.Second,
		3 * time.Second,
	} {
		value, done := tween.Value(start.Add(at))
		fmt.Printf("t=%dms value=%.1f done=%t\n", at/time.Millisecond, value, done)
	}
	// Output:
	// t=-1000ms value=0.0 done=false
	// t=1000ms value=50.0 done=false
	// t=3000ms value=100.0 done=true
}

// ExampleSpring samples a critically damped spring from rest. Response is
// roughly the settle time: at one response the spring is within a few
// percent of its target, and it never overshoots. Both returns come from
// the closed form, so sampling allocates nothing and cannot drift.
func ExampleSpring() {
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	spring := motion.Spring{
		From:     0,
		To:       100,
		Start:    start,
		Response: 300 * time.Millisecond,
	}
	for _, step := range []time.Duration{
		0,
		150 * time.Millisecond,
		300 * time.Millisecond,
		600 * time.Millisecond,
	} {
		position, velocity := spring.Value(start.Add(step))
		fmt.Printf("position=%.2f velocity=%.2f\n", position, velocity)
	}
	// Output:
	// position=0.00 velocity=0.00
	// position=66.91 velocity=353.58
	// position=94.37 velocity=70.90
	// position=99.90 velocity=1.43
}
