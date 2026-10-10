package terminal

import (
	"strings"
	"testing"

	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/render"
)

// hostileLinks are destinations that try to leave the OSC 8 control string
// they are placed in: spliced string terminators that smuggle escape
// sequences, raw control characters, malformed UTF-8, id fields that spill
// into the URI slot, and oversized payloads. Every one of them must degrade
// to plain text at the encoding seam: the glyph survives, the destination
// never reaches the wire.
var hostileLinks = []struct {
	name string
	link render.Link
}{
	{"uri-splices-st-then-clears-screen", render.Link{URI: "https://example.com/docs\x1b\\\x1b[2J"}},
	{"uri-splices-bel-and-sets-title", render.Link{URI: "https://example.com/\x07\x1b]0;pwned\x07"}},
	{"uri-starts-a-raw-escape", render.Link{URI: "https://example.com/\x1b[?1049h"}},
	{"uri-carries-the-8-bit-st", render.Link{URI: "https://example.com/\u009c\x1b[2K"}},
	{"uri-holds-a-linefeed", render.Link{URI: "https://example.com/\n"}},
	{"uri-holds-a-carriage-return", render.Link{URI: "https://example.com/\r"}},
	{"uri-holds-a-tab", render.Link{URI: "https://example.com/\t"}},
	{"uri-holds-delete", render.Link{URI: "https://example.com/\x7f"}},
	{"uri-holds-a-c1-control", render.Link{URI: "https://example.com/\u0085"}},
	{"uri-is-malformed-utf8", render.Link{URI: "https://example.com/\x80"}},
	{"uri-embeds-a-surrogate-half", render.Link{URI: "https://example.com/\xed\xa0\x80"}},
	{"id-splices-into-the-uri-field", render.Link{ID: "doc;https://evil.example"}},
	{"id-splits-params-into-pairs", render.Link{ID: "doc:id=alt"}},
	{"id-splices-st-then-writes-clipboard", render.Link{ID: "row\x1b\\\x1b]52;c;payload\x07"}},
	{"id-is-malformed-utf8", render.Link{ID: "r\x80ow"}},
	{"uri-exceeds-the-cap", render.Link{URI: "https://example.com/" + strings.Repeat("a", maxLinkURI)}},
	{"id-exceeds-the-cap", render.Link{ID: strings.Repeat("i", maxLinkID+1)}},
}

func TestEncoderRejectsHostileLinkTargets(t *testing.T) {
	for _, hostile := range hostileLinks {
		t.Run(hostile.name, func(t *testing.T) {
			frame := render.NewBuffer(16, 1)
			grid := newReferenceGrid(16, 1)
			grid.links = true
			render.NewCanvas(frame).PutCell(0, 0, render.Cell{Grapheme: "A", Link: hostile.link})
			data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
			if got := strings.Count(data, "\x1b]8;"); got != 0 {
				t.Fatalf("a hostile destination opened %d link regions: %q", got, data)
			}
			if !strings.Contains(data, "A") {
				t.Fatalf("the glyph was dropped along with its link: %q", data)
			}
			for _, smuggled := range []string{"\x07", "\x1b[2J", "\x1b[2K", "\x1b]0;", "\x1b]52;", "\x1b[?1049h", "\u009c"} {
				if strings.Contains(data, smuggled) {
					t.Fatalf("the hostile payload %q reached the wire: %q", smuggled, data)
				}
			}
		})
	}
}

// A hostile destination that replaces a valid one mid-frame must actively
// close the open region: skipping the open is not enough, because the cells
// after it would inherit the valid link they no longer have.
func TestEncoderClosesTheRegionWhenALinkTurnsHostile(t *testing.T) {
	frame := render.NewBuffer(8, 1)
	grid := newReferenceGrid(8, 1)
	grid.links = true
	canvas := render.NewCanvas(frame)
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Link: render.Link{ID: "doc", URI: "https://doc.example"}})
	canvas.PutCell(1, 0, render.Cell{Grapheme: "B", Link: render.Link{ID: "doc;https://evil.example"}})
	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
	if !strings.Contains(data, "A\x1b]8;;\x1b\\B") {
		t.Fatalf("the hostile link must close the open region instead of reopening it: %q", data)
	}
	if got := strings.Count(data, "\x1b]8;"); got != 2 {
		t.Fatalf("got %d OSC 8 sequences, want one open and one close: %q", got, data)
	}
}

// Repainting a cell whose valid link became hostile must not pay for a
// spurious close run after run: the run state tracks the effective link.
func TestEncoderSkipsRedundantClosesForHostileRuns(t *testing.T) {
	frame := render.NewBuffer(8, 1)
	grid := newReferenceGrid(8, 1)
	grid.links = true
	hostile := render.Link{URI: "https://example.com/\x1b[2J"}
	canvas := render.NewCanvas(frame)
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Link: hostile})
	encodeAndVerify(t, grid, frame, nil, capability.TrueColor)

	previous := frame.Clone()
	render.NewCanvas(frame).PutCell(1, 0, render.Cell{Grapheme: "B", Link: hostile})
	data := encodeAndVerify(t, grid, frame, previous, capability.TrueColor)
	if got := strings.Count(data, "\x1b]8;"); got != 0 {
		t.Fatalf("an unchanged hostile link must not emit OSC 8 sequences: %q", data)
	}
	if !strings.Contains(data, "B") {
		t.Fatalf("the new glyph was not transmitted: %q", data)
	}
}

// Legitimate destinations — Unicode URLs, separators that are legal inside
// the URI field, ids that stay within the params field — must keep riding
// OSC 8 unchanged.
func TestEncoderKeepsLegitimateLinkTargets(t *testing.T) {
	unicodeLink := render.Link{ID: "строка-1", URI: "https://例え.テスト/パス?найди=да"}
	separatorLink := render.Link{URI: "https://example.com/a;b?c=1&d=%20#frag"}
	frame := render.NewBuffer(24, 1)
	grid := newReferenceGrid(24, 1)
	grid.links = true
	canvas := render.NewCanvas(frame)
	canvas.PutCell(0, 0, render.Cell{Grapheme: "A", Link: unicodeLink})
	canvas.PutCell(4, 0, render.Cell{Grapheme: "B", Link: separatorLink})
	canvas.PutCell(8, 0, render.Cell{Grapheme: "C"})
	data := encodeAndVerify(t, grid, frame, nil, capability.TrueColor)
	if !strings.Contains(data, "\x1b]8;id=строка-1;https://例え.テスト/パス?найди=да\x1b\\") {
		t.Fatalf("a legitimate Unicode destination lost its link: %q", data)
	}
	if !strings.Contains(data, "\x1b]8;;https://example.com/a;b?c=1&d=%20#frag\x1b\\") {
		t.Fatalf("a URI with separators lost its link: %q", data)
	}
}

// The policy itself: printable runes only, no id separators, caps at the
// documented bounds, and the empty link stays the close form.
func TestSafeLinkPolicy(t *testing.T) {
	safe := []render.Link{
		{},
		{ID: "doc"},
		{ID: "doc-2", URI: "https://example.com"},
		{URI: "https://例え.テスト/パス"},
		{URI: "https://example.com/a;b?c=1&d=%20"},
		{URI: "https://example.com/spaced words"},
		{ID: "ключ=a=b"},
		{URI: "https://example.com/" + strings.Repeat("a", maxLinkURI-20)},
		{ID: strings.Repeat("i", maxLinkID)},
	}
	for _, link := range safe {
		if !safeLink(link) {
			t.Fatalf("a legitimate destination was rejected: %#v", link)
		}
		if effectiveLink(link) != link {
			t.Fatalf("an effective link must be itself: %#v", link)
		}
	}
	unsafe := []render.Link{
		{URI: "https://example.com/\x1b\\\x1b[2J"},
		{URI: "https://example.com/\x07"},
		{URI: "https://example.com/\n"},
		{URI: "https://example.com/\t"},
		{URI: "https://example.com/\r"},
		{URI: "https://example.com/\x7f"},
		{URI: "https://example.com/\u0080"},
		{URI: "https://example.com/\u009c"},
		{URI: "https://example.com/\u200b"},
		{URI: "https://example.com/\x80"},
		{URI: "https://example.com/\xed\xa0\x80"},
		{ID: "doc;alt"},
		{ID: "doc:alt"},
		{ID: "\x07"},
		{ID: "r\x80ow"},
		{URI: "https://example.com/" + strings.Repeat("a", maxLinkURI-19)},
		{ID: strings.Repeat("i", maxLinkID+1)},
	}
	for _, link := range unsafe {
		if safeLink(link) {
			t.Fatalf("a hostile destination was accepted: %#v", link)
		}
		if effectiveLink(link) != (render.Link{}) {
			t.Fatalf("an unsafe link must degrade to no link: %#v", link)
		}
	}
}
