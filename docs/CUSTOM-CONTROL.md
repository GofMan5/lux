# Your own control in an evening

Snapshot: **2026-10-08**. This is the recipe for building one LUX control from
scratch in an evening: state, style, `Apply`, region registration, and the two
one-line hooks that wire it into an application. The worked example — a
stepper `[− 42 +]` — is not a copy-paste sketch: the exact code below lives in
[ui/stepper_recipe_test.go](../ui/stepper_recipe_test.go) and runs in `go test ./ui/`, so this document
cannot rot silently.

LUX gives every control these things for free; you owe it the rest:

| The framework owns | You own |
|---|---|
| Hit-testing against committed geometry | `Apply`: the one pure function that mutates state |
| Focus ring, Tab/BackTab order, hover, press capture | Draw: register the region, then paint it |
| Routing: arrows/wheel → `Scroll`, Enter/Space → `Activate`, typing → `Input` | Layout truth: hits and glyphs use the same rectangle |
| EGC-safe clipping, out-of-view guards | Honesty: what the control does *not* do |
| Release-commit geometry (a dropped frame cannot steal clicks) | A zero-value-ready state struct |

## The five pieces

### 1. State: zero value is ready

```go
// StepperState is the persistent value of one stepper control. The zero
// value is 0 and unbounded; Min < Max bounds the value to that range.
type StepperState struct {
    Value int
    Min   int
    Max   int
    Step  int
}
```

No constructor, no `Init()`. `Min == Max` (the zero value) means unbounded;
`Max > Min` clamps. The application owns everything else — the label, the
units, the meaning. The widget never stores what it cannot derive from an
event.

### 2. Style: zero values inherit Normal

```go
// StepperStyle defines stepper visual states. Zero values inherit Normal.
type StepperStyle struct {
    Normal        render.Style
    Focused       render.Style
    Hovered       render.Style
    Pressed       render.Style
    DisabledStyle render.Style
    Disabled      bool
}
```

### 3. Apply: the only place state changes

`Apply` is a pure transition — no regions, no canvas, no clock. The router
hands you an `Action` with region-local `X`/`Y` and the target's `Bounds`;
keyboard tests set `Bounds` themselves.

```go
// Apply mutates the state for one routed stepper action and reports whether
// the value changed and whether a step key or half was used. The layout
// width comes from Action.Bounds: cell 0 is the minus half, the last cell
// is the plus half, and a press between them does nothing — seeking by
// drag is not claimed. Arrows, pages, Home/End, and the wheel step the
// value; a mouse press on a half steps immediately (a stepper is not a
// commit-style control, so the release is inert); key releases are inert;
// Enter/Space are inert.
func (s *StepperState) Apply(action Action) (changed, activated bool) {
    if s == nil {
        return false, false
    }
    if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
        return false, false
    }
    if action.Kind != Scroll {
        return false, false
    }
    before := s.Value
    switch action.Event.Kind {
    case event.Key:
        switch action.Event.Key.Code {
        case event.KeyLeft, event.KeyDown:
            s.Value -= s.step()
            activated = true
        case event.KeyRight, event.KeyUp:
            s.Value += s.step()
            activated = true
        case event.KeyPageUp:
            s.Value -= 10 * s.step()
            activated = true
        case event.KeyPageDown:
            s.Value += 10 * s.step()
            activated = true
        case event.KeyHome:
            if s.Max > s.Min {
                s.Value = s.Min
                activated = true
            }
        case event.KeyEnd:
            if s.Max > s.Min {
                s.Value = s.Max
                activated = true
            }
        }
    case event.Mouse:
        switch action.Event.Mouse.Action {
        case event.MouseWheel:
            switch action.Event.Mouse.Button {
            case event.MouseWheelUp, event.MouseWheelLeft:
                s.Value += s.step()
                activated = true
            case event.MouseWheelDown, event.MouseWheelRight:
                s.Value -= s.step()
                activated = true
            }
        case event.MousePress:
            width := action.Bounds.W
            if action.X == 0 && width > 1 {
                s.Value -= s.step()
                activated = true
            } else if action.X == width-1 && width > 1 {
                s.Value += s.step()
                activated = true
            }
        }
    }
    s.clamp()
    return s.Value != before, activated
}
```

Three contract rules worth noticing:

- **Key releases are inert.** Terminals report key-up as synthetic events;
  the checkbox is the canonical example (see [ui/checkbox.go](../ui/checkbox.go)).
- **A mouse press on a non-activatable region arrives as `Scroll`** with
  local coordinates — that is how a `Scrollable` region learns *where* it
  was pressed, and it is how the two halves of this stepper work with a
  single ID.
- **`changed` and `activated` answer different questions.** `changed` feeds
  "repaint me"; `activated` feeds "the user did the thing" (a report line,
  a sound, a follow-up command).

### 4. Draw: register, then paint

Region registration happens in paint order inside `View`. Register the
geometry first — even when the region is clipped out — because hits route
by ID, then paint through the clip.

```go
// Stepper registers and draws one stepper: a minus cell, the centered
// value, a plus cell. The same width math drives drawing and pointer hits.
func (c *Context) Stepper(id ID, canvas *render.Canvas, bounds render.Rect, state *StepperState, style StepperStyle) {
    if canvas == nil || state == nil || bounds.Empty() {
        return
    }
    flags := Focusable | Scrollable
    if style.Disabled {
        flags |= Disabled
    }
    c.Region(id, canvas, bounds, flags)
    if bounds.Intersect(canvas.Bounds()).Empty() {
        return
    }
    visual := style.Normal
    switch {
    case style.Disabled:
        visual = fallbackStyle(style.DisabledStyle, visual)
    case c.Pressed(id):
        visual = fallbackStyle(style.Pressed, visual)
    case c.Focused(id):
        visual = fallbackStyle(style.Focused, visual)
    case c.Hovered(id):
        visual = fallbackStyle(style.Hovered, visual)
    }
    canvas = canvas.Clip(bounds)
    canvas.Fill(render.Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1}, " ", visual)
    if bounds.W >= 2 {
        canvas.Text(bounds.X, bounds.Y, "-", visual)
        canvas.Text(bounds.X+bounds.W-1, bounds.Y, "+", visual)
    }
    if bounds.W >= 4 {
        value := strconv.Itoa(state.Value)
        x := bounds.X + (bounds.W-len(value))/2
        if x > bounds.X {
            canvas.Text(x, bounds.Y, value, visual)
        }
    }
}
```

The `Bounds` that `Apply` reads and the rectangle painted here are the same
object — that is the whole trick. One ID, one rectangle, two consumers.

### 5. Wiring: two lines in the app

```go
func (app *app) Update(next event.Event) lux.Command {
    if action, handled := app.UI.Handle(next); handled {
        switch action.Target {
        case "count":
            app.count.Apply(action)
        }
        return nil
    }
    return nil
}

func (app *app) View(frame *lux.Frame) {
    app.UI.Begin(frame)
    defer app.UI.End(frame)
    app.UI.Stepper("count", frame.Canvas, render.Rect{X: 4, Y: 1, W: 9, H: 1}, &app.count, StepperStyle{})
}
```

The full two-page version of this loop, with a table, tree, tabs, and
popups sharing it, lives in [examples/widgets/main.go](../examples/widgets/main.go).

## The testing ladder

Every control in this repo climbs the same three rungs; the stepper's are
in [ui/stepper_recipe_test.go](../ui/stepper_recipe_test.go):

1. **`Apply` table tests.** Pure functions, no regions: every key, wheel
   direction, press half, release, nil state, clamping. This is where
   correctness lives.
2. **Paint test.** Build a `render.NewBuffer`, a canvas, a `*lux.Frame`,
   `ctx.Begin(frame)`, draw, `ctx.End(frame)`, then read cells back and
   assert the exact row. Duplicate IDs panic during build — a free
   invariant.
3. **Routing test.** Commit a snapshot with the region, drive
   `ctx.Handle(...)` with real `event.Event`s, and assert the routed
   `Action` (target, kind, local `X`), then feed it to `Apply`. This rung
   is what catches "the router sends a press as `Scroll`, not `Activate`"
   mistakes.

Interactive end-to-end tests (real backend, mouse, repaint loop) come
fourth for app-level flows; see [examples/widgets/main_test.go](../examples/widgets/main_test.go).

## Pitfalls

- Registering the same ID twice in one frame panics on purpose; it means
  two controls claim one identity.
- Drawing *after* `ctx.End` registers nothing: `End` snapshots `building`.
- Do not mutate state in `View`. Painting is idempotent; if the value
  changes between two paints with no event in between, the control is
  lying about who owns state.
- `action.X` is region-local even when the region is clipped or inside a
  scrolled container — never add the region origin back.
- A click on an `Activatable` region arrives as `Activate` **on the
  release** (the router requires release-inside). A press on a
  `Scrollable`-only region arrives as `Scroll` **on the press**. Pick the
  flag by which moment the control should react.
- `PageUp`/`PageDown` are yours to define; lists use "visible rows",
  a stepper uses ×10. Say which in the doc comment.

## What the stepper does not claim

- No drag-seek: dragging across the halves does not track the pointer
  (that is the gauge's contract, [ui/gauge.go](../ui/gauge.go)).
- No typing: digits do not edit the value (that is `Input` territory).
- No external spin buttons, no float mode, no repeat-on-hold.

Not-claimed lists are load-bearing: they are the difference between a
control with edges and a control that surprises its author in a year.

## Where to read more

- [ui/context.go](../ui/context.go) — region commit, routing, focus, capture.
- [ui/tree.go](../ui/tree.go) — a data-heavy control: `rowAt` indirection,
  expansion state, viewport math.
- [ui/checkbox.go](../ui/checkbox.go) — the smallest activatable control.
- [docs/INTERACTION.md](INTERACTION.md) — the full interaction contract:
  capture, release-commit, modal trap, shortcuts.
