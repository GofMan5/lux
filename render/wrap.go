package render

import (
	"strings"

	"github.com/GofMan5/lux/internal/grapheme"
)

// WrapMode selects where wrapping may break a line that ran out of room.
type WrapMode uint8

const (
	// WrapWord breaks lines at whitespace and moves the whole word that no
	// longer fits to the next line; a word wider than the limit itself
	// falls back to breaking between its graphemes. It is the zero value:
	// the plain WrapOptions{} asks for the convention paragraphs expect.
	WrapWord WrapMode = iota
	// WrapChar breaks between any two graphemes as soon as the next one no
	// longer fits, word boundaries included. Whitespace at the break is
	// kept as cells rather than consumed.
	WrapChar
)

// WrapOptions tunes word wrap. The zero value word-wraps with no
// hyphenation. Hyphenation stays off on purpose: a soft hyphen is a
// zero-width codepoint that terminals may draw or drop, so breaking on it
// would measure one line with two width authorities.
type WrapOptions struct {
	Mode WrapMode
}

// TextMetrics is the measured layout of wrapped text: Width is the widest
// wrapped line in terminal cells and Height is the number of wrapped
// lines. A trailing hard newline produces a final empty line, the way an
// editor counts the line the caret sits on after it.
type TextMetrics struct {
	Width  int
	Height int
}

// WrapLines wraps text to at most limit terminal cells per line under the
// default width policy and returns the wrapped lines. Hard breaks (\n,
// \r\n, and a lone \r, matching TextWidth) always end a line. A limit of
// zero or less disables soft wrapping. Graphemes are never split: a
// width-2 grapheme that would straddle the limit moves to the next line
// and leaves the last cell of its wrapped line empty. Whitespace at a
// soft break is consumed; whitespace before a hard break is content and
// stays. Control graphemes occupy no cells but are preserved in the
// wrapped lines, mirroring Canvas.Text which skips them while drawing:
// every wrapped line therefore measures the same cells it draws. A line
// can exceed the limit only when it is unbreakable: a wide grapheme under
// a one-cell limit, a tab wider than the limit, or indentation wider than
// the limit.
func WrapLines(text string, limit int, opts WrapOptions) []string {
	lines, _ := wrapLines(text, limit, opts, WidthPolicy{})
	return lines
}

// WrapLinesWith is WrapLines under an explicit width policy, so wrap
// decisions match what a canvas with that policy draws.
func WrapLinesWith(text string, limit int, opts WrapOptions, policy WidthPolicy) []string {
	lines, _ := wrapLines(text, limit, opts, policy)
	return lines
}

// MeasureWrap returns the layout of text wrapped to limit cells per line
// under the default width policy, without drawing it.
func MeasureWrap(text string, limit int, opts WrapOptions) TextMetrics {
	_, metrics := wrapLines(text, limit, opts, WidthPolicy{})
	return metrics
}

// MeasureWrapWith is MeasureWrap under an explicit width policy.
func MeasureWrapWith(text string, limit int, opts WrapOptions, policy WidthPolicy) TextMetrics {
	_, metrics := wrapLines(text, limit, opts, policy)
	return metrics
}

// TextWrap wraps text to the width of rect, draws the wrapped lines from
// rect's origin downward, and returns the metrics of the whole wrapped
// text under this canvas's width policy. Drawing stops after rect.H rows
// while Height still counts every wrapped line, so Height > rect.H
// reports clipped overflow. Tabs keep their four-column stops relative to
// the wrapped line's start, the same basis the wrap limit used.
func (c *Canvas) TextWrap(rect Rect, text string, opts WrapOptions, style Style) TextMetrics {
	if c == nil {
		return TextMetrics{}
	}
	lines, metrics := wrapLines(text, rect.W, opts, c.WidthPolicy())
	for i, line := range lines {
		if i >= rect.H {
			break
		}
		c.Text(rect.X, rect.Y+i, line, style)
	}
	return metrics
}

func wrapLines(text string, limit int, opts WrapOptions, policy WidthPolicy) ([]string, TextMetrics) {
	if text == "" {
		return nil, TextMetrics{}
	}
	if opts.Mode == WrapChar {
		return wrapChar(text, limit, policy)
	}
	return wrapWord(text, limit, policy)
}

// wrapWriter holds the line being assembled and the metrics of the lines
// already closed, so word mode, char mode, and word splitting share one
// break discipline.
type wrapWriter struct {
	limit       int
	policy      WidthPolicy
	lines       []string
	line        strings.Builder
	col         int
	pendingHard bool
	metrics     TextMetrics
}

func (w *wrapWriter) closeLine() {
	w.lines = append(w.lines, w.line.String())
	w.line.Reset()
	w.metrics.Width = max(w.metrics.Width, w.col)
	w.metrics.Height++
	w.col = 0
	w.pendingHard = false
}

func (w *wrapWriter) write(s string, cells int) {
	w.line.WriteString(s)
	w.col += cells
}

// writeCell appends one grapheme of the given width, breaking the line
// first when the grapheme no longer fits. A zero-width cluster always
// rides with whatever precedes it, and a fresh line accepts any single
// grapheme even when it is wider than the limit, because a width-2
// cluster under a one-cell limit has nowhere smaller to go.
func (w *wrapWriter) writeCell(grapheme string, width int) {
	if width == 0 {
		w.line.WriteString(grapheme)
		return
	}
	if w.limit > 0 && w.col > 0 && w.col+width > w.limit {
		w.closeLine()
	}
	w.line.WriteString(grapheme)
	w.col += width
}

// cellWidthOf returns the cells one grapheme of wrapped text occupies: the
// policy-aware terminal width when the grapheme will be drawn, zero when
// Canvas.Text would skip it (control graphemes and invalid bytes).
// Measuring with the same skip the draw performs keeps the wrap limit and
// the drawn cells in agreement, and keeping such graphemes in the output
// preserves the input's grapheme-cluster boundaries instead of letting
// neighbors merge around a deletion.
func cellWidthOf(grapheme string, policy WidthPolicy) int {
	if !printable(grapheme) {
		return 0
	}
	return terminalWidthOf(grapheme, policy)
}

// splitRun char-wraps an unbreakable run that is wider than the limit,
// starting at the current column. It is the word-mode fallback for words
// that cannot fit on any line whole.
func (w *wrapWriter) splitRun(run string) {
	graphemes := grapheme.New(run)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		w.writeCell(grapheme, cellWidthOf(grapheme, w.policy))
	}
}

// wsAdvance measures a run of spaces and tabs starting at column col:
// a space advances one cell, a tab to the next four-column stop.
func wsAdvance(col int, ws string) int {
	advance := 0
	for i := 0; i < len(ws); i++ {
		if ws[i] == '\t' {
			step := 4 - col%4
			col += step
			advance += step
		} else {
			col++
			advance++
		}
	}
	return advance
}

// wrapChar breaks between graphemes as soon as the next one no longer
// fits. A tab is one unit: it either fits its whole stop advance or the
// line breaks before it.
func wrapChar(text string, limit int, policy WidthPolicy) ([]string, TextMetrics) {
	w := &wrapWriter{limit: limit, policy: policy}
	graphemes := grapheme.New(text)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch grapheme {
		case "\n", "\r\n", "\r":
			w.closeLine()
			w.pendingHard = true
		case "\t":
			width := 4 - w.col%4
			if w.limit > 0 && w.col > 0 && w.col+width > w.limit {
				w.closeLine()
				width = 4
			}
			w.line.WriteString(grapheme)
			w.col += width
		default:
			// Control graphemes cost no cells and are kept so the line
			// still draws exactly what was measured.
			w.writeCell(grapheme, cellWidthOf(grapheme, policy))
		}
	}
	if w.line.Len() > 0 || w.pendingHard {
		w.closeLine()
	}
	return w.lines, w.metrics
}

// wrapWord breaks at whitespace when a line fills up and consumes the
// whitespace at the break, so a wrapped line never starts with the spaces
// that were spent reaching the margin. Indentation at the start of a
// line is content, never a break opportunity: it is kept, and when it is
// wider than the limit it overflows as its own line the way an
// unbreakable word would.
func wrapWord(text string, limit int, policy WidthPolicy) ([]string, TextMetrics) {
	w := &wrapWriter{limit: limit, policy: policy}
	var (
		word  strings.Builder
		wordW int
		ws    strings.Builder
	)
	flushWS := func() {
		if ws.Len() == 0 {
			return
		}
		w.write(ws.String(), wsAdvance(w.col, ws.String()))
		ws.Reset()
	}
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		run, runW := word.String(), wordW
		word.Reset()
		wordW = 0
		wsStr := ws.String()
		ws.Reset()

		if w.col == 0 {
			// Fresh line: indentation is kept, never consumed.
			wsCells := wsAdvance(0, wsStr)
			if wsCells > 0 {
				w.write(wsStr, wsCells)
				if w.limit > 0 && wsCells >= w.limit {
					// Indentation as wide as the limit fills the line by
					// itself; wider overflows it.
					w.closeLine()
				}
			}
			if w.limit > 0 && w.col+runW > w.limit {
				w.splitRun(run)
			} else {
				w.write(run, runW)
			}
			return
		}

		// The line already has a word, so the pending whitespace is the
		// break opportunity.
		wsCells := wsAdvance(w.col, wsStr)
		if w.limit <= 0 || w.col+wsCells+runW <= w.limit {
			w.write(wsStr, wsCells)
			w.write(run, runW)
			return
		}
		// Soft break: the whitespace at the break is consumed.
		w.closeLine()
		if w.limit > 0 && runW > w.limit {
			w.splitRun(run)
		} else {
			w.write(run, runW)
		}
	}

	graphemes := grapheme.New(text)
	for graphemes.Next() {
		grapheme := graphemes.Str()
		switch grapheme {
		case "\n", "\r\n", "\r":
			flushWord()
			flushWS()
			w.closeLine()
			w.pendingHard = true
		case " ", "\t":
			flushWord()
			ws.WriteString(grapheme)
		default:
			// Control graphemes stay in the word at zero cells, keeping
			// the run's cluster boundaries identical to the input's.
			word.WriteString(grapheme)
			wordW += cellWidthOf(grapheme, policy)
		}
	}
	flushWord()
	flushWS()
	if w.line.Len() > 0 || w.pendingHard {
		w.closeLine()
	}
	return w.lines, w.metrics
}
