package terminal

import (
	"context"
	"encoding/base64"
	"strings"
)

// Taskbar progress, desktop notifications, and clipboard writes share one
// property the frame encoder cannot express: they are control sidebands,
// not screen content. They ride an annex — bytes the app layer queues
// during View and the writer hands over through WriteAnnex right after the
// frame they belong to — so they serialize with frame content instead of
// racing it on the output file. None of the three protocols defines a
// detection query, so like OSC 8 hyperlinks the application opts in and
// silence cannot be separated from support.

// progressClear is the OSC 9;4 "no progress" reset. Every leave sequence
// writes it when the application opted into progress reporting: a taskbar
// progress state belongs to the running program, and a terminal that
// showed one during the run would keep showing it after exit. The BEL
// terminator is the form Windows Terminal and ConEmu — the two lineages
// that define OSC 9;4 — document.
const progressClear = "\x1b]9;4;0\a"

// maxClipboardAnnex bounds the text a clipboard write carries. Every
// implementation that honors OSC 52 at all accepts payloads far beyond
// this, but multi-megabyte "clipboard writes" are how a runaway program
// wedges a terminal's parser; 32 KiB covers interactive copy/paste and
// refuses abuse by returning no bytes at all.
const maxClipboardAnnex = 32 << 10

// progressStateNone..progressStatePaused are the OSC 9;4 states: 0 removes
// the taskbar progress, 1 and 4 are indeterminate (1 spinning, 4 paused),
// 2 is a determinate bar, 3 an error state. Anything outside 0..4 encodes
// to no bytes: an unknown state is a caller bug, and emitting a malformed
// sequence would be worse than emitting nothing.
const (
	progressStateNone          = 0
	progressStateIndeterminate = 1
	progressStateNormal        = 2
	progressStateError         = 3
	progressStatePaused        = 4
)

// ProgressAnnex builds the OSC 9;4 taskbar progress sequence. percent is
// clamped into 0..100 and ignored for the none state, which never carries
// one; every other state includes it, because Windows Terminal ignores the
// value it does not need while ConEmu expects the field. A state outside
// 0..4 returns nil — the caller's annex is simply empty.
func ProgressAnnex(state, percent int) []byte {
	if state < progressStateNone || state > progressStatePaused {
		return nil
	}
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}
	var builder strings.Builder
	builder.WriteString("\x1b]9;4;")
	builder.WriteString(intDecimal(state))
	if state != progressStateNone {
		builder.WriteByte(';')
		builder.WriteString(intDecimal(percent))
	}
	builder.WriteByte('\a')
	return []byte(builder.String())
}

// NotifyAnnex builds the OSC 9;777 desktop notification sequence. The
// title and body are sanitized like a window title — C0 controls, DEL,
// and one-byte C1 controls stripped, because a BEL or ESC would end the
// OSC string early and leak the rest as commands — and the title also
// drops semicolons, which OSC 9;777 uses as its field separator: a
// semicolon in the title would slide the body into the title slot. A
// notification whose title and body are both empty after sanitization
// returns nil; a blank toast is noise no terminal should display.
func NotifyAnnex(title, body string) []byte {
	title = sanitizeTitle(title)
	title = strings.ReplaceAll(title, ";", "")
	body = sanitizeTitle(body)
	if title == "" && body == "" {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("\x1b]777;notify;")
	builder.WriteString(title)
	builder.WriteByte(';')
	builder.WriteString(body)
	builder.WriteString("\x1b\\")
	return []byte(builder.String())
}

// ClipboardAnnex builds the OSC 52 clipboard-write sequence. The text is
// base64-encoded — the encoding's alphabet cannot carry a BEL, ESC, or
// semicolon, so no sanitization is needed — and the c selection
// designates the system clipboard, the one every OSC 52 implementation
// shares; primary selections are terminal-specific and stay out. Text
// above maxClipboardAnnex returns nil rather than a truncated write:
// half a clipboard is worse than none. The read direction of OSC 52 — a
// program asking the terminal to type the user's clipboard into its
// stdin — is deliberately absent; this package never builds it.
func ClipboardAnnex(text string) []byte {
	if len(text) > maxClipboardAnnex {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("\x1b]52;c;")
	builder.WriteString(base64.StdEncoding.EncodeToString([]byte(text)))
	builder.WriteString("\x1b\\")
	return []byte(builder.String())
}

// intDecimal renders a small non-negative integer without fmt's
// reflection machinery; progress states and percents are the only callers.
func intDecimal(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [8]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

// WriteAnnex writes control bytes on the backend's output, serialized
// against frame presentation by the same one-slot semaphore Present
// acquires. The runtime calls it immediately after the frame whose View
// queued the bytes, on the same goroutine, so annex bytes never interleave
// with frame bytes on the wire. Plain and redirected streams return
// without writing — they never received the enter sequences either — and
// so do suspended and closed backends: an annex is framed content, not
// an obligation that survives the frame being dropped. An error surfaces
// exactly like a presentation error, because a half-written annex is the
// same kind of unknown terminal state.
func (b *Backend) WriteAnnex(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := b.writeSlotChan()
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slot }()
	b.mu.Lock()
	if !b.opened || b.plain || b.suspended || b.writer == nil {
		b.mu.Unlock()
		return nil
	}
	writer := b.writer
	b.mu.Unlock()
	return writeAllContext(ctx, writer, data)
}
