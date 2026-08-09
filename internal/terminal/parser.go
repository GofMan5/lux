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
	case 0x7f, '\b':
		return event.KeyPress(event.KeyBackspace, 0), 1, true
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
	codes := map[int]event.KeyCode{1: event.KeyHome, 2: event.KeyInsert, 3: event.KeyDelete, 4: event.KeyEnd, 5: event.KeyPageUp, 6: event.KeyPageDown, 7: event.KeyHome, 8: event.KeyEnd, 11: event.KeyF1, 12: event.KeyF2, 13: event.KeyF3, 14: event.KeyF4, 15: event.KeyF5, 17: event.KeyF6, 18: event.KeyF7, 19: event.KeyF8, 20: event.KeyF9, 21: event.KeyF10, 23: event.KeyF11, 24: event.KeyF12}
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
	return result
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
