# Animation

Snapshot: **2026-10-10**. This is the motion loop in one page: how a LUX
program animates, why its timing stays correct when frames are late, and the
two primitives you reach for. Every snippet below is lifted from
[examples/02-animation](../examples/02-animation/main.go), which builds with
`go build ./examples/02-animation/` — so this document cannot rot silently.

## The contract: animation is a function of time

A LUX animation is not a coroutine that steps on a timer. It is a pure value
sampled against the clock the runtime already owns:

```go
progress, done := tween.Value(frame.Now)
```

`frame.Now` is the runtime's monotonic clock, handed to `View` on every
frame. The consequences are what make the loop robust:

- **A late frame skips ahead, it does not fall behind.** `Value` maps elapsed
  time to progress. If the machine hiccups for 300 ms, the next sample shows
  the value for *now* — the 25 stale intermediate positions are never
  drawn. There is no "catch-up replay" jitter, because there is nothing to
  replay.
- **Animation never races input.** `Update` handles events; `View` samples
  motion. Nothing in the model is advanced by a timer callback that might
  interleave with a keystroke mid-handler.
- **The runtime paces, the app asks.** `frame.RequestFrame()` in `View` arms
  the runtime's adaptive pacer (60→30→20→10 FPS as work demands). An idle
  app that stops requesting frames disarms its timer entirely and burns no
  CPU.

## The two primitives

### Tween: from A to B with a curve

```go
motion.Tween{
    From:     0,
    To:       1,
    Start:    now,
    Duration: 1200 * time.Millisecond,
    Ease:     motion.InOutCubic,
}
```

`Value(now)` returns the eased value and whether the tween is finished.
`Ease` is optional — the default is linear; the library ships `Linear`,
`InOutCubic`, `OutCubic`, and `OutBack` (the small overshoot).

A tween does not loop by itself, and that is deliberate: looping is a policy
your `View` owns. The example restarts one when it completes:

```go
progress, done := a.loop.Value(now)
if done {
    a.loop = motion.Tween{From: 0, To: 1, Start: now,
        Duration: tweenDuration, Ease: motion.InOutCubic}
    progress = 0
}
```

### Spring: physical arrival

```go
motion.Spring{
    From:     0,        // starting position
    To:       1,        // target
    Velocity: 0,        // initial velocity, e.g. handed over on retarget
    Start:    now,
    Response: 900 * time.Millisecond, // ≈ time to settle
}
```

`Spring` is an analytic critically damped spring: it glides to the target
with no overshoot, and `Response` is roughly how long settling takes.
Because it is a closed form, sampling it is allocation-free and exact —
there is no per-tick integration to drift.

Retargeting is replacement, not mutation. You sample the current position
and velocity, then build a new spring from them:

```go
position, velocity := a.spring.Value(next.At)
a.spring = motion.Spring{
    From: position, To: newTarget, Velocity: velocity,
    Start: next.At, Response: springResponse,
}
```

That is the whole "interrupting animation" story: the handover keeps the
momentum, the new spring absorbs it, and no stale timeline survives.

## Wiring it into the frame

The `View` of [examples/02-animation](../examples/02-animation/main.go)
draws a tweened gauge and a spring-driven marker, then ends with the one
call that keeps the system honest:

```go
// Keep frames coming while anything moves: the runtime arms its
// adaptive pacer from this call, and idle apps never wake.
frame.RequestFrame()
```

When the last motion settles, the app simply stops asking. The next frame
arrives only when input, resize, or a new animation warrants one — which is
also why an idle LUX program measures 0% CPU instead of polling a clock.

## Choosing between them

- **Tween** when you know the shape: durations, curves, two knowns ends.
  Content appearances, progress fills, slide-ins.
- **Spring** when the endpoint moves: user-driven positions, retargetable
  values, anything that must interrupt mid-flight and keep its momentum.
- **Neither** when reduced motion is set: `frame.ReducedMotion` is available
  on every frame; respect it by snapping to the end value.

## Where to go next

- [QUICKSTART.md](QUICKSTART.md) — the base loop this builds on.
- [ARCHITECTURE.md](ARCHITECTURE.md) — what the pacer does between
  `RequestFrame` and the next `View`.
- [PERFORMANCE.md](PERFORMANCE.md) — how the motion loop is measured.
