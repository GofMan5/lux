package terminal

import (
	"context"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lux-tui/lux/capability"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

func TestParserStreamingKeysUTF8AndModifiers(t *testing.T) {
	parser := newParser()
	var got []event.Event
	for _, chunk := range [][]byte{[]byte("a\x1b["), []byte("A\x1b[1;6D"), []byte("\x1bé"), []byte("\x03")} {
		got = append(got, parser.feed(chunk, false)...)
	}
	want := []event.Event{
		event.RuneKey('a', 0),
		event.KeyPress(event.KeyUp, 0),
		event.KeyPress(event.KeyLeft, event.ModShift|event.ModCtrl),
		event.RuneKey('é', event.ModAlt),
		event.RuneKey('C', event.ModCtrl),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestParserMousePasteFocusAndFunctions(t *testing.T) {
	input := "\x1b[<20;7;9M\x1b[<64;2;3M\x1b[I\x1b[O\x1b[15~\x1b[200~hello\x1b[31m界\x1b[201~"
	got := newParser().feed([]byte(input), false)
	if len(got) != 6 {
		t.Fatalf("event count = %d: %#v", len(got), got)
	}
	if mouse := got[0].Mouse; got[0].Kind != event.Mouse || mouse.X != 6 || mouse.Y != 8 || mouse.Action != event.MousePress || mouse.Button != event.MouseLeft || mouse.Mod != event.ModCtrl|event.ModShift {
		t.Fatalf("press = %#v", got[0])
	}
	if mouse := got[1].Mouse; mouse.Action != event.MouseWheel || mouse.Button != event.MouseWheelUp || mouse.X != 1 || mouse.Y != 2 {
		t.Fatalf("wheel = %#v", got[1])
	}
	if got[2].Kind != event.Focus || !got[2].Focused || got[3].Kind != event.Focus || got[3].Focused {
		t.Fatalf("focus = %#v %#v", got[2], got[3])
	}
	if !got[4].Key.Is(event.KeyF5, 0) {
		t.Fatalf("function key = %#v", got[4])
	}
	// The paste payload remains opaque, including escape sequences.
	if got[5].Kind != event.Paste || got[5].Text != "hello\x1b[31m界" {
		t.Fatalf("paste = %#v", got[5])
	}
}

func TestParserMouseAllMotion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want event.MouseEvent
	}{
		{"hover motion", "\x1b[<35;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseNone}},
		{"hover motion without flag", "\x1b[<3;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseNone}},
		{"drag with button", "\x1b[<32;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseLeft}},
		{"press", "\x1b[<0;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MousePress, Button: event.MouseLeft}},
		{"release keeps button", "\x1b[<0;5;2m", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseRelease, Button: event.MouseLeft}},
		{"release middle", "\x1b[<1;5;2m", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseRelease, Button: event.MouseMiddle}},
		{"release without button", "\x1b[<3;5;2m", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseRelease, Button: event.MouseNone}},
		{"shifted hover", "\x1b[<39;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseNone, Mod: event.ModShift}},
		{"wheel right", "\x1b[<67;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseWheel, Button: event.MouseWheelRight}},
		{"back button", "\x1b[<128;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MousePress, Button: event.MouseBack}},
		{"forward button", "\x1b[<129;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MousePress, Button: event.MouseForward}},
		{"back button release", "\x1b[<128;5;2m", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseRelease, Button: event.MouseBack}},
		{"forward button drag", "\x1b[<161;5;2M", event.MouseEvent{X: 4, Y: 1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseForward}},
	}
	for _, tc := range cases {
		got := newParser().feed([]byte(tc.in), false)
		if len(got) != 1 || got[0].Kind != event.Mouse {
			t.Fatalf("%s: events = %#v", tc.name, got)
		}
		if got[0].Mouse != tc.want {
			t.Fatalf("%s: mouse = %#v, want %#v", tc.name, got[0].Mouse, tc.want)
		}
	}
	// Out-of-range and malformed sequences still fall through to the
	// unknown-key escape hatch.
	for _, bad := range []string{"\x1b[<0;0;2M", "\x1b[<0;5;0M", "\x1b[<256;5;2M", "\x1b[<0;5;2X", "\x1b[<0;5M", "\x1b[<130;5;2M", "\x1b[<131;5;2M"} {
		got := newParser().feed([]byte(bad), false)
		if len(got) == 0 || got[0].Kind != event.Key || !got[0].Key.Is(event.KeyUnknown, 0) {
			t.Fatalf("bad sequence %q decoded as %#v", bad, got)
		}
	}
}

// pixelParser returns a parser decoding mode 1016 streams, with the optional
// cell-size report already absorbed.
func pixelParser(cellWidth, cellHeight int) *parser {
	parser := newParser()
	parser.pixelMouse = true
	if cellWidth > 0 {
		parser.cellW, parser.cellH = cellWidth, cellHeight
	}
	return parser
}

// TestParserPixelMouse covers the mode 1016 decode: pixel coordinates pass
// through verbatim, cells derive from the reported cell size (or stay -1
// until one lands), kitty's leave code becomes an off-screen no-button
// motion, and the reserved codes stay junk.
func TestParserPixelMouse(t *testing.T) {
	beforeSize := []struct {
		name string
		in   string
		want event.MouseEvent
	}{
		{"press before cell size", "\x1b[<0;49;21M", event.MouseEvent{X: -1, Y: -1, PixelX: 49, PixelY: 21, Action: event.MousePress, Button: event.MouseLeft}},
		{"hover motion", "\x1b[<35;49;21M", event.MouseEvent{X: -1, Y: -1, PixelX: 49, PixelY: 21, Action: event.MouseMove, Button: event.MouseNone}},
		{"leave sentinel", "\x1b[<288;49;21M", event.MouseEvent{X: -1, Y: -1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseNone}},
		{"leave sentinel with modifiers", "\x1b[<296;49;21M", event.MouseEvent{X: -1, Y: -1, PixelX: -1, PixelY: -1, Action: event.MouseMove, Button: event.MouseNone, Mod: event.ModAlt}},
	}
	for _, tc := range beforeSize {
		got := pixelParser(0, 0).feed([]byte(tc.in), false)
		if len(got) != 1 || got[0].Kind != event.Mouse {
			t.Fatalf("%s: events = %#v", tc.name, got)
		}
		if got[0].Mouse != tc.want {
			t.Fatalf("%s: mouse = %#v, want %#v", tc.name, got[0].Mouse, tc.want)
		}
	}
	afterSize := []struct {
		name string
		in   string
		want event.MouseEvent
	}{
		{"press maps to cell", "\x1b[<0;49;21M", event.MouseEvent{X: 4, Y: 1, PixelX: 49, PixelY: 21, Action: event.MousePress, Button: event.MouseLeft}},
		{"release maps to cell", "\x1b[<0;49;21m", event.MouseEvent{X: 4, Y: 1, PixelX: 49, PixelY: 21, Action: event.MouseRelease, Button: event.MouseLeft}},
		{"wheel maps to cell", "\x1b[<64;49;21M", event.MouseEvent{X: 4, Y: 1, PixelX: 49, PixelY: 21, Action: event.MouseWheel, Button: event.MouseWheelUp}},
		{"cell boundary rounds down", "\x1b[<0;9;20M", event.MouseEvent{X: 0, Y: 0, PixelX: 9, PixelY: 20, Action: event.MousePress, Button: event.MouseLeft}},
		{"back button maps to cell", "\x1b[<128;49;21M", event.MouseEvent{X: 4, Y: 1, PixelX: 49, PixelY: 21, Action: event.MousePress, Button: event.MouseBack}},
	}
	for _, tc := range afterSize {
		got := pixelParser(10, 21).feed([]byte(tc.in), false)
		if len(got) != 1 || got[0].Kind != event.Mouse {
			t.Fatalf("%s: events = %#v", tc.name, got)
		}
		if got[0].Mouse != tc.want {
			t.Fatalf("%s: mouse = %#v, want %#v", tc.name, got[0].Mouse, tc.want)
		}
	}
	// The pixel button cap is 511 (kitty's leave codes occupy 256-316), so
	// anything above stays junk — as do the reserved 130/131 button codes.
	for _, bad := range []string{"\x1b[<512;49;21M", "\x1b[<130;49;21M", "\x1b[<131;49;21M", "\x1b[<0;-1;21M", "\x1b[<0;49;-1M"} {
		got := pixelParser(10, 21).feed([]byte(bad), false)
		if len(got) == 0 || got[0].Kind != event.Key || !got[0].Key.Is(event.KeyUnknown, 0) {
			t.Fatalf("bad pixel sequence %q decoded as %#v", bad, got)
		}
	}
}

// TestParserCellSizeReport covers the CSI 16 t reply handling: the report
// is absorbed silently while pixel mouse is active — updating the cell
// mapping mid-stream — and stays visible as junk in a cell-mode stream,
// where this backend never queries.
func TestParserCellSizeReport(t *testing.T) {
	parser := pixelParser(0, 0)
	got := parser.feed([]byte("\x1b[6;21;10t"), false)
	if len(got) != 0 || parser.cellW != 10 || parser.cellH != 21 {
		t.Fatalf("reply not absorbed: events=%#v cell=%d,%d", got, parser.cellW, parser.cellH)
	}
	// The absorbed size converts the next pixel report to cells.
	got = parser.feed([]byte("\x1b[<0;49;21M"), false)
	if len(got) != 1 || got[0].Mouse.X != 4 || got[0].Mouse.Y != 1 {
		t.Fatalf("cell mapping after reply = %#v", got)
	}
	// A zero, negative, or trailing-field size is junk, not a mapping
	// change. The negative row matters because any other non-digit would
	// itself be a CSI final byte and split the sequence earlier.
	for _, bad := range []string{"\x1b[6;0;10t", "\x1b[6;21;0t", "\x1b[6;-1;10t", "\x1b[6;21t", "\x1b[6;21;10M", "\x1b[6;21;10;5t"} {
		parser := pixelParser(0, 0)
		events := parser.feed([]byte(bad), false)
		if len(events) != 1 || events[0].Kind != event.Key || !events[0].Key.Is(event.KeyUnknown, 0) || parser.cellW != 0 || parser.cellH != 0 {
			t.Fatalf("malformed report %q decoded as %#v cell=%d,%d", bad, events, parser.cellW, parser.cellH)
		}
	}
	// In cell mode the gate is off: the same bytes stay visible instead of
	// being swallowed as parser bookkeeping.
	got = newParser().feed([]byte("\x1b[6;21;10t"), false)
	if len(got) != 1 || got[0].Kind != event.Key || !got[0].Key.Is(event.KeyUnknown, 0) {
		t.Fatalf("cell-mode report = %#v", got)
	}
	// A doubled escape before the reply is terminal junk, not a mapping
	// change: the inner decode sees a None-kind event and the outer layer
	// surfaces it as an unknown key.
	got = pixelParser(0, 0).feed([]byte("\x1b\x1b[6;21;10t"), false)
	if len(got) != 1 || got[0].Kind != event.Key || !got[0].Key.Is(event.KeyUnknown, 0) {
		t.Fatalf("doubled-escape reply = %#v", got)
	}
}

// TestEnterPixelSequences pins the wire constants: pixel mode swaps 1006 for
// 1016, keeps the 1003 all-motion and the rest of the enter stack, and pairs
// the enter with a cell-size query whose reply the parser absorbs.
func TestEnterPixelSequences(t *testing.T) {
	if !strings.Contains(enterPixel, "\x1b[?1016h") || strings.Contains(enterPixel, "\x1b[?1006h") {
		t.Fatalf("enterPixel does not replace 1006 with 1016: %q", enterPixel)
	}
	if !strings.Contains(enterPixel, "\x1b[?1003h") || !strings.Contains(enterPixel, "\x1b[?1049h") || !strings.Contains(enterPixel, cellSizeQuery) {
		t.Fatalf("enterPixel lost shared enter stack: %q", enterPixel)
	}
	if !strings.Contains(leavePixel, "\x1b[?1016l") || !strings.Contains(leavePixel, "\x1b[?1003l") || !strings.Contains(leavePixel, "\x1b[?1049l") || strings.Contains(leavePixel, "\x1b[?1006l") {
		t.Fatalf("leavePixel does not mirror enterPixel: %q", leavePixel)
	}
}

func TestParserBoundedMalformedInput(t *testing.T) {
	parser := newParser()
	got := parser.feed(append([]byte{0x1b, '['}, []byte(strings.Repeat("1", maxSequence+5))...), false)
	if len(parser.buffer) > maxSequence || len(got) == 0 || !got[0].Key.Is(event.KeyEscape, 0) {
		t.Fatalf("unbounded or unconsumed escape: buffered=%d events=%#v", len(parser.buffer), got)
	}

	parser = newParser()
	payload := strings.Repeat("x", maxPaste+100)
	got = parser.feed([]byte(pasteStart+payload+pasteEnd), false)
	if len(got) != 1 || got[0].Kind != event.Paste || len(got[0].Text) != maxPaste {
		t.Fatalf("bounded paste len=%d events=%d", len(got[0].Text), len(got))
	}
}

func TestParserLoneEscapeTimeout(t *testing.T) {
	parser := newParser()
	if got := parser.feed([]byte{0x1b}, false); len(got) != 0 || !parser.pendingEscape() {
		t.Fatalf("lone escape was not buffered: %#v", got)
	}
	got := parser.flushEscape()
	if len(got) != 1 || !got[0].Key.Is(event.KeyEscape, 0) || parser.pendingEscape() {
		t.Fatalf("escape timeout = %#v", got)
	}
}

// TestParserKittyDisambiguateKeys covers the CSI u decoding of the kitty
// keyboard protocol's disambiguate and event-type levels, the two levels
// the backend enables. The terminal keeps unambiguous keys legacy, so the
// mix below with arrow keys and plain runes in the last case is the shape
// a real enhanced stream takes.
func TestParserKittyDisambiguateKeys(t *testing.T) {
	kittyRune := func(r rune, mod event.Mod, action event.KeyAction) event.Event {
		repeat := uint16(1)
		if action == event.ActionRelease {
			repeat = 0
		}
		return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: r, Mod: mod, Action: action, Repeat: repeat}}
	}
	kittyKey := func(code event.KeyCode, mod event.Mod, action event.KeyAction) event.Event {
		repeat := uint16(1)
		if action == event.ActionRelease {
			repeat = 0
		}
		return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: code, Mod: mod, Action: action, Repeat: repeat}}
	}
	tests := []struct {
		name  string
		input string
		want  event.Event
	}{
		{"escape", "\x1b[27u", event.KeyPress(event.KeyEscape, 0)},
		{"alt+escape", "\x1b[27;3u", event.KeyPress(event.KeyEscape, event.ModAlt)},
		{"ctrl+escape", "\x1b[27;5u", event.KeyPress(event.KeyEscape, event.ModCtrl)},
		{"shift+alt+escape", "\x1b[27;4u", event.KeyPress(event.KeyEscape, event.ModShift|event.ModAlt)},
		{"ctrl+a", "\x1b[97;5u", event.RuneKey('a', event.ModCtrl)},
		{"alt+x", "\x1b[120;3u", event.RuneKey('x', event.ModAlt)},
		{"ctrl+alt+x", "\x1b[120;7u", event.RuneKey('x', event.ModCtrl|event.ModAlt)},
		{"shift+ctrl+a", "\x1b[97;6u", event.RuneKey('a', event.ModShift|event.ModCtrl)},
		{"super+x", "\x1b[120;9u", event.RuneKey('x', event.ModSuper)},
		// The modifier value is 1 + bitfield, so 17/33/65/129 carry the
		// hyper, meta, caps-lock, and num-lock bits; enhanced terminals
		// report them for non-text keys and they surface as Mod bits.
		{"hyper bit", "\x1b[97;17u", event.RuneKey('a', event.ModHyper)},
		{"meta bit", "\x1b[97;33u", event.RuneKey('a', event.ModMeta)},
		{"caps-lock bit", "\x1b[97;65u", event.RuneKey('a', event.ModCapsLock)},
		{"num-lock bit", "\x1b[97;129u", event.RuneKey('a', event.ModNumLock)},
		{"caps-lock and num-lock bits", "\x1b[97;193u", event.RuneKey('a', event.ModCapsLock|event.ModNumLock)},
		{"shift+enter", "\x1b[13;2u", event.KeyPress(event.KeyEnter, event.ModShift)},
		{"ctrl+enter", "\x1b[13;5u", event.KeyPress(event.KeyEnter, event.ModCtrl)},
		{"alt+enter", "\x1b[13;3u", event.KeyPress(event.KeyEnter, event.ModAlt)},
		{"ctrl+backspace", "\x1b[127;5u", event.KeyPress(event.KeyBackspace, event.ModCtrl)},
		{"alt+backspace", "\x1b[127;3u", event.KeyPress(event.KeyBackspace, event.ModAlt)},
		{"shift+tab normalizes to backtab", "\x1b[9;2u", event.KeyPress(event.KeyBackTab, 0)},
		{"ctrl+tab keeps the shift bit clear", "\x1b[9;5u", event.KeyPress(event.KeyTab, event.ModCtrl)},
		{"explicit press modifier value", "\x1b[9;1u", event.KeyPress(event.KeyTab, 0)},
		{"plain rune code", "\x1b[97u", event.RuneKey('a', 0)},
		{"latin-1 rune code", "\x1b[233u", event.RuneKey('é', 0)},
		{"cyrillic rune code", "\x1b[1048u", event.RuneKey('И', 0)},
		{"astral rune code", "\x1b[128516u", event.RuneKey('😄', 0)},
		{"largest codepoint", "\x1b[1114111u", event.RuneKey(0x10FFFF, 0)},
		{"space with a modifier", "\x1b[32;3u", event.RuneKey(' ', event.ModAlt)},
		// Event types: the canonical form nests the type after a colon in
		// the modifier field (CSI 97;1:3u is a release); the trailing
		// semicolon field is the pre-standard spelling, still read.
		{"press event type", "\x1b[97;3;1u", kittyRune('a', event.ModAlt, event.ActionPress)},
		{"repeat event type", "\x1b[97;3;2u", kittyRune('a', event.ModAlt, event.ActionRepeat)},
		{"release event type", "\x1b[97;3;3u", kittyRune('a', event.ModAlt, event.ActionRelease)},
		{"canonical press subparameter", "\x1b[97;1:1u", kittyRune('a', 0, event.ActionPress)},
		{"canonical repeat subparameter", "\x1b[97;3:2u", kittyRune('a', event.ModAlt, event.ActionRepeat)},
		{"canonical release subparameter", "\x1b[97;3:3u", kittyRune('a', event.ModAlt, event.ActionRelease)},
		{"canonical release of escape", "\x1b[27;5:3u", kittyKey(event.KeyEscape, event.ModCtrl, event.ActionRelease)},
		{"canonical press wins over a junk trailing field", "\x1b[97;1:1;120u", kittyRune('a', 0, event.ActionPress)},
		{"associated text is ignored", "\x1b[97;3;1;97u", kittyRune('a', event.ModAlt, event.ActionPress)},
		{"alternate-key subparameters are stripped", "\x1b[97:65:97;3u", event.RuneKey('a', event.ModAlt)},
		// Junk in a parameter can only be bytes below the CSI final range,
		// because anything from 0x40 up terminates the sequence; the
		// tolerated junk below therefore uses punctuation.
		{"junk modifier field is tolerated as unmodified", "\x1b[97;,u", event.RuneKey('a', 0)},
		{"zero modifier value", "\x1b[97;0u", event.RuneKey('a', 0)},
		// Unrequested or unrepresentable event types surface as an unknown
		// key instead of being silently dropped or misread as a press.
		{"junk event type is unknown", "\x1b[97;3;,u", event.KeyPress(event.KeyUnknown, 0)},
		{"out-of-range event type is unknown", "\x1b[97;3;9u", event.KeyPress(event.KeyUnknown, 0)},
		{"out-of-range canonical event type is unknown", "\x1b[97;3:9u", event.KeyPress(event.KeyUnknown, 0)},
		// The query and stack-management shapes share the final byte but
		// never the leading parameter, and they must not decode as keys.
		{"query reply shape is unknown", "\x1b[?1u", event.KeyPress(event.KeyUnknown, 0)},
		{"query reply shape with junk flags", "\x1b[?,u", event.KeyPress(event.KeyUnknown, 0)},
		{"stack push shape is unknown", "\x1b[>1u", event.KeyPress(event.KeyUnknown, 0)},
		{"stack pop shape is unknown", "\x1b[<u", event.KeyPress(event.KeyUnknown, 0)},
		{"empty code is unknown", "\x1b[;5u", event.KeyPress(event.KeyUnknown, 0)},
		{"bare CSI u is unknown", "\x1b[u", event.KeyPress(event.KeyUnknown, 0)},
		{"code zero is unknown", "\x1b[0u", event.KeyPress(event.KeyUnknown, 0)},
		{"control codepoint is unknown", "\x1b[3u", event.KeyPress(event.KeyUnknown, 0)},
		// Kitty numbers its functional and media keys inside the Unicode
		// private use area U+E000..U+F8FF and sends CSI u for them even in
		// pure legacy mode, so the table below maps them onto plain keys.
		// The codepoints kitty itself leaves unused — the block's base, the
		// modifier keys (a modifier key never arrives as a key event), and
		// the trailing half — still decode as unknown rather than
		// injecting a private-use character into a text field.
		{"F13 from the kitty table", "\x1b[57376u", event.KeyPress(event.KeyF13, 0)},
		{"F35 caps the kitty F-row", "\x1b[57398u", event.KeyPress(event.KeyF35, 0)},
		{"ctrl+F13 keeps modifiers", "\x1b[57376;5u", event.KeyPress(event.KeyF13, event.ModCtrl)},
		{"keypad digit normalizes to a rune", "\x1b[57400u", event.RuneKey('1', 0)},
		{"keypad separator normalizes to a rune", "\x1b[57416u", event.RuneKey(',', 0)},
		{"keypad enter is enter", "\x1b[57414u", event.KeyPress(event.KeyEnter, 0)},
		{"keypad left is left", "\x1b[57417;3u", event.KeyPress(event.KeyLeft, event.ModAlt)},
		{"media play/pause decodes with an event type", "\x1b[57430;1:3u", kittyKey(event.KeyMediaPlayPause, 0, event.ActionRelease)},
		{"unused base of the kitty range is unknown", "\x1b[57344u", event.KeyPress(event.KeyUnknown, 0)},
		{"kitty modifier key stays unknown", "\x1b[57441u", event.KeyPress(event.KeyUnknown, 0)},
		{"kitty keypad begin is unknown", "\x1b[57427u", event.KeyPress(event.KeyUnknown, 0)},
		{"end of the private use area is unknown", "\x1b[63743u", event.KeyPress(event.KeyUnknown, 0)},
		{"first codepoint past the private use area is a rune", "\x1b[63744u", event.RuneKey(0xF900, 0)},
		{"surrogate codepoint is unknown", "\x1b[55296u", event.KeyPress(event.KeyUnknown, 0)},
		{"codepoint past Unicode is unknown", "\x1b[1114112u", event.KeyPress(event.KeyUnknown, 0)},
		{"overflowing code is unknown", "\x1b[99999999999999999999u", event.KeyPress(event.KeyUnknown, 0)},
		{"five fields are unknown", "\x1b[97;3;1;97;9u", event.KeyPress(event.KeyUnknown, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newParser().feed([]byte(tt.input), false)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("feed(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// TestParserKittyEscapeIsImmediate pins the stage 6 acceptance: in enhanced
// mode Esc arrives as the complete CSI 27 u sequence, which ends on a final
// byte, so it decodes the moment its last byte lands and never waits out
// the 35 ms lone-escape delay.
func TestParserKittyEscapeIsImmediate(t *testing.T) {
	parser := newParser()
	if got := parser.feed([]byte("\x1b[27u"), false); len(got) != 1 || !got[0].Key.Is(event.KeyEscape, 0) {
		t.Fatalf("escape was not decoded immediately: %#v", got)
	}
	if parser.pendingEscape() {
		t.Fatal("a complete kitty escape left the parser waiting on a timeout")
	}
	// Bytes already in flight behind the escape arrive as their own events,
	// proving the escape did not swallow the rest of the stream.
	got := newParser().feed([]byte("\x1b[27uX"), false)
	if len(got) != 2 || !got[0].Key.Is(event.KeyEscape, 0) || got[1] != event.RuneKey('X', 0) {
		t.Fatalf("stream after kitty escape = %#v", got)
	}
}

// TestParserKittyStreamsAcrossReads proves a CSI u event split across read
// boundaries decodes only once its final byte arrives.
func TestParserKittyStreamsAcrossReads(t *testing.T) {
	parser := newParser()
	var got []event.Event
	for _, chunk := range []string{"\x1b[2", "7;5", "u\x1b[", "27;3"} {
		got = append(got, parser.feed([]byte(chunk), false)...)
	}
	if len(got) != 1 || !got[0].Key.Is(event.KeyEscape, event.ModCtrl) {
		t.Fatalf("split ctrl+escape = %#v", got)
	}
	got = append(got, parser.feed([]byte("u\x1b[Aa"), false)...)
	want := []event.Event{
		event.KeyPress(event.KeyEscape, event.ModCtrl),
		event.KeyPress(event.KeyEscape, event.ModAlt),
		event.KeyPress(event.KeyUp, 0),
		event.RuneKey('a', 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enhanced stream mixed with legacy keys = %#v, want %#v", got, want)
	}
}

// TestParserLegacyFunctionKeysAndC0Splits pins the legacy half of the kitty
// truth. Terminals re-send xterm's shifted function-key CSI ~ encodings
// after kitty mode is on (29~ is MENU for kitty, F16 for xterm — kitty
// wins). The C0 backspace pair splits: DEL is plain Backspace, BS is the
// ctrl chord, each carrying the alt prefix behind a lone escape. Alt-
// modified arrow keys arrive with a doubled escape, and a lone doubled
// escape surfaces as two escapes one timeout tick apart.
func TestParserLegacyFunctionKeysAndC0Splits(t *testing.T) {
	input := "\x1b[25~\x1b[34~\x1b[29~\x08\x7f\x1b\x08\x1b\t\x1b\x1b[A"
	want := []event.Event{
		event.KeyPress(event.KeyF13, 0),
		event.KeyPress(event.KeyF20, 0),
		event.KeyPress(event.KeyMenu, 0),
		event.KeyPress(event.KeyBackspace, event.ModCtrl),
		event.KeyPress(event.KeyBackspace, 0),
		event.KeyPress(event.KeyBackspace, event.ModCtrl|event.ModAlt),
		event.KeyPress(event.KeyTab, event.ModAlt),
		event.KeyPress(event.KeyUp, event.ModAlt),
	}
	if got := newParser().feed([]byte(input), false); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy stream = %#v, want %#v", got, want)
	}

	// A doubled escape without a follower waits: each timeout tick
	// surfaces one escape, two ticks apart.
	parser := newParser()
	if got := parser.feed([]byte("\x1b\x1b"), false); len(got) != 0 {
		t.Fatalf("doubled escape resolved early: %#v", got)
	}
	got := parser.flushEscape()
	if len(got) != 1 || !got[0].Key.Is(event.KeyEscape, 0) {
		t.Fatalf("first timeout = %#v", got)
	}
	got = parser.flushEscape()
	if len(got) != 1 || !got[0].Key.Is(event.KeyEscape, 0) {
		t.Fatalf("second timeout = %#v", got)
	}
	if parser.pendingEscape() {
		t.Fatal("doubled escape left residue after two timeouts")
	}
}

func TestReadLoopClosesCapturedDoneChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("anonymous pipes are not console input handles on Windows")
	}
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	done := make(chan struct{})
	backend := &Backend{input: input}
	go backend.readLoop(context.Background(), done, make(chan event.Event, 1), newParser(), nil)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(hangTimeout):
		t.Fatal("read loop did not close its captured generation channel")
	}
}

func TestANSIFrameExactDiffAndWideCells(t *testing.T) {
	frame := render.NewBuffer(5, 1)
	canvas := render.NewCanvas(frame)
	style := render.Style{Foreground: render.RGBColor(1, 2, 3), Background: render.IndexedColor(42), Attrs: render.AttrBold}
	canvas.Text(0, 0, "A界B", style)
	got := string(appendFrame(nil, frame, nil, capability.TrueColor, true, false, false))
	// The trailing blank leaves the terminal at the default style, so the frame
	// needs no closing reset.
	want := "\x1b[1;1H\x1b[0;1;38;2;1;2;3;48;5;42mA界B\x1b[0m "
	if got != want {
		t.Fatalf("full frame\n got: %q\nwant: %q", got, want)
	}

	previous := frame.Clone()
	if got := appendFrame(nil, frame, previous, capability.TrueColor, true, true, false); len(got) != 0 {
		t.Fatalf("unchanged synchronized frame = %q", got)
	}
	canvas.Put(4, 0, "X", render.Style{})
	got = string(appendFrame(nil, frame, previous, capability.ANSI16, true, true, false))
	want = "\x1b[?2026h\x1b[1;5H\x1b[0mX\x1b[?2026l"
	if got != want {
		t.Fatalf("diff\n got: %q\nwant: %q", got, want)
	}
}

func TestANSIFrameSkipsColorsEquivalentAfterDowngrade(t *testing.T) {
	previous := render.NewBuffer(1, 1)
	frame := render.NewBuffer(1, 1)
	render.NewCanvas(previous).Put(0, 0, "x", render.Style{Foreground: render.RGBColor(255, 0, 0)})
	render.NewCanvas(frame).Put(0, 0, "x", render.Style{Foreground: render.ANSIColor(9)})
	if got := appendFrame(nil, frame, previous, capability.ANSI16, true, false, false); len(got) != 0 {
		t.Fatalf("equivalent downgraded color emitted diff: %q", got)
	}
	if got := appendFrame(nil, frame, previous, capability.TrueColor, true, false, false); len(got) == 0 {
		t.Fatal("different true colors did not emit diff")
	}
}

func TestColorDowngradeAndPlainFrame(t *testing.T) {
	style := render.Style{Foreground: render.RGBColor(255, 0, 0), Background: render.IndexedColor(21)}
	if got, want := string(appendFullStyle(nil, style, capability.ANSI16)), "\x1b[0;91;104m"; got != want {
		t.Fatalf("ANSI16 style = %q, want %q", got, want)
	}
	if got, want := string(appendFullStyle(nil, style, capability.ANSI256)), "\x1b[0;38;5;196;48;5;21m"; got != want {
		t.Fatalf("ANSI256 style = %q, want %q", got, want)
	}

	frame := render.NewBuffer(4, 2)
	canvas := render.NewCanvas(frame)
	canvas.Text(0, 0, "hi", style)
	canvas.Text(0, 1, "界", style)
	if got, want := string(appendPlain(nil, frame)), "hi\n界\n"; got != want {
		t.Fatalf("plain frame = %q, want %q", got, want)
	}
}

func TestPlainBackendLifecycleAndFullWrites(t *testing.T) {
	t.Setenv("TERM", "dumb")
	file, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	backend := New(nil, file)
	features, err := backend.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if features.Interactive || features.Color != capability.Monochrome {
		t.Fatalf("plain capabilities = %#v", features)
	}
	frame := render.NewBuffer(2, 1)
	render.NewCanvas(frame).Text(0, 0, "ok", render.Style{})
	if err := backend.Present(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
	if _, _, err := backend.Size(); err == nil {
		t.Fatal("redirected Size succeeded")
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok\n" {
		t.Fatalf("output = %q", data)
	}
}

func TestBackendErrors(t *testing.T) {
	backend := New(nil, nil)
	if err := backend.Present(context.Background(), render.NewBuffer(1, 1)); err == nil {
		t.Fatal("Present before Open succeeded")
	}
	if err := backend.Present(context.Background(), nil); err == nil {
		t.Fatal("nil Present succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := backend.NextEvent(ctx); err == nil {
		t.Fatal("NextEvent before Open succeeded")
	}

	t.Setenv("TERM", "dumb")
	backend = New(nil, os.Stdout)
	if _, err := backend.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.NextEvent(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("NextEvent error = %v", err)
	}
	_ = backend.Close()
}

func FuzzParserNeverPanics(f *testing.F) {
	seeds := [][]byte{
		[]byte("hello"), []byte("\x1b[A"), []byte("\x1b[<0;1;1M"), []byte(pasteStart + "x" + pasteEnd),
		{0xff, 0x80, 0x1b},
	}
	// Kitty CSI u regressions: query and stack shapes, private-use key codes,
	// surrogates, overflowing digits, colon subparameters, event types, and
	// sequences that straddle read boundaries.
	kittySeeds := []string{
		"\x1b[?1u", "\x1b[>1u", "\x1b[<u", "\x1b[u", "\x1b[27u", "\x1b[27;5u", "\x1b[27;3u",
		"\x1b[57344u", "\x1b[63743u", "\x1b[63744u", "\x1b[1114112u", "\x1b[55296u", "\x1b[57343:27;1:3;65u",
		"\x1b[99999999999999999999u", "\x1b[9;2u", "\x1b[97;3;2;97u", "\x1b[97;;u", "\x1b[13;5u",
		"\x1b[27;17u", "\x1b[27;33u", "\x1b[;2u", "\x1b[27;2;99999999999999999999u",
		// Event-type subparameters (canonical colon form and the
		// pre-standard trailing field) and the lock-state modifier bits.
		"\x1b[97;1:3u", "\x1b[27;5:2u", "\x1b[13;1:1;13u", "\x1b[97;3:3u", "\x1b[97;3:,u",
		"\x1b[97;65u", "\x1b[97;129u", "\x1b[97;193u", "\x1b[27;257u",
		// Functional-table regressions: kitty private-use keys, the C0
		// backspace split, and the doubled-escape prefix.
		"\x1b[57376u", "\x1b[57398;5u", "\x1b[57400u", "\x1b[57414;3u", "\x1b[57427u",
		"\x1b[57430;1:3u", "\x1b[57441;2u", "\x1b[29~", "\x1b[34;5~", "\x08", "\x1b\x08",
		"\x1b\t", "\x1b\x1b", "\x1b\x1b[A", "\x1b\x1b[Z", "\x1b\x1b[<0;1;1M",
		// All-motion mouse shapes: hover without a button, releases, wheels,
		// and bit combinations past the button cap.
		"\x1b[<35;3;4M", "\x1b[<3;3;4M", "\x1b[<32;1;2M", "\x1b[<0;1;2m",
		"\x1b[<67;1;2M", "\x1b[<64;1;2m", "\x1b[<255;1;1M", "\x1b[<256;1;1M",
		"\x1b[<288;1;1M", "\x1b[<99999999999999999999;1;1M", "\x1b[<0;0;0M",
		// Pixel-mode shapes: the 511 button cap, kitty leave codes at both
		// ends of their range, the reserved 130/131 codes, negative
		// coordinates, and the cell-size report — valid, zero-sized, and
		// behind a doubled escape.
		"\x1b[<511;640;480M", "\x1b[<512;640;480M", "\x1b[<256;1;1M",
		"\x1b[<316;1;1M", "\x1b[<317;1;1M", "\x1b[<130;1;1M", "\x1b[<131;1;1M",
		"\x1b[<0;-1;1M", "\x1b[6;21;10t", "\x1b[6;0;10t", "\x1b[6;21;0t",
		"\x1b[6;99999999999999999999;10t", "\x1b\x1b[6;21;10t", "\x1b[6;21t",
		"\x1b[6;21;10;5t", "\x1b[6t",
		// OSC 8 hyperlinks and styled-underline shapes: terminals echo these
		// back on some clipboard and debug paths, so the parser must stay
		// bounded on well-formed, truncated, and unterminated forms, and on
		// the BEL terminator an OSC may carry instead of ST.
		"\x1b]8;;http://example.com\x1b\\", "\x1b]8;id=doc;http://example.com\x1b\\",
		"\x1b]8;;\x1b\\", "\x1b]8;;http://example.com", "\x1b]8;", "\x1b]8",
		"\x1b]8;;http://e\x07x", "\x1b]0;title\x07", "\x1b]8;;u\x9c",
		"\x1b[4:3m", "\x1b[4:0m", "\x1b[4;4:2;58;2;1;2;3m", "\x1b[58;5;213m",
		"\x1b[59m", "\x1b[24m", "\x1b[?2026h", "\x1b[?2026l",
	}
	for _, seed := range kittySeeds {
		seeds = append(seeds, []byte(seed))
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		parser := newParser()
		mid := len(data) / 2
		_ = parser.feed(data[:mid], false)
		_ = parser.feed(data[mid:], true)
		if len(parser.buffer) > maxSequence || len(parser.paste) > maxPaste {
			t.Fatalf("parser exceeded limits: buffer=%d paste=%d", len(parser.buffer), len(parser.paste))
		}
		// The pixel-mode decode runs the same stream through the alternate
		// guard set — pixel coordinates, the 511 button cap, and the CSI t
		// cell-size arm — so both decodes stay panic-free under the same
		// mutations.
		pixels := newParser()
		pixels.pixelMouse = true
		pixels.cellW, pixels.cellH = 10, 20
		_ = pixels.feed(data[:mid], false)
		_ = pixels.feed(data[mid:], true)
		if len(pixels.buffer) > maxSequence || len(pixels.paste) > maxPaste {
			t.Fatalf("pixel parser exceeded limits: buffer=%d paste=%d", len(pixels.buffer), len(pixels.paste))
		}
	})
}

func TestParserEOFCompletesEscapeAndNUL(t *testing.T) {
	got := newParser().feed([]byte("\x1b[A\x00"), true)
	if len(got) != 2 || !got[0].Key.Is(event.KeyUp, 0) || got[1].Key.Rune != ' ' || got[1].Key.Mod != event.ModCtrl {
		t.Fatalf("EOF events = %#v", got)
	}
}
