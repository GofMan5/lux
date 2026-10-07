package terminal

import (
	"os"
	"strconv"
	"strings"
)

// muxKind names which multiplexer, if any, negotiation is talking through.
// The distinction matters only for documentation and tests — every detected
// multiplexer applies the same input gate.
type muxKind uint8

const (
	muxNone muxKind = iota
	muxTmux
	muxScreen
)

func (kind muxKind) String() string {
	switch kind {
	case muxTmux:
		return "tmux"
	case muxScreen:
		return "screen"
	default:
		return "none"
	}
}

// detectMultiplexer decides whether the reports the probe received came from
// a multiplexer's pane rather than from the host terminal.
//
// Environment is the primary witness: tmux exports $TMUX to its panes and
// screen exports $STY, and $TERM carries the propagated pane terminal name
// ("tmux-256color", "screen.xterm-256color"), which survives an ssh hop that
// $TMUX itself does not — the classic case is a local tmux pane with ssh into
// a remote box: the remote shell has no $TMUX, but the probe bytes still land
// in the local tmux's parser, and TERM says so.
//
// The device reports are the corroborating witness, valid exactly where the
// environment lies or is absent. tmux answers DA1 with the VT100 shape
// "?1;2c" (sixel builds append ";4") while every host modern enough for the
// probed protocols reports a VT220-class attribute set — GNU screen answers
// DA1 byte-identically, so that witness alone cannot tell tmux from screen,
// which is harmless: the gate below treats every multiplexer the same, and
// the DA2 code or the environment separates them for bookkeeping. tmux's DA2
// manufacturer code is 84 — a value no real host claims (Windows Terminal
// answers 0, xterm 41, VTE 65, screen 83, WezTerm 123). Screen's DA2 is not
// used as a fingerprint: only the version parameter behind the manufacturer
// code drifts between builds, and 83 was never audited for exclusivity among
// real terminals, so screen stays environment-detected only.
//
// Any single witness is enough. A false positive costs only the input
// protocols the gate below withholds; a false negative costs a pane that
// pretends to speak a protocol its re-encoded input never carries. Asymmetry
// like that belongs on the conservative side.
func detectMultiplexer(reply probeReply) muxKind {
	if os.Getenv("TMUX") != "" {
		return muxTmux
	}
	if os.Getenv("STY") != "" {
		return muxScreen
	}
	term := os.Getenv("TERM")
	if strings.HasPrefix(term, "tmux") {
		return muxTmux
	}
	if strings.HasPrefix(term, "screen") {
		return muxScreen
	}
	if tmuxDA2Code(reply.da2) == 84 {
		return muxTmux
	}
	switch reply.da1 {
	case "1;2", "1;2;4":
		return muxTmux
	}
	return muxNone
}

// tmuxDA2Code extracts the manufacturer code — the first parameter — from a
// recorded DA2 body. Empty and malformed bodies return -1, which matches no
// known manufacturer.
func tmuxDA2Code(da2 string) int {
	if da2 == "" {
		return -1
	}
	code := da2
	if idx := strings.IndexByte(code, ';'); idx >= 0 {
		code = code[:idx]
	}
	value, err := strconv.Atoi(code)
	if err != nil {
		return -1
	}
	return value
}

// gateMultiplexerInput enforces the provenance rule of stage 6: input
// protocols require a trustworthy answerer, output protocols do not.
//
// The probe's questions are answered by whichever parser receives them —
// inside a pane that parser is the multiplexer, and that is the same parser
// that will consume the program's output. So DECRPM verdicts for the output
// family (scroll regions via DECOM, synchronized output via 2026) describe
// the app-to-multiplexer path and are honest: tmux implements DECSTBM and
// 2026 for its panes itself, and rejections are equally honest.
//
// Input encodings are categorically different: the bytes a pane receives are
// whatever the multiplexer chooses to re-emit, not what the host emits. tmux
// parses every keystroke and re-encodes it legacy — it never re-emits kitty
// flag reports, SGR pixel coordinates, or win32-input-mode records even when
// the host underneath speaks all three to tmux itself. A "supported" answer
// for those modes from a multiplexer therefore attributes nothing about the
// input path, so the gate rewrites it to unknown, which keeps the legacy
// decoding that matches the bytes the pane actually receives. Rejections and
// silence stand: both come from the multiplexer's own input parser — the
// same parser that will feed the pane — so "not supported" is as honest as
// "no answer".
//
// Confirming passthrough cannot rescue the verdict, because DCS passthrough
// is output-only: a pane can wrap output for the host, but host replies are
// consumed by the multiplexer's own input parser and never forwarded back —
// there is no byte-level way to observe "the host answered me directly" from
// inside a pane. Querying before launching the multiplexer would work but is
// not observable either. kitty thus stays off inside a detected
// multiplexer until the multiplexer implements the protocol natively; a
// native implementation's answers would then describe the pane path and be
// honest exactly the way 2026's already are.
//
// The gate is defense in depth rather than a live-bug fix: today's tmux
// honestly rejects 1016 and 9001 (3.7 and later) or never answers them
// (3.6 and earlier), and drops the kitty query in every version, so its
// panes already negotiate to legacy input. What the gate buys is survival
// of the next "helpful" multiplexer, a mis-routed passthrough reply, or a
// pane whose $TERM was forced to a host name — input stays decodable in
// all of them.
func gateMultiplexerInput(reply probeReply) probeReply {
	if detectMultiplexer(reply) == muxNone {
		return reply
	}
	if reply.keyboard == probeSupported {
		reply.keyboard = probeUnknown
	}
	if reply.pixel == probeSupported {
		reply.pixel = probeUnknown
	}
	if reply.win32 == probeSupported {
		reply.win32 = probeUnknown
	}
	return reply
}
