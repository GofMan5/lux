package ui

import (
	"time"

	"github.com/GofMan5/lux/render"
)

// SpinnerStyle picks the glyph orbit and pacing. Frames are single-cell
// glyphs; the first frame is the resting glyph before any time passes. A
// zero Style resolves through the context theme's Accent.
type SpinnerStyle struct {
	Frames        []string
	FrameDuration time.Duration
	Style         render.Style
}

// SpinnerASCII is the default orbit: "|/-\ Locale-proof: pure ASCII.
var SpinnerASCII = []string{"|", "/", "-", "\\"}

// SpinnerBraille is a ten-phase braille orbit.
var SpinnerBraille = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerArc is a four-phase rotating disc.
var SpinnerArc = []string{"◐", "◓", "◑", "◒"}

// SpinnerState is when the spinner started. The zero value is a spinner that
// has not started: Age reports 0 and the first frame paints, so a zero-value
// SpinnerState is ready to draw and stays on its resting glyph until the app
// sets Started.
type SpinnerState struct {
	Started time.Time
}

// Age returns the time elapsed since the spinner started; a zero Started
// means not started, so Age is 0 rather than 56 years.
func (s *SpinnerState) Age(now time.Time) time.Duration {
	if s == nil || s.Started.IsZero() {
		return 0
	}
	return max(now.Sub(s.Started), 0)
}

// spinnerFrame picks the frame index for an elapsed age: the floor of
// age/duration, modulo the frame count. The phase is a pure function of
// absolute time, so the spin rate is exact at any frame rate — a 10 FPS and
// a 240 FPS presenter draw the same glyph at the same wall-clock instant,
// which is the absolute-time contract the motion layer holds.
func spinnerFrame(age, duration time.Duration, frames int) int {
	if frames <= 0 || duration <= 0 {
		return 0
	}
	return int(age/duration) % frames
}

// Spinner paints one spinning glyph. It is display-only by design — no
// region, no routing, no focus; "busy" is a state the app owns, the spinner
// only tells time. The app passes now (usually time.Time Now) so the drawn
// frame is deterministic for tests and replayable for the interaction lab;
// drawing the same (state, now) always paints the same glyph. A zero Style
// resolves through the context theme's Accent. The glyph draws at bounds'
// origin and clips to bounds.W, so a wider bounds leaves the cells to the
// right for the app to use. Not claimed: multi-cell frames, an
// indeterminate marquee, frame scheduling (the runtime's pacer owns that).
func (c *Context) Spinner(canvas *render.Canvas, bounds render.Rect, state *SpinnerState, now time.Time, style SpinnerStyle) {
	if canvas == nil || bounds.Empty() {
		return
	}
	if bounds.Intersect(canvas.DrawableRect()).Empty() {
		return
	}
	frames := style.Frames
	if len(frames) == 0 {
		frames = SpinnerASCII
	}
	duration := style.FrameDuration
	if duration <= 0 {
		duration = 100 * time.Millisecond
	}
	index := spinnerFrame(state.Age(now), duration, len(frames))
	glyph := clipText(frames[index], bounds.W, canvas.WidthPolicy())
	if glyph == "" {
		glyph = frames[0]
	}
	visual := themedStyle(style.Style, c.Theme().Accent, style.Style)
	canvas = canvas.Clip(bounds)
	canvas.Text(bounds.X, bounds.Y, glyph, visual)
}
