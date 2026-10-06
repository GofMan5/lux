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
	got := string(appendFrame(nil, frame, nil, capability.TrueColor, true, false))
	// The trailing blank leaves the terminal at the default style, so the frame
	// needs no closing reset.
	want := "\x1b[1;1H\x1b[0;1;38;2;1;2;3;48;5;42mA界B\x1b[0m "
	if got != want {
		t.Fatalf("full frame\n got: %q\nwant: %q", got, want)
	}

	previous := frame.Clone()
	if got := appendFrame(nil, frame, previous, capability.TrueColor, true, true); len(got) != 0 {
		t.Fatalf("unchanged synchronized frame = %q", got)
	}
	canvas.Put(4, 0, "X", render.Style{})
	got = string(appendFrame(nil, frame, previous, capability.ANSI16, true, true))
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
	if got := appendFrame(nil, frame, previous, capability.ANSI16, true, false); len(got) != 0 {
		t.Fatalf("equivalent downgraded color emitted diff: %q", got)
	}
	if got := appendFrame(nil, frame, previous, capability.TrueColor, true, false); len(got) == 0 {
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
	for _, seed := range [][]byte{[]byte("hello"), []byte("\x1b[A"), []byte("\x1b[<0;1;1M"), []byte(pasteStart + "x" + pasteEnd), {0xff, 0x80, 0x1b}} {
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
	})
}

func TestParserEOFCompletesEscapeAndNUL(t *testing.T) {
	got := newParser().feed([]byte("\x1b[A\x00"), true)
	if len(got) != 2 || !got[0].Key.Is(event.KeyUp, 0) || got[1].Key.Rune != ' ' || got[1].Key.Mod != event.ModCtrl {
		t.Fatalf("EOF events = %#v", got)
	}
}
