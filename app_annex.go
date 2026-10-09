package lux

import (
	"github.com/lux-tui/lux/internal/terminal"
)

// ProgressState selects the shape a terminal draws for taskbar progress.
// It maps one to one onto the OSC 9;4 states: None removes the progress
// indicator, Indeterminate and Paused draw a spinner, Normal draws a
// determinate bar at the reported percent, and Error keeps the bar but
// colors it as failed. A state outside the constants is a caller bug and
// encodes to no bytes at all.
type ProgressState int

const (
	// ProgressNone removes the taskbar progress indicator. The percent
	// argument is ignored; the runtime Run constructs also writes this
	// state on every exit path, so a quitting program never leaves a
	// progress bar behind.
	ProgressNone ProgressState = 0
	// ProgressIndeterminate draws an active spinner: work is ongoing and
	// its extent is unknown.
	ProgressIndeterminate ProgressState = 1
	// ProgressNormal draws a determinate bar at percent.
	ProgressNormal ProgressState = 2
	// ProgressError keeps the bar at percent but marks it failed.
	ProgressError ProgressState = 3
	// ProgressPaused keeps the indicator without animation: work stopped
	// but is resumable.
	ProgressPaused ProgressState = 4
)

// WithProgress opts into OSC 9;4 taskbar progress reporting, honored by
// Windows Terminal, ConEmu, and iTerm2. Frame.SetProgress then reports
// progress alongside View; the runtime the option arms also writes the
// no-progress reset on every exit path, so a quit never leaves a bar on
// the taskbar. The default is off because the sequence changes state
// outside the terminal window — the user's taskbar — and a terminal that
// does not implement OSC 9;4 would be reporting progress to no one.
// Like WithTitle, the option applies to the backend Run constructs for
// itself; a custom backend owns its own progress policy.
func WithProgress(enabled bool) Option {
	return func(config *config) error { config.progress = enabled; return nil }
}

// WithNotifications opts into OSC 9;777 desktop notifications, honored by
// foot, wezterm, and the rxvt lineage. Frame.Notify then delivers toasts
// alongside View; the title and body are sanitized — control runes
// stripped, semicolons dropped from the title — because they ride an OSC
// string, and a BEL or ESC inside one would end it early and leak the
// rest as terminal commands. The default is off: a toast is the most
// intrusive thing a terminal program can emit, and terminals without
// OSC 9;777 (Windows Terminal, kitty) would see nothing. A custom
// backend owns its own notification policy.
func WithNotifications(enabled bool) Option {
	return func(config *config) error { config.notifications = enabled; return nil }
}

// WithClipboard opts into OSC 52 clipboard writes, honored by kitty,
// foot, wezterm, tmux (with set-clipboard on), and Windows Terminal. The
// consent gate exists because writing the user's clipboard from a
// terminal program is a privileged action — many of the implementing
// terminals confirm it, and tmux keeps it off by default. Only the write
// direction is ever emitted: OSC 52 also defines a read that makes the
// terminal type the user's clipboard into the program's stdin, and a
// program that can read a clipboard through a pipe is a keylogger with
// extra steps, so the runtime never builds that sequence. Writes carry
// at most 32 KiB of text; larger requests are dropped rather than
// truncated. A custom backend owns its own clipboard policy.
func WithClipboard(enabled bool) Option {
	return func(config *config) error { config.clipboard = enabled; return nil }
}

// SetProgress reports taskbar progress for this frame. The bytes ride the
// frame's annex — written after the frame is presented, never interleaved
// with it — and a dropped frame drops its report with it, so a model
// should call SetProgress on every View the way it draws its progress
// bar: state, not event. Call it only under WithProgress; otherwise it
// queues nothing. percent is clamped into 0..100 and ignored for the
// None, Indeterminate, and Paused states.
func (f *Frame) SetProgress(state ProgressState, percent int) {
	if f == nil || !f.progress {
		return
	}
	if state < ProgressNone || state > ProgressPaused {
		return
	}
	f.annex = append(f.annex, terminal.ProgressAnnex(int(state), percent)...)
}

// Notify delivers a desktop notification for this frame, riding the
// frame's annex the way SetProgress does. A notification is an event,
// not a state: a model sends one from the View that follows the thing
// worth announcing, and the toast appears when that frame does. Call it
// only under WithNotifications; otherwise it queues nothing. The title
// carries no semicolons (they separate OSC 9;777's fields) and neither
// field carries control runes; both sanitized away before the bytes are
// built.
func (f *Frame) Notify(title, body string) {
	if f == nil || !f.notifications {
		return
	}
	f.annex = append(f.annex, terminal.NotifyAnnex(title, body)...)
}

// CopyToClipboard writes text to the system clipboard for this frame,
// riding the frame's annex. Call it only under WithClipboard; otherwise
// it queues nothing. Text above 32 KiB is dropped rather than truncated —
// half a clipboard is worse than none. The ui package's Clipboard port
// pairs with this method: a model stores the pending text when a widget
// copies and flushes it through CopyToClipboard in the next View, which
// keeps every write on the presentation path.
func (f *Frame) CopyToClipboard(text string) {
	if f == nil || !f.clipboard {
		return
	}
	f.annex = append(f.annex, terminal.ClipboardAnnex(text)...)
}
