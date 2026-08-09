// Package motion contains allocation-free, absolute-time animation primitives.
package motion

import (
	"math"
	"time"
)

// Ease maps normalized time to normalized progress.
type Ease func(float64) float64

func Linear(t float64) float64 { return clamp01(t) }

func InOutCubic(t float64) float64 {
	t = clamp01(t)
	if t < .5 {
		return 4 * t * t * t
	}
	x := -2*t + 2
	return 1 - x*x*x/2
}

func OutCubic(t float64) float64 {
	x := 1 - clamp01(t)
	return 1 - x*x*x
}

func OutBack(t float64) float64 {
	t = clamp01(t)
	const c1 = 1.70158
	const c3 = c1 + 1
	x := t - 1
	return 1 + c3*x*x*x + c1*x*x
}

// Tween samples a value from From to To against the monotonic component of time.Time.
type Tween struct {
	From     float64
	To       float64
	Start    time.Time
	Duration time.Duration
	Ease     Ease
}

// Value returns the current value and whether the tween has reached its end.
func (t Tween) Value(now time.Time) (float64, bool) {
	if t.Duration <= 0 || !now.Before(t.Start.Add(t.Duration)) {
		return t.To, true
	}
	if now.Before(t.Start) {
		return t.From, false
	}
	progress := float64(now.Sub(t.Start)) / float64(t.Duration)
	ease := t.Ease
	if ease == nil {
		ease = Linear
	}
	return t.From + (t.To-t.From)*ease(progress), false
}

// Spring is an analytic, critically damped spring. It is sampled by absolute
// time, so delayed frames skip stale visual states instead of replaying them.
type Spring struct {
	From     float64
	To       float64
	Velocity float64
	Start    time.Time
	Response time.Duration
}

// Value returns position and velocity. Response is approximately the time to
// settle; non-positive response snaps to the target.
func (s Spring) Value(now time.Time) (position, velocity float64) {
	if s.Response <= 0 || !now.After(s.Start) {
		if s.Response <= 0 {
			return s.To, 0
		}
		return s.From, s.Velocity
	}
	t := now.Sub(s.Start).Seconds()
	omega := 4.6 / s.Response.Seconds()
	delta := s.From - s.To
	c := s.Velocity + omega*delta
	exp := math.Exp(-omega * t)
	return s.To + (delta+c*t)*exp, (s.Velocity - omega*c*t) * exp
}

func clamp01(value float64) float64 { return min(max(value, 0), 1) }
