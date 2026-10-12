// Scene definitions shared by every lane.
//
// A scene is a pure content semantics: given (scene, step, geometry) the
// functions below say what the screen must show. Each adapter renders that
// description through its own framework-idiomatic path, so all lanes draw
// the same logical screen and the comparison measures frameworks, not apps.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/uniseg"
)

// sceneID mirrors the numbered scenes in docs/PERFORMANCE.md.
type sceneID int

const (
	scStatic     sceneID = iota + 1 // 1: static full screen, no changes
	scOneCell                       // 2: one cell flips every step
	scDamage                        // 3: 10% seeded damage per step
	scFull                          // 4: every cell changes every step
	scList                          // 5: 10k-row virtualized list scroll
	scResize                        // 6: resize burst
	scInputBurst                    // 7: 600 mixed keys in 6 batches
	scEGC                           // 8: CJK + emoji + ZWJ grapheme soup
	scRTT                           // 9: 100ms slow link
	scSubcell                       // 10: subcell/3D (lux only)
	scTall                          // 11: unvirtualized tall document (lux only)
)

var sceneNames = [...]string{
	"static", "onecell", "damage", "full", "list",
	"resize", "inputburst", "egc", "rtt", "subcell", "tall",
}

func (s sceneID) name() string   { return sceneNames[s-1] }
func (s sceneID) String() string { return s.name() }

// comparableScenes are the scenes all three lanes can run.
func comparableScenes() []sceneID {
	return []sceneID{
		scStatic, scOneCell, scDamage, scFull, scList,
		scResize, scInputBurst, scEGC, scRTT,
	}
}

func sceneFromName(name string) (sceneID, bool) {
	for index, candidate := range sceneNames {
		if candidate == name {
			return sceneID(index + 1), true
		}
	}
	return 0, false
}

// driveMode selects how the harness feeds the run.
type driveMode int

const (
	drivePaced  driveMode = iota // one key per 16ms tick
	driveBurst                   // batches of keys as fast as possible
	driveResize                  // alternating pty sizes
	driveRTT                     // paced keys against a slow reading link
)

func (s sceneID) drive() driveMode {
	switch s {
	case scInputBurst:
		return driveBurst
	case scResize:
		return driveResize
	case scRTT:
		return driveRTT
	default:
		return drivePaced
	}
}

// Pacing parameters. Quick mode shrinks counts so a smoke run finishes in
// about a minute; the full run documents its own multi-minute budget.
const (
	tickInterval = 16 * time.Millisecond
	pacedTicks   = 300
	warmupTicks  = 100

	burstBatches = 6
	burstKeys    = 100

	resizeTotal = 20
	resizeEvery = 250 * time.Millisecond
	resizeQuiet = 150 * time.Millisecond

	rttStepsFull = 12
	rttQuiet     = 500 * time.Millisecond
	rttLinkDelay = 100 * time.Millisecond

	settleQuiet   = 150 * time.Millisecond
	settleTimeout = 25 * time.Second
	idleWindow    = 2 * time.Second
)

func pacedTickCount(quick bool) int {
	if quick {
		return 60
	}
	return pacedTicks
}

func batchCount(quick bool) int {
	if quick {
		return 2
	}
	return burstBatches
}

func resizeCount(quick bool) int {
	if quick {
		return 8
	}
	return resizeTotal
}

func rttStepCount(quick bool) int {
	if quick {
		return 3
	}
	return rttStepsFull
}

func warmupTickCount(quick bool) int {
	if quick {
		return 20
	}
	return warmupTicks
}

func quietFor(sc sceneID) time.Duration {
	if sc.drive() == driveRTT {
		return rttQuiet
	}
	return settleQuiet
}

// burstKeyCycle is the mixed input every lane must parse: printable runes,
// CSI arrows, a two-byte UTF-8 rune, carriage return, and tab. All lanes
// receive the exact same byte stream via the pty master.
var burstKeyCycle = [][]byte{
	{'j'},
	{'k'},
	{0x1b, '[', 'A'},
	{0x1b, '[', 'B'},
	{0x1b, '[', 'C'},
	{0x1b, '[', 'D'},
	{0xce, 0xb1}, // α
	{'\r'},
	{'x'},
	{'\t'},
}

func burstKey(index int) []byte {
	return burstKeyCycle[index%len(burstKeyCycle)]
}

const fillerText = "the quick brown fox jumps over 0123456789 "

// rowText is the static base row for row-based scenes, clipped to w.
func rowText(y, w int) string {
	s := fmt.Sprintf("row %04d ", y)
	for len(s) < w {
		s += fillerText
	}
	return s[:w]
}

// rowColor is the deterministic truecolor foreground for base row y.
func rowColor(y int) uint32 {
	return uint32(0x30+y%32)<<16 | uint32((y*29)%256)<<8 | uint32((y*67)%256)
}

const listRowCount = 10000

// tallRowCount bounds the deliberately unvirtualized Text stress scene.
const tallRowCount = 160

// listRow is one row of the 10k-row list scene.
func listRow(index, w int) string {
	s := fmt.Sprintf("%06d | ", index)
	for len(s) < w {
		s += fillerText
	}
	return s[:w]
}

// listOffset is the top row shown for a given step (scrolls one row per
// step, wrapping past the end of the list).
func listOffset(step, h int) int {
	if h >= listRowCount {
		return 0
	}
	return step % (listRowCount - h)
}

// egcText mixes wide CJK, ZWJ family emoji, flags, skin tones, and regular
// combining text so grapheme parsing is the story.
const egcText = "汉字你好世界 👨‍👩‍👧‍👦 🇫🇷🇯🇵 🏳️‍🌈 🧑‍🚀 αβγ→λ ﬁ ①②③ "

// egcRow builds a row of cluster soup clipped at grapheme boundaries.
func egcRow(y, w int) string {
	var b strings.Builder
	width := 0
	for width < w {
		g := uniseg.NewGraphemes(egcText)
		for g.Next() {
			if width >= w {
				break
			}
			cluster := g.Str()
			width += uniseg.StringWidth(cluster)
			b.WriteString(cluster)
		}
	}
	return b.String()
}

// egcHighlightRow is the row painted in reverse video for a step.
func egcHighlightRow(step, h int) int {
	return (step * 3) % max(h, 1)
}

// cursorCell is the on-screen column of the moving cursor cell.
func cursorCell(step, w int) int {
	return (step * 7) % max(w, 1)
}

func cellHash(x, y, step int) uint32 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + uint32(step)*974711
	h ^= h >> 13
	return h
}

// cellOverride reports the cell to draw instead of the base row at (x,y).
// has is false when the base row applies.
func cellOverride(sc sceneID, step, x, y, w, h int) (ch string, fg uint32, has bool) {
	switch sc {
	case scOneCell, scRTT:
		if x == w/3 && y == h/3 {
			if step%2 == 0 {
				return "X", 0xff3020, true
			}
			return "O", 0x20ff60, true
		}
	case scDamage:
		if hash := cellHash(x, y, step); hash%10 == 0 {
			n := hash % 26
			return string(rune('A' + n)), uint32(0x70+n*8)<<16 | 0x2020ff | n, true
		}
	case scFull:
		n := uint32((x*7 + y*13 + step) % 26)
		return string(rune('A' + n)), 0x400000 | (uint32(x+step)%256)<<8 | uint32(y+step)%256, true
	case scInputBurst:
		if x == cursorCell(step, w) && y == h/3 {
			return "*", 0xff3030, true
		}
	}
	return "", 0, false
}

// rowScenes are the scenes drawn from row strings.
func isRowScene(sc sceneID) bool {
	switch sc {
	case scStatic, scOneCell, scRTT, scDamage, scList, scResize, scInputBurst:
		return true
	}
	return false
}
