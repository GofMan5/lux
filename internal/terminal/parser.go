package terminal

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lux-tui/lux/event"
)

const (
	maxSequence = 128
	maxPaste    = 1 << 20
	pasteStart  = "\x1b[200~"
	pasteEnd    = "\x1b[201~"
)

type parser struct {
	buffer  []byte
	paste   []byte
	inPaste bool
}

func newParser() *parser { return &parser{} }

func (p *parser) pendingEscape() bool {
	return len(p.buffer) > 0 && p.buffer[0] == 0x1b && !p.inPaste
}

func (p *parser) flushEscape() []event.Event {
	if !p.pendingEscape() {
		return nil
	}
	p.buffer = p.buffer[1:]
	return []event.Event{event.KeyPress(event.KeyEscape, 0)}
}

func (p *parser) feed(data []byte, eof bool) []event.Event {
	p.buffer = append(p.buffer, data...)
	result := make([]event.Event, 0, 4)
	for len(p.buffer) > 0 {
		if p.inPaste {
			end := strings.Index(string(p.buffer), pasteEnd)
			if end < 0 {
				keep := min(len(pasteEnd)-1, len(p.buffer))
				p.appendPaste(p.buffer[:len(p.buffer)-keep])
				p.buffer = p.buffer[len(p.buffer)-keep:]
				break
			}
			p.appendPaste(p.buffer[:end])
			p.buffer = p.buffer[end+len(pasteEnd):]
			result = append(result, event.Event{Kind: event.Paste, Text: strings.ToValidUTF8(string(p.paste), "�")})
			p.paste, p.inPaste = p.paste[:0], false
			continue
		}
		if p.buffer[0] == 0x1b {
			next, consumed, complete := parseEscape(p.buffer, eof)
			if !complete {
				if len(p.buffer) <= maxSequence {
					break
				}
				next, consumed, complete = event.KeyPress(event.KeyEscape, 0), 1, true
			}
			if consumed == len(pasteStart) && string(p.buffer[:consumed]) == pasteStart {
				p.buffer = p.buffer[consumed:]
				p.inPaste = true
				continue
			}
			if complete {
				result = append(result, next)
				p.buffer = p.buffer[consumed:]
				continue
			}
		}
		next, consumed, complete := parseRune(p.buffer, eof)
		if !complete {
			break
		}
		result = append(result, next)
		p.buffer = p.buffer[consumed:]
	}
	if eof {
		if p.inPaste {
			p.appendPaste(p.buffer)
			p.buffer = p.buffer[:0]
			result = append(result, event.Event{Kind: event.Paste, Text: strings.ToValidUTF8(string(p.paste), "�")})
			p.paste, p.inPaste = p.paste[:0], false
		}
		for len(p.buffer) > 0 {
			if p.buffer[0] == 0x1b {
				next, consumed, complete := parseEscape(p.buffer, true)
				if complete {
					result = append(result, next)
					p.buffer = p.buffer[consumed:]
					continue
				}
			}
			next, consumed, _ := parseRune(p.buffer, true)
			result = append(result, next)
			p.buffer = p.buffer[consumed:]
		}
	}
	return result
}

func (p *parser) appendPaste(data []byte) {
	remaining := maxPaste - len(p.paste)
	if remaining > 0 {
		p.paste = append(p.paste, data[:min(remaining, len(data))]...)
	}
}

func parseRune(data []byte, eof bool) (event.Event, int, bool) {
	if len(data) == 0 {
		return event.Event{}, 0, false
	}
	switch data[0] {
	case '\r', '\n':
		return event.KeyPress(event.KeyEnter, 0), 1, true
	case '\t':
		return event.KeyPress(event.KeyTab, 0), 1, true
	case 0x7f:
		// DEL is the legacy Backspace; kitty distinguishes the ctrl chord
		// as the bare BS byte, so the two control codes decode apart.
		return event.KeyPress(event.KeyBackspace, 0), 1, true
	case '\b':
		return event.KeyPress(event.KeyBackspace, event.ModCtrl), 1, true
	}
	if data[0] < 0x20 {
		if data[0] == 0 {
			return event.RuneKey(' ', event.ModCtrl), 1, true
		}
		return event.RuneKey(rune(data[0]+'@'), event.ModCtrl), 1, true
	}
	if !utf8.FullRune(data) && !eof {
		return event.Event{}, 0, false
	}
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size == 1 {
		return event.Event{Kind: event.Text, Text: "�"}, 1, true
	}
	return event.RuneKey(r, 0), size, true
}

func parseEscape(data []byte, eof bool) (event.Event, int, bool) {
	if len(data) == 1 {
		if eof {
			return event.KeyPress(event.KeyEscape, 0), 1, true
		}
		return event.Event{}, 0, false
	}
	if data[1] == 0x1b {
		if len(data) == 2 {
			if eof {
				// At end of stream resolve the first escape; the loop
				// rehandles the surviving one below.
				return event.KeyPress(event.KeyEscape, 0), 1, true
			}
			return event.Event{}, 0, false
		}
		if data[2] == '[' || data[2] == 'O' {
			// A doubled escape before a CSI or SS3 opener is Alt-modified
			// arrow-family keys: Alt+Up is ESC ESC [ A. Decode the inner
			// sequence once and add the modifier rather than decoding the
			// second escape as ctrl+[ (the rune the C0 byte spells).
			next, consumed, complete := parseEscape(data[1:], eof)
			if !complete {
				return next, consumed, false
			}
			if next.Kind == event.Key {
				next.Key.Mod |= event.ModAlt
				return next, consumed + 1, true
			}
			// Mouse, focus, and paste markers never follow a doubled
			// escape; anything else here is terminal junk.
			return event.KeyPress(event.KeyUnknown, 0), consumed + 1, true
		}
		if eof {
			// A rune or other byte behind the pair: resolve the leading
			// escape and let the loop decode what follows on its own.
			return event.KeyPress(event.KeyEscape, 0), 1, true
		}
		// Not yet resolvable: a lone-timeout will surface the escapes one
		// 35 ms tick apart, which is the honest reading of Esc Esc.
		return event.Event{}, 0, false
	}
	if data[1] != '[' && data[1] != 'O' {
		next, consumed, complete := parseRune(data[1:], eof)
		if !complete {
			return event.Event{}, 0, false
		}
		if next.Kind == event.Key {
			next.Key.Mod |= event.ModAlt
		}
		return next, consumed + 1, true
	}
	end := sequenceEnd(data)
	if end < 0 {
		return event.Event{}, 0, false
	}
	sequence := string(data[:end+1])
	if sequence == pasteStart {
		return event.Event{}, len(sequence), true
	}
	if mouse, ok := parseMouse(sequence); ok {
		return event.Event{Kind: event.Mouse, Mouse: mouse}, len(sequence), true
	}
	if focus, ok := map[string]bool{"\x1b[I": true, "\x1b[O": false}[sequence]; ok {
		return event.Event{Kind: event.Focus, Focused: focus}, len(sequence), true
	}
	if key, ok := parseKittyKey(sequence); ok {
		return key, len(sequence), true
	}
	code, modifiers, ok := keySequence(sequence)
	if ok {
		return event.KeyPress(code, modifiers), len(sequence), true
	}
	return event.KeyPress(event.KeyUnknown, 0), len(sequence), true
}

func sequenceEnd(data []byte) int {
	for index := 2; index < len(data) && index <= maxSequence; index++ {
		if data[index] >= 0x40 && data[index] <= 0x7e {
			return index
		}
	}
	return -1
}

func keySequence(sequence string) (event.KeyCode, event.Mod, bool) {
	simple := map[string]event.KeyCode{
		"\x1b[A": event.KeyUp, "\x1b[B": event.KeyDown, "\x1b[C": event.KeyRight, "\x1b[D": event.KeyLeft,
		"\x1b[H": event.KeyHome, "\x1b[F": event.KeyEnd, "\x1b[Z": event.KeyBackTab,
		"\x1bOA": event.KeyUp, "\x1bOB": event.KeyDown, "\x1bOC": event.KeyRight, "\x1bOD": event.KeyLeft,
		"\x1bOH": event.KeyHome, "\x1bOF": event.KeyEnd,
		"\x1bOP": event.KeyF1, "\x1bOQ": event.KeyF2, "\x1bOR": event.KeyF3, "\x1bOS": event.KeyF4,
	}
	if code, ok := simple[sequence]; ok {
		return code, 0, true
	}
	final := sequence[len(sequence)-1]
	body := sequence[2 : len(sequence)-1]
	parts := strings.Split(body, ";")
	modifier := event.Mod(0)
	if len(parts) > 1 {
		value, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil {
			modifier = decodeModifier(value)
		}
	}
	if final != '~' {
		codes := map[byte]event.KeyCode{'A': event.KeyUp, 'B': event.KeyDown, 'C': event.KeyRight, 'D': event.KeyLeft, 'H': event.KeyHome, 'F': event.KeyEnd, 'P': event.KeyF1, 'Q': event.KeyF2, 'R': event.KeyF3, 'S': event.KeyF4}
		code, ok := codes[final]
		return code, modifier, ok
	}
	value, err := strconv.Atoi(parts[0])
	if err != nil {
		return event.KeyUnknown, 0, false
	}
	// The 25-34 range is xterm's shifted function keys; kitty re-sends
	// the same bytes after turning on its protocol, so the table follows
	// kitty's numbering — 29~ is MENU there, where xterm calls it F16 —
	// and the gap at 27 (xterm's Escape) stays empty because an Esc
	// never rides a CSI ~ encoding in practice.
	codes := map[int]event.KeyCode{1: event.KeyHome, 2: event.KeyInsert, 3: event.KeyDelete, 4: event.KeyEnd, 5: event.KeyPageUp, 6: event.KeyPageDown, 7: event.KeyHome, 8: event.KeyEnd, 11: event.KeyF1, 12: event.KeyF2, 13: event.KeyF3, 14: event.KeyF4, 15: event.KeyF5, 17: event.KeyF6, 18: event.KeyF7, 19: event.KeyF8, 20: event.KeyF9, 21: event.KeyF10, 23: event.KeyF11, 24: event.KeyF12, 25: event.KeyF13, 26: event.KeyF14, 28: event.KeyF15, 29: event.KeyMenu, 31: event.KeyF17, 32: event.KeyF18, 33: event.KeyF19, 34: event.KeyF20}
	code, ok := codes[value]
	return code, modifier, ok
}

func decodeModifier(value int) event.Mod {
	if value <= 1 {
		return 0
	}
	bits := value - 1
	var result event.Mod
	if bits&1 != 0 {
		result |= event.ModShift
	}
	if bits&2 != 0 {
		result |= event.ModAlt
	}
	if bits&4 != 0 {
		result |= event.ModCtrl
	}
	if bits&8 != 0 {
		result |= event.ModSuper
	}
	if bits&16 != 0 {
		result |= event.ModHyper
	}
	if bits&32 != 0 {
		result |= event.ModMeta
	}
	if bits&64 != 0 {
		result |= event.ModCapsLock
	}
	if bits&128 != 0 {
		result |= event.ModNumLock
	}
	return result
}

// parseKittyKey decodes one CSI u key event from the kitty keyboard
// protocol's disambiguate and event-type levels — the two flags the
// backend enables. Text and unambiguous keys keep legacy encodings, but
// kitty numbers every key that lacks one (functional keys, media keys,
// keypad keys) inside the Unicode private use area and sends CSI u for
// them even in otherwise legacy modes, so kittyFunctional's table is the
// only thing standing between those codepoints and a private-use rune
// injected into a text field. Codepoints in the block that kitty itself
// leaves unused still decode as unknown.
//
// The event type nests as the second colon sub-parameter of the modifier
// field — CSI 27;1:3u is a release — and some early implementations
// trailed it as a third semicolon field, so both spellings are read and
// only the spec's values 1-3 are honoured: press, auto-repeat, release.
// Enter, Tab, and Backspace keep legacy bytes and never report releases
// at these levels (the spec's shell-safety exception), so a release
// carrying their codes is still decoded honestly rather than trusted.
// The associated-text parameter belongs to a level the backend never
// enables, so its absence is the spec and its presence is ignored.
//
// The query reply shape, CSI ? flags u, is rejected by the
// leading-parameter check and falls through to the unknown-key fallback.
func parseKittyKey(sequence string) (event.Event, bool) {
	if len(sequence) < 4 || sequence[len(sequence)-1] != 'u' {
		return event.Event{}, false
	}
	switch sequence[2] {
	case '?', '<', '>', '=', 0x20:
		return event.Event{}, false
	}
	fields := strings.Split(sequence[2:len(sequence)-1], ";")
	if len(fields) > 4 {
		return event.Event{}, false
	}
	code, ok := kittyNumber(fields[0])
	if !ok {
		return event.Event{}, false
	}
	var mod event.Mod
	if len(fields) > 1 {
		if value, ok := kittyNumber(fields[1]); ok {
			mod = decodeModifier(value)
		}
	}
	action := event.ActionPress
	if len(fields) > 1 {
		eventType, typed := kittyEventType(fields[1])
		if !typed && len(fields) > 2 {
			// The trailing semicolon field is the pre-standard spelling;
			// honour it only when the canonical sub-parameter is absent.
			value, parsed := kittyNumber(fields[2])
			if !parsed {
				// Unparseable trailing field: the 6.1 contract treated that
				// as an unknown key, and a key nobody asked for stays
				// unknown.
				return event.KeyPress(event.KeyUnknown, 0), true
			}
			eventType, typed = value, true
		}
		if typed {
			switch eventType {
			case 1: // press
			case 2: // auto-repeat: another activation, not a second press
				action = event.ActionRepeat
			case 3: // release: decoded in full for callers that opt in
				action = event.ActionRelease
			default:
				return event.KeyPress(event.KeyUnknown, 0), true
			}
		}
	}
	switch code {
	case 27:
		return kittyKey(event.KeyEscape, 0, mod, action), true
	case 13:
		return kittyKey(event.KeyEnter, 0, mod, action), true
	case 127:
		return kittyKey(event.KeyBackspace, 0, mod, action), true
	case 9:
		if mod&event.ModShift != 0 {
			// Shift+Tab normalizes to BackTab without the shift bit so the
			// event matches the legacy CSI Z encoding of the same key.
			return kittyKey(event.KeyBackTab, 0, mod&^event.ModShift, action), true
		}
		return kittyKey(event.KeyTab, 0, mod, action), true
	}
	if code >= 57344 && code <= 63743 {
		// The private use area U+E000..U+F8FF is exactly where the kitty
		// protocol numbers its functional keys. Modifier keys themselves
		// (57441-57454) never arrive as events and stay unknown.
		if functional, r, ok := kittyFunctional(code); ok {
			return kittyKey(functional, r, mod, action), true
		}
		return event.KeyPress(event.KeyUnknown, 0), true
	}
	if code < 32 || code > 0x10FFFF || (code >= 0xD800 && code <= 0xDFFF) {
		return event.KeyPress(event.KeyUnknown, 0), true
	}
	return kittyKey(event.KeyRune, rune(code), mod, action), true
}

// kittyFunctional maps the kitty protocol's functional-key codepoints from
// the Unicode private use area onto plain keys. Kitty sends CSI u for
// these keys even in pure legacy mode, so the mapping is mandatory, not
// an enhancement. Keypad keys normalize to their non-keypad equivalents —
// apps care about the typed character, not which pad produced it — which
// is also why KP_ENTER is Enter and not a distinct code. Codepoints kitty
// leaves unused (the 57344-57357 hole, KP_BEGIN, the trailing half of the
// area) return false and surface as unknown keys.
func kittyFunctional(code int) (event.KeyCode, rune, bool) {
	switch code {
	case 57358:
		return event.KeyCapsLock, 0, true
	case 57359:
		return event.KeyScrollLock, 0, true
	case 57360:
		return event.KeyNumLock, 0, true
	case 57361:
		return event.KeyPrintScreen, 0, true
	case 57362:
		return event.KeyPause, 0, true
	case 57363:
		return event.KeyMenu, 0, true
	case 57409:
		return event.KeyRune, '.', true
	case 57410:
		return event.KeyRune, '/', true
	case 57411:
		return event.KeyRune, '*', true
	case 57412:
		return event.KeyRune, '-', true
	case 57413:
		return event.KeyRune, '+', true
	case 57414:
		return event.KeyEnter, 0, true
	case 57415:
		return event.KeyRune, '=', true
	case 57416:
		return event.KeyRune, ',', true
	case 57417:
		return event.KeyLeft, 0, true
	case 57418:
		return event.KeyRight, 0, true
	case 57419:
		return event.KeyUp, 0, true
	case 57420:
		return event.KeyDown, 0, true
	case 57421:
		return event.KeyPageUp, 0, true
	case 57422:
		return event.KeyPageDown, 0, true
	case 57423:
		return event.KeyHome, 0, true
	case 57424:
		return event.KeyEnd, 0, true
	case 57425:
		return event.KeyInsert, 0, true
	case 57426:
		return event.KeyDelete, 0, true
	case 57428:
		return event.KeyMediaPlay, 0, true
	case 57429:
		return event.KeyMediaPause, 0, true
	case 57430:
		return event.KeyMediaPlayPause, 0, true
	case 57431:
		return event.KeyMediaReverse, 0, true
	case 57432:
		return event.KeyMediaStop, 0, true
	case 57433:
		return event.KeyMediaFastForward, 0, true
	case 57434:
		return event.KeyMediaRewind, 0, true
	case 57435:
		return event.KeyMediaTrackNext, 0, true
	case 57436:
		return event.KeyMediaTrackPrevious, 0, true
	case 57437:
		return event.KeyMediaRecord, 0, true
	case 57438:
		return event.KeyVolumeDown, 0, true
	case 57439:
		return event.KeyVolumeUp, 0, true
	case 57440:
		return event.KeyVolumeMute, 0, true
	}
	switch {
	case code >= 57376 && code <= 57398:
		return event.KeyF13 + event.KeyCode(code-57376), 0, true
	case code >= 57399 && code <= 57408:
		return event.KeyRune, rune('0' + code-57399), true
	}
	return 0, 0, false
}

// kittyKey builds the key event shape every CSI u decode shares. A
// release reports no presses, so its coalesced repeat count is zero. The
// rune stays zero for every functional code; only keypad digits and
// punctuation normalize onto KeyRune with their typed character.
func kittyKey(code event.KeyCode, r rune, mod event.Mod, action event.KeyAction) event.Event {
	repeat := uint16(1)
	if action == event.ActionRelease {
		repeat = 0
	}
	return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: code, Rune: r, Mod: mod, Action: action, Repeat: repeat}}
}

// kittyNumber reads one CSI u parameter, tolerating the colon-separated
// alternate-key sub-parameters a higher protocol level appends: only the
// value before the first colon is the parameter's own number.
func kittyNumber(field string) (int, bool) {
	if cut := strings.IndexByte(field, ':'); cut >= 0 {
		field = field[:cut]
	}
	value, err := strconv.Atoi(field)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// kittyEventType reads the event-type sub-parameter of the CSI u modifier
// field — the canonical wire form nests it after a colon (CSI 97;1:3u) —
// and reports ok=false when the field carries no sub-parameter.
func kittyEventType(field string) (int, bool) {
	parts := strings.SplitN(field, ":", 3)
	if len(parts) < 2 {
		return 0, false
	}
	value, err := strconv.Atoi(parts[1])
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func parseMouse(sequence string) (event.MouseEvent, bool) {
	if !strings.HasPrefix(sequence, "\x1b[<") {
		return event.MouseEvent{}, false
	}
	final := sequence[len(sequence)-1]
	if final != 'M' && final != 'm' {
		return event.MouseEvent{}, false
	}
	parts := strings.Split(sequence[3:len(sequence)-1], ";")
	if len(parts) != 3 {
		return event.MouseEvent{}, false
	}
	button, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || x < 1 || y < 1 || button < 0 || button > 255 {
		return event.MouseEvent{}, false
	}
	mouse := event.MouseEvent{X: x - 1, Y: y - 1}
	if button&4 != 0 {
		mouse.Mod |= event.ModShift
	}
	if button&8 != 0 {
		mouse.Mod |= event.ModAlt
	}
	if button&16 != 0 {
		mouse.Mod |= event.ModCtrl
	}
	base := button & 3
	if button&64 != 0 {
		mouse.Action = event.MouseWheel
		mouse.Button = [...]event.MouseButton{event.MouseWheelUp, event.MouseWheelDown, event.MouseWheelLeft, event.MouseWheelRight}[base]
	} else if final == 'm' || base == 3 {
		mouse.Action = event.MouseRelease
		mouse.Button = event.MouseNone
	} else {
		mouse.Button = [...]event.MouseButton{event.MouseLeft, event.MouseMiddle, event.MouseRight, event.MouseNone}[base]
		if button&32 != 0 {
			mouse.Action = event.MouseMove
		} else {
			mouse.Action = event.MousePress
		}
	}
	return mouse, true
}
