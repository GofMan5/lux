package ui_test

import (
	"fmt"
	"strings"

	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
)

// renderRows returns one line of text per canvas row, skipping empty rows and
// trailing blanks. Wide graphemes keep one cell, continuations hold none, so
// the dump stays deterministic on every width policy.
func renderRows(buffer *render.Buffer) []string {
	width, height := buffer.Size()
	var lines []string
	for y := range height {
		var line strings.Builder
		for x := range width {
			if cell, ok := buffer.CellOK(x, y); ok && cell.Grapheme != "" {
				line.WriteString(cell.Grapheme)
			}
		}
		if text := strings.TrimRight(line.String(), " "); text != "" {
			lines = append(lines, text)
		}
	}
	return lines
}

// ExampleContext_Table draws a three-row table with a fixed and a rubber
// column and prints the painted rows. The zero-value TableState is ready: row
// 0 is selected and visible without any setup.
func ExampleContext_Table() {
	buffer := render.NewBuffer(28, 5)
	canvas := render.NewCanvas(buffer)
	rows := [][2]string{
		{"1", "alpha"},
		{"2", "beta"},
		{"3", "gamma"},
	}
	var ctx ui.Context
	state := ui.TableState{Selected: 1}
	ctx.Table("table", canvas, render.Rect{W: 28, H: 5}, &state,
		[]ui.TableColumn{{Title: "ID", Width: 4}, {Title: "NAME"}},
		len(rows),
		func(index int) []string { return rows[index][:] },
		ui.TableStyle{})
	for _, line := range renderRows(buffer) {
		fmt.Println(line)
	}
	// Output:
	// ID   NAME
	// 1    alpha
	// 2    beta
	// 3    gamma
}
