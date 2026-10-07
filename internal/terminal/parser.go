package terminal

import (
	"strconv"
	"strings"
	"unicode"
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
	// pixelMouse mirrors the backend's active mouse encoding: mode 1016
	// reports carry pixel coordinates instead of 1-based cell numbers. It
	// stays false on cell terminals, where mouse sequences decode
	// unchanged.
	pixelMouse bool
	// cellW and cellH hold the cell size from the CSI 16 t reply; they
	// stay zero until the reply lands, and pixel reports map to cells
	// only once both are positive.
	cellW, cellH int
	// win32Input mirrors the backend's win32-input-mode latch (DEC private
	// mode 9001): key events arrive as CSI underscore records carrying the
	// full Windows KEY_EVENT_RECORD. It stays false elsewhere, where a '_'
	// final byte is not part of any encoding this backend speaks and its
	// sequences surface as unknown keys.
	win32Input bool
}

// decodeMode is the decode context parseEscape hands its CSI arms: whether
// coordinates arrive as pixels and how large a cell is for the conversion
// (parseMouse and the CSI-t reply arm), and whether the terminal sends
// win32-input-mode records (the CSI-underscore arm).
type decodeMode struct {
	pixel bool
	cellW int
	cellH int
	win32 bool
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
			next, consumed, complete := parseEscape(p.buffer, eof, decodeMode{p.pixelMouse, p.cellW, p.cellH, p.win32Input})
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
				if next.Kind == event.None {
					// The pixel-mode cell-size reply updates the
					// coordinate mapping instead of surfacing as an
					// application event.
					p.absorbCellSize(next)
				} else {
					result = append(result, next)
				}
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
				next, consumed, complete := parseEscape(p.buffer, true, decodeMode{p.pixelMouse, p.cellW, p.cellH, p.win32Input})
				if complete {
					if next.Kind == event.None {
						p.absorbCellSize(next)
					} else {
						result = append(result, next)
					}
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

// absorbCellSize records a cell-size reply instead of surfacing it as an
// event. The CSI 16 t query is only sent while pixel mouse is active, so its
// reply is the parser's own bookkeeping, never an application event.
func (p *parser) absorbCellSize(next event.Event) {
	if next.Width > 0 && next.Height > 0 {
		p.cellW, p.cellH = next.Width, next.Height
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

func parseEscape(data []byte, eof bool, mode decodeMode) (event.Event, int, bool) {
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
			next, consumed, complete := parseEscape(data[1:], eof, mode)
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
	if mouse, ok := parseMouse(sequence, mode); ok {
		return event.Event{Kind: event.Mouse, Mouse: mouse}, len(sequence), true
	}
	if mode.pixel {
		// The cell-size reply to the CSI 16 t query this backend itself
		// sends in pixel mode. It never appears in a cell-mode stream, so
		// the gate keeps junk shaped like a reply visible as an unknown
		// key rather than silently eating it. The reply reports height
		// first, matching the xterm cell-size report form CSI 6 ; h ; w t
		// that answers CSI 16 t.
		if width, height, ok := parseCellSizeReport(sequence); ok {
			return event.Event{Kind: event.None, Width: width, Height: height}, len(sequence), true
		}
	}
	if focus, ok := map[string]bool{"\x1b[I": true, "\x1b[O": false}[sequence]; ok {
		return event.Event{Kind: event.Focus, Focused: focus}, len(sequence), true
	}
	if key, ok := parseKittyKey(sequence); ok {
		return key, len(sequence), true
	}
	if mode.win32 {
		// The win32-input-mode record arm, gated like the cell-size reply:
		// records only arrive on a terminal this backend turned mode 9001
		// on with, so outside the mode a '_' final byte belongs to no
		// encoding this backend speaks and falls through to the unknown-key
		// escape hatch below.
		if key, ok := parseWin32Key(sequence); ok {
			return key, len(sequence), true
		}
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
		return event.KeyRune, rune('0' + code - 57399), true
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

// parseCellSizeReport decodes the CSI 6 ; height ; width t reply to the
// cell-size query CSI 16 t. The report is only meaningful in pixel mode,
// which the caller gates on; here the shape is all that matters.
func parseCellSizeReport(sequence string) (int, int, bool) {
	if !strings.HasPrefix(sequence, "\x1b[6;") || sequence[len(sequence)-1] != 't' {
		return 0, 0, false
	}
	// The "6;" prefix is already stripped: what remains before the final
	// 't' is exactly the height and width.
	parts := strings.Split(sequence[4:len(sequence)-1], ";")
	if len(parts) != 2 {
		return 0, 0, false
	}
	height, err1 := strconv.Atoi(parts[0])
	width, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || height < 1 || width < 1 {
		return 0, 0, false
	}
	return width, height, true
}

// parseMouse decodes SGR mouse sequences. Cell mode (the default) carries
// 1-based cell coordinates: the parser converts them to the zero-based cells
// the event API uses and reports PixelX/PixelY as -1. Pixel mode (kitty
// mode 1016) carries 0-based pixel coordinates instead: they survive
// verbatim in PixelX/PixelY while X/Y hold the cell the pointer is in,
// derived by floor division once the cell-size reply has landed — before
// that, or when the terminal's pointer-leave report fires, X/Y are -1.
func parseMouse(sequence string, mode decodeMode) (event.MouseEvent, bool) {
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
	if err1 != nil || err2 != nil || err3 != nil {
		return event.MouseEvent{}, false
	}
	if !mode.pixel && (x < 1 || y < 1 || button < 0 || button > 255) {
		return event.MouseEvent{}, false
	}
	if mode.pixel && (x < 0 || y < 0 || button < 0 || button > 511) {
		return event.MouseEvent{}, false
	}
	mouse := event.MouseEvent{}
	if mode.pixel {
		// Pixel reports are 0-based; cell coordinates follow from floor
		// division once a positive cell size is known. Until then (and on
		// the pointer-leave sentinel) X and Y stay -1, and hover logic
		// simply misses its hit test.
		mouse.PixelX, mouse.PixelY = x, y
		if mode.cellW > 0 && mode.cellH > 0 {
			mouse.X, mouse.Y = x/mode.cellW, y/mode.cellH
		} else {
			mouse.X, mouse.Y = -1, -1
		}
	} else {
		mouse.X, mouse.Y = x-1, y-1
		mouse.PixelX, mouse.PixelY = -1, -1
	}
	if button&4 != 0 {
		mouse.Mod |= event.ModShift
	}
	if button&8 != 0 {
		mouse.Mod |= event.ModAlt
	}
	if button&16 != 0 {
		mouse.Mod |= event.ModCtrl
	}
	// kitty encodes a pointer leave as 256 | 32 | modifiers, a code the
	// cell-protocol button space cannot reach: the cell guard above
	// already rejected it there, so what survives here is pixel mode,
	// where the code reads as the leave it is. The modifier bits parsed
	// above stay in the event, mirroring kitty's own decode.
	if button >= 256 && button <= 316 {
		mouse.Action = event.MouseMove
		mouse.Button = event.MouseNone
		mouse.X, mouse.Y, mouse.PixelX, mouse.PixelY = -1, -1, -1, -1
		return mouse, true
	}
	base := button & 3
	if button&64 != 0 {
		mouse.Action = event.MouseWheel
		mouse.Button = [...]event.MouseButton{event.MouseWheelUp, event.MouseWheelDown, event.MouseWheelLeft, event.MouseWheelRight}[base]
	} else if button&128 != 0 {
		// Buttons past the classic three: kitty reserves the wheel bit's
		// number space for back/forward buttons, encoded 128-131 with the
		// extra-button bit. The two unused codes of that range (130, 131)
		// stay reserved, so they decode as junk rather than guessing a
		// button that was never sent.
		switch base {
		case 0:
			mouse.Button = event.MouseBack
		case 1:
			mouse.Button = event.MouseForward
		default:
			return event.MouseEvent{}, false
		}
		if final == 'm' {
			mouse.Action = event.MouseRelease
		} else if button&32 != 0 {
			mouse.Action = event.MouseMove
		} else {
			mouse.Action = event.MousePress
		}
	} else if final == 'm' {
		// SGR closes a press with the same button code and the lowercase
		// final byte, so the button survives the release instead of
		// collapsing to none.
		mouse.Action = event.MouseRelease
		mouse.Button = [...]event.MouseButton{event.MouseLeft, event.MouseMiddle, event.MouseRight, event.MouseNone}[base]
	} else if base == 3 {
		// The no-button code with the press final byte is 1003 all-motion
		// hover: xterm reports plain pointer motion as 32|3 = 35, and only
		// the 'm' final byte ever means release.
		mouse.Action = event.MouseMove
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

// parseWin32Key decodes one win32-input-mode record — the DEC private mode
// 9001 encoding Windows terminals send when the backend enables it, the
// Windows counterpart of kitty's CSI u. A record is
//
//	CSI Vk;Sc;Uc;Kd;Cs;Rc _
//
// carrying a KEY_EVENT_RECORD: the virtual key code, the diagnostic scan
// code, the translated UnicodeChar, the key-down flag, the control-key
// state, and the repeat count. Every parameter is optional on the wire:
// an omitted or empty field defaults to zero — except the repeat count,
// which defaults to one, the wire spelling of "this record is one press".
//
// The grammar is deliberately stricter than the kitty arm: parameters are
// plain semicolon-separated numbers, so a record with more than six
// fields, a signed or non-numeric field, a key-down flag outside the
// boolean pair, or a repeat count past the WORD a console can report is
// malformed — charm reads any non-zero key-down as a release, but a flag
// that malformed signals a broken encoder, so lux rejects the record and
// lets it surface as an unknown key rather than guessing.
func parseWin32Key(sequence string) (event.Event, bool) {
	if len(sequence) < 3 || !strings.HasPrefix(sequence, "\x1b[") || sequence[len(sequence)-1] != '_' {
		return event.Event{}, false
	}
	fields := strings.Split(sequence[2:len(sequence)-1], ";")
	if len(fields) > 6 {
		return event.Event{}, false
	}
	// The scan code (second parameter) is diagnostic-only: identity,
	// state, and text all live in the other fields, so it is read for
	// grammar and then ignored.
	values := [6]int{0, 0, 0, 0, 0, 1}
	for i, field := range fields {
		if field == "" {
			continue
		}
		for j := 0; j < len(field); j++ {
			if field[j] < '0' || field[j] > '9' {
				return event.Event{}, false
			}
		}
		value, err := strconv.Atoi(field)
		if err != nil {
			return event.Event{}, false
		}
		values[i] = value
	}
	vk, _, uc, kd, cs, rc := values[0], values[1], values[2], values[3], values[4], values[5]
	if kd != 0 && kd != 1 {
		return event.Event{}, false
	}
	if rc > 0xFFFF {
		return event.Event{}, false
	}
	action, repeat := event.ActionRelease, uint16(0)
	if kd == 1 {
		// A press coalesces the terminal-reported repeat count; a count of
		// zero on a press is a broken record clamped to one, never an
		// event that claims no presses happened.
		action, repeat = event.ActionPress, uint16(max(rc, 1))
	}
	mod := win32Mods(cs)
	// A printable UnicodeChar — including space — is the text path: the
	// rune survives verbatim with its modifiers, mirroring how the kitty
	// arm reports modified text keys (Shift stays, so Shift+A reads as
	// 'A' plus the shift bit).
	if r := rune(uc); uc > 0 && unicode.IsPrint(r) {
		// AltGr arrives as right-alt plus left-ctrl in the control-key
		// state while the character itself is the accented rune. The
		// pair is synthetic, so strip it and report the rune plain — or
		// every European layout would read as Ctrl+Alt+<rune>.
		if cs&0x9 == 0x9 {
			mod &^= event.ModCtrl | event.ModAlt
		}
		return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyRune, Rune: r, Mod: mod, Action: action, Repeat: repeat}}, true
	}
	// UnicodeChar zero or a control character means the key is functional
	// or modifier-composed: the virtual key code carries its identity,
	// exactly what the CSI u code does for kitty.
	code, r, ok := win32VirtualKey(vk)
	if !ok {
		// An unmapped virtual key — a modifier bookend, a windows key, a
		// bare numpad digit — is still a well-formed record, so it decodes
		// as an unknown key carrying the record's own action, repeat, and
		// modifiers instead of falling back to the legacy unknown press:
		// a Ctrl-up bookend reads as a release, never a phantom key.
		return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: event.KeyUnknown, Mod: mod, Action: action, Repeat: repeat}}, true
	}
	// Shift+Tab normalizes to backtab, mirroring the kitty arm and the
	// legacy CSI Z spelling.
	if code == event.KeyTab && mod&event.ModShift != 0 {
		code, mod = event.KeyBackTab, mod&^event.ModShift
	}
	return event.Event{Kind: event.Key, Key: event.KeyEvent{Code: code, Rune: r, Mod: mod, Action: action, Repeat: repeat}}, true
}

// win32Mods maps the control-key state half of a record onto event
// modifiers. Both ctrl positions and both alt positions collapse into
// their single event bit — apps act on "ctrl", not on which hand — and
// caps-lock and num-lock ride along exactly as kitty's modifier field
// reports them. SCROLLLOCK_ON (0x40) and ENHANCED_KEY (0x100) are
// deliberately dropped: the event vocabulary has no scroll-lock bit, and
// the enhanced-key note distinguishes duplicates the VK already names
// (numpad Enter against the main Enter), so neither changes an app-level
// key identity.
func win32Mods(cs int) event.Mod {
	var mod event.Mod
	// RIGHT_CTRL (0x4) and LEFT_CTRL (0x8) both map to the single ctrl
	// bit, so the ctrl mask is 0xC while the alt pair (RIGHT_ALT 0x1,
	// LEFT_ALT 0x2) is 0x3 — mixing halves would read right-alt as ctrl.
	if cs&0xC != 0 {
		mod |= event.ModCtrl
	}
	if cs&0x3 != 0 {
		mod |= event.ModAlt
	}
	if cs&0x10 != 0 {
		mod |= event.ModShift
	}
	if cs&0x80 != 0 {
		mod |= event.ModCapsLock
	}
	if cs&0x20 != 0 {
		mod |= event.ModNumLock
	}
	return mod
}

// win32VirtualKey maps the virtual key code onto the event vocabulary.
// The holes are deliberate: modifier-key records (the VK_SHIFT family,
// 0x10/0x11/0x12/0xA0-0xA5) are bookkeeping this API expresses as Mod
// bits on the keys they modify, so they decode as unknown rather than
// inventing phantom presses; the windows keys (0x5B/0x5C) have no event
// slot, matching kitty's handling of its own modifier codes; and numpad
// digits (0x60-0x69) only reach this table with a zero UnicodeChar —
// num-locked numpad presses arrive as their printable rune on the text
// path, and a bare numpad-digit record is a shape real consoles do not
// send (they report the navigation VKs instead), so it stays unknown
// rather than guessing which digit was meant.
func win32VirtualKey(vk int) (event.KeyCode, rune, bool) {
	switch {
	case vk >= 0x41 && vk <= 0x5A:
		// Letters: the base layout identity, independent of what the
		// console translated it into — Ctrl+A arrives as Uc=1, Alt+A as
		// Uc=0, and both should read as the letter plus their modifiers.
		return event.KeyRune, rune(vk - 0x41 + 'a'), true
	case vk >= 0x30 && vk <= 0x39:
		return event.KeyRune, rune(vk - 0x30 + '0'), true
	case vk >= 0x70 && vk <= 0x87:
		return event.KeyF1 + event.KeyCode(vk-0x70), 0, true
	}
	switch vk {
	case 0x08:
		return event.KeyBackspace, 0, true
	case 0x09:
		return event.KeyTab, 0, true
	case 0x0D:
		return event.KeyEnter, 0, true
	case 0x1B:
		return event.KeyEscape, 0, true
	case 0x13:
		return event.KeyPause, 0, true
	case 0x14:
		return event.KeyCapsLock, 0, true
	case 0x20:
		// Ctrl+Space keeps the console's zero UnicodeChar; the virtual
		// key names the character it is.
		return event.KeyRune, ' ', true
	case 0x21:
		return event.KeyPageUp, 0, true
	case 0x22:
		return event.KeyPageDown, 0, true
	case 0x23:
		return event.KeyEnd, 0, true
	case 0x24:
		return event.KeyHome, 0, true
	case 0x25:
		return event.KeyLeft, 0, true
	case 0x26:
		return event.KeyUp, 0, true
	case 0x27:
		return event.KeyRight, 0, true
	case 0x28:
		return event.KeyDown, 0, true
	case 0x2C:
		return event.KeyPrintScreen, 0, true
	case 0x2D:
		return event.KeyInsert, 0, true
	case 0x2E:
		return event.KeyDelete, 0, true
	case 0x5D:
		return event.KeyMenu, 0, true
	// Numpad operators, num-locked or not: the keys are distinct from
	// their main-row twins on every Windows layout, but the characters
	// they type are the same, so they normalize onto KeyRune exactly as
	// the kitty keypad punctuation does.
	case 0x6A:
		return event.KeyRune, '*', true
	case 0x6B:
		return event.KeyRune, '+', true
	case 0x6C:
		return event.KeyRune, ',', true
	case 0x6D:
		return event.KeyRune, '-', true
	case 0x6E:
		return event.KeyRune, '.', true
	case 0x6F:
		return event.KeyRune, '/', true
	case 0xAD:
		return event.KeyVolumeMute, 0, true
	case 0xAE:
		return event.KeyVolumeDown, 0, true
	case 0xAF:
		return event.KeyVolumeUp, 0, true
	case 0xB0:
		return event.KeyMediaTrackNext, 0, true
	case 0xB1:
		return event.KeyMediaTrackPrevious, 0, true
	case 0xB2:
		return event.KeyMediaStop, 0, true
	case 0xB3:
		return event.KeyMediaPlayPause, 0, true
	}
	return 0, 0, false
}
