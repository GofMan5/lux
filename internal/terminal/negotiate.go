package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lux-tui/lux/capability"
)

// defaultProbeTimeout bounds capability negotiation. A terminal that answers
// the probe usually does so in single-digit milliseconds; slow links spend the
// rest as grace. A report that arrives after the deadline is not filtered:
// suppressing report-shaped sequences then would also eat pasted input that
// merely looks like one. A late report instead reaches the parser, which
// knows neither the DA1 'c' nor the DECRPM '$y' final byte, so it surfaces as
// a single unknown-key event — the honest cost on links slower than the
// budget, where raising LUX_TUI_PROBE_TIMEOUT is the real remedy. Keystrokes
// typed during the probe are still replayed, so the worst case is a startup
// delay plus that one cosmetic event — never a stuck Open.
const defaultProbeTimeout = 150 * time.Millisecond

// probeRequest is written once. The primary device attributes report proves
// the input path actually echoes device reports back; the DECRPM queries ask
// whether DEC private mode 6 (DECOM) and mode 2026 (synchronized output) are
// recognized. Mode 6 is a proxy for DECSTBM scroll regions: DECSTBM is a
// control sequence, not a mode, so DECRQM cannot ask about it directly, and
// every terminal family pairs DECSTBM with DECOM — a terminal that rejects
// DECOM lacks the private-mode machinery scroll regions rely on.
const probeRequest = "\x1b[c\x1b[?6$p\x1b[?2026$p"

// maxProbeStream bounds how many bytes a probe inspects. A real terminal sends
// three short reports; anything spraying kilobytes at an unestablished
// connection is hostile, and the probe stops looking instead of buffering.
// Bytes already read are never dropped: everything unclassified is replayed
// as leftover, so the cap can only shorten the inspection, never eat input.
const maxProbeStream = 4096

// probeFeature is what a device report proved about one capability.
type probeFeature uint8

const (
	// probeUnknown means no report answered: the default stands.
	probeUnknown probeFeature = iota
	// probeRejected means the terminal positively does not know the mode.
	probeRejected
	// probeSupported means the terminal recognized the mode in any state.
	probeSupported
)

// probeReply is the outcome of one negotiation round.
type probeReply struct {
	// answered is set by any primary device attributes report: the terminal
	// is live and understands query protocols at all.
	answered bool
	scroll   probeFeature
	sync     probeFeature
}

// negotiate probes a terminal and returns its report together with every byte
// the probe could not classify. It writes one request, then reads with the
// same deadline machinery as the main read loop, so a silent terminal costs
// exactly one bounded wait and a broken pipe returns immediately. It stops
// early once every question is answered; a terminal that answers DA1 but not
// DECRQM keeps the probe alive until the deadline, which startup pays once.
//
// A report whose tail is still missing when the deadline hits is returned
// unclassified with its head: the replayed bytes surface to the app as one
// escape key press followed by literal runes. That is the honest price of a
// bounded wait — one cosmetic burst on a slow link at startup, never lost
// input — and it beats the alternative of waiting on a half-delivered reply
// with no proof the tail will ever come.
func negotiate(ctx context.Context, input *os.File, writer io.Writer, timeout time.Duration) (probeReply, []byte) {
	if err := writeAllContext(ctx, writer, []byte(probeRequest)); err != nil {
		// A probe write that fails means the terminal is unwritable, and the
		// enterTerminal write right after will surface the real error.
		return probeReply{}, nil
	}
	deadline := time.Now().Add(timeout)
	var stream []byte
	buffer := make([]byte, 256)
	for {
		reply, leftover := parseProbeReply(stream)
		if reply.answered && reply.scroll != probeUnknown && reply.sync != probeUnknown {
			return reply, leftover
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return reply, leftover
		}
		count, err := readInput(ctx, input, buffer, remaining)
		if count > 0 {
			stream = append(stream, buffer[:count]...)
			if len(stream) >= maxProbeStream {
				// The cap bounds inspection, not replay: bytes already read
				// off the input are classified or returned as leftover,
				// never dropped, because discarding them could eat a
				// keystroke or split a sequence mid-rune.
				return parseProbeReply(stream)
			}
		}
		if err != nil {
			// EOF, a hard error, or a canceled context ends the probe; a
			// read timeout falls through, and the deadline check at the top
			// of the next round returns what arrived so far.
			if !errors.Is(err, errReadTimeout) || ctx.Err() != nil {
				return parseProbeReply(stream)
			}
		}
	}
}

// parseProbeReply scans a probe byte stream. Only reports the probe itself
// could have caused are consumed: a device attributes report ends in 'c', and
// a mode report is the two-parameter "Pd;Pm$y" form of DECRPM. Every other
// byte — keystrokes, unknown or truncated sequences — is returned as leftover
// so Open can replay it into the input parser without losing a single event.
func parseProbeReply(stream []byte) (probeReply, []byte) {
	var reply probeReply
	var leftover []byte
	for i := 0; i < len(stream); {
		rest := stream[i:]
		if len(rest) >= 3 && rest[0] == 0x1b && rest[1] == '[' && rest[2] == '?' {
			if end, ok := reportEnd(rest); ok {
				final := rest[end]
				body := rest[3:end]
				switch final {
				case 'c':
					// A device attributes body is digits and semicolons,
					// the shape real terminals send. Requiring that shape
					// keeps keystrokes that merely look like a report out
					// of the report path; a user deliberately typing digits
					// can still imitate one, which probing a shared stream
					// cannot rule out.
					if !isAttributeBody(body) {
						break
					}
					reply.answered = true
					i += end + 1
					continue
				case 'y':
					if mode, status, ok := parseStatusReport(body); ok {
						switch mode {
						case 6: // DECOM: the proxy for scroll regions (see probeRequest).
							reply.scroll = featureFromStatus(mode, status)
						case 2026:
							reply.sync = featureFromStatus(mode, status)
						}
						i += end + 1
						continue
					}
				}
			}
		}
		leftover = append(leftover, stream[i])
		i++
	}
	return reply, leftover
}

// reportEnd locates the final byte of a report candidate, the first byte in
// the CSI final range. DECRPM's '$' introducer sits below that range, so the
// scan naturally lands on 'y'. The window must stay above real report sizes —
// Windows Terminal answers DA1 with a 36-39 byte report
// (?61;4;6;7;14;21;22;23;24;28;32;42;52c), so a shorter cap would reject a
// complete reply as leftover, burn the whole probe timeout, and leak the
// reply as keystrokes. Beyond that it is deliberately one byte stricter than
// the parser's maxSequence: a report that long is pathological, and letting
// it stay leftover surfaces it as one unknown key instead of classifying a
// shape that long. The body validators and maxProbeStream remain the real
// guards.
func reportEnd(rest []byte) (int, bool) {
	for i := 3; i < len(rest) && i < maxSequence; i++ {
		if rest[i] >= 0x40 && rest[i] <= 0x7e {
			return i, true
		}
	}
	return 0, false
}

// isAttributeBody reports whether a device attributes body is digits and
// semicolons — the shape a real terminal sends. Anything else a human could
// have typed stays in leftover instead of forging a report.
func isAttributeBody(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	for _, b := range body {
		if (b < '0' || b > '9') && b != ';' {
			return false
		}
	}
	return true
}

// parseStatusReport reads the "Pd;Pm$" body of a mode report.
func parseStatusReport(body []byte) (mode, status int, ok bool) {
	if len(body) < 4 || body[len(body)-1] != '$' {
		return 0, 0, false
	}
	semicolon := -1
	for i, b := range body[:len(body)-1] {
		switch {
		case b == ';':
			if semicolon >= 0 {
				return 0, 0, false
			}
			semicolon = i
		case b < '0' || b > '9':
			return 0, 0, false
		}
	}
	if semicolon <= 0 {
		return 0, 0, false
	}
	mode, err := strconv.Atoi(string(body[:semicolon]))
	if err != nil {
		return 0, 0, false
	}
	status, err = strconv.Atoi(string(body[semicolon+1 : len(body)-1]))
	if err != nil {
		return 0, 0, false
	}
	return mode, status, true
}

// featureFromStatus maps a DECRPM status to a feature verdict. Status 0 means
// the mode is not recognized. Status 4 (permanently reset) is mode-dependent:
// for synchronized output it is as good as unsupported — the set-mode wrapper
// would be a permanent no-op, so the frames would stream unsynchronized anyway —
// while for the DECOM proxy it changes nothing, because DECSTBM sequences work
// with origin mode permanently reset and this backend never sets DECOM itself.
// Status 1-3 mean the mode exists in some state; the backend turns modes on
// explicitly when it uses them, so the current state is irrelevant here.
func featureFromStatus(mode, status int) probeFeature {
	if status == 0 || (status == 4 && mode == 2026) {
		return probeRejected
	}
	return probeSupported
}

// applyProbeFeatures downgrades a feature snapshot to what the terminal itself
// reported. Unknown answers keep the defaults — a silent terminal is assumed
// to support what every VT102 descendant supports — and only a positive
// rejection removes a capability, so a lost reply can never corrupt output.
// The sync veto mirrors the scroll veto: synchronized output is opt-in via
// LUX_TUI_SYNC_OUTPUT, but a terminal that answers DECRQM with "mode 2026
// unrecognized" would otherwise receive mode-set wrappers it cannot parse.
func applyProbeFeatures(features capability.Set, reply probeReply) capability.Set {
	if reply.scroll == probeRejected {
		features.ScrollRegions = false
	}
	if reply.sync == probeRejected {
		features.SynchronizedOutput = false
	}
	return features
}

// probeTimeoutConfigured reads LUX_TUI_PROBE_TIMEOUT. "0", a negative number,
// "off", "false", and "no" disable negotiation for deterministic runs; a
// positive integer is a millisecond budget; anything else falls back to the
// default after printing a one-line notice on stderr — negotiation runs before
// the alternate screen is entered, so the notice cannot disturb a frame. The
// kill-switch matters most on terminals that answer nothing: they would
// otherwise pay the full timeout at every startup.
func probeTimeoutConfigured() (time.Duration, bool) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("LUX_TUI_PROBE_TIMEOUT")))
	switch value {
	case "", "default":
		return defaultProbeTimeout, true
	case "off", "false", "no":
		return 0, false
	}
	if ms, err := strconv.Atoi(value); err == nil {
		if ms <= 0 {
			return 0, false
		}
		return time.Duration(ms) * time.Millisecond, true
	}
	fmt.Fprintf(os.Stderr, "lux-tui: invalid LUX_TUI_PROBE_TIMEOUT %q, using default %s\n", value, defaultProbeTimeout)
	return defaultProbeTimeout, true
}
