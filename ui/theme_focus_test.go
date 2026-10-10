package ui

import (
	"testing"

	"github.com/GofMan5/lux/render"
)

// focusTestTheme keeps every role distinct so a cell style identifies the
// role that produced it: Text=2, Accent=3, Focus=6.
var focusTestTheme = Theme{
	Text:   render.Style{Foreground: render.ANSIColor(2)},
	Accent: render.Style{Foreground: render.ANSIColor(3)},
	Focus:  render.Style{Foreground: render.ANSIColor(6)},
}

// TestThemeFocusRoleAcrossControls pins the stage-3 contract: a focused
// control resolves its zero-value Focused style through the theme's Focus
// role, and stays distinguishable from a merely selected one. Zero-value
// style fields resolve through the theme; an explicit style always wins.
func TestThemeFocusRoleAcrossControls(t *testing.T) {
	drawTable := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		rows := [][]string{{"alpha"}, {"beta"}}
		c.Table("t", canvas, render.Rect{W: 12, H: 3}, &TableState{Selected: 1},
			[]TableColumn{{Title: "name", Width: 12}}, len(rows),
			func(i int) []string { return rows[i] }, TableStyle{})
	}
	drawTabs := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.Tabs("t", canvas, render.Rect{W: 12, H: 1}, &TabsState{Selected: 1},
			[]string{"aa", "bb"}, TabsStyle{})
	}
	drawSelect := func(open bool) func(*Context, *render.Buffer) {
		return func(c *Context, buffer *render.Buffer) {
			canvas := render.NewCanvas(buffer)
			c.Select("t", canvas, render.Rect{W: 12, H: 1}, &SelectState{Open: open},
				[]string{"aa", "bb"}, SelectStyle{})
		}
	}
	drawTree := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.Tree("t", canvas, render.Rect{W: 12, H: 2}, &TreeState{}, 2,
			func(index int) TreeRow { return TreeRow{ID: "row"} }, TreeStyle{})
	}
	drawRadio := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.RadioGroup("t", canvas, render.Rect{W: 12, H: 2}, &RadioState{},
			[]string{"one", "two"}, RadioStyle{})
	}
	drawList := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.List("t", canvas, render.Rect{W: 12, H: 2}, &ListState{},
			[]string{"one", "two"}, ListStyle{})
	}
	drawButton := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.Button("t", canvas, render.Rect{W: 10, H: 3}, "ok", ButtonStyle{})
	}
	drawCheckbox := func(c *Context, buffer *render.Buffer) {
		canvas := render.NewCanvas(buffer)
		c.Checkbox("t", canvas, render.Rect{W: 12, H: 1}, &CheckboxState{}, "label", CheckboxStyle{})
	}

	cases := []struct {
		name     string
		focused  bool
		x, y     int
		expected render.Style
		draw     func(c *Context, buffer *render.Buffer)
	}{
		{name: "table selected row", focused: true, x: 0, y: 2, expected: focusTestTheme.Focus, draw: drawTable},
		{name: "table selected row without focus", focused: false, x: 0, y: 2, expected: focusTestTheme.Accent, draw: drawTable},
		{name: "tabs selected tab", focused: true, x: 6, y: 0, expected: focusTestTheme.Focus, draw: drawTabs},
		{name: "tabs selected tab without focus", focused: false, x: 6, y: 0, expected: focusTestTheme.Accent, draw: drawTabs},
		{name: "select closed field", focused: true, x: 1, y: 0, expected: focusTestTheme.Focus, draw: drawSelect(false)},
		{name: "select closed field without focus", focused: false, x: 1, y: 0, expected: focusTestTheme.Text, draw: drawSelect(false)},
		{name: "select open option", focused: true, x: 1, y: 1, expected: focusTestTheme.Focus, draw: drawSelect(true)},
		{name: "select open option without focus", focused: false, x: 1, y: 1, expected: focusTestTheme.Accent, draw: drawSelect(true)},
		{name: "tree selected row", focused: true, x: 0, y: 0, expected: focusTestTheme.Focus, draw: drawTree},
		{name: "tree selected row without focus", focused: false, x: 0, y: 0, expected: focusTestTheme.Accent, draw: drawTree},
		{name: "radio selected row", focused: true, x: 0, y: 0, expected: focusTestTheme.Focus, draw: drawRadio},
		{name: "radio selected row without focus", focused: false, x: 0, y: 0, expected: focusTestTheme.Accent, draw: drawRadio},
		{name: "list selected row", focused: true, x: 0, y: 0, expected: focusTestTheme.Focus, draw: drawList},
		{name: "list selected row without focus", focused: false, x: 0, y: 0, expected: focusTestTheme.Accent, draw: drawList},
		{name: "button", focused: true, x: 0, y: 1, expected: focusTestTheme.Focus, draw: drawButton},
		{name: "button without focus", focused: false, x: 0, y: 1, expected: focusTestTheme.Text, draw: drawButton},
		{name: "checkbox row", focused: true, x: 5, y: 0, expected: focusTestTheme.Focus, draw: drawCheckbox},
		{name: "checkbox row without focus", focused: false, x: 5, y: 0, expected: focusTestTheme.Text, draw: drawCheckbox},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ctx Context
			ctx.begun = true
			ctx.UseTheme(focusTestTheme)
			// The first focusable region of a frame auto-receives focus, so
			// the unfocused variants must park it on another id rather than
			// leave it empty.
			if tc.focused {
				ctx.viewFocus = "t"
			} else {
				ctx.viewFocus = "other"
			}
			buffer := render.NewBuffer(12, 3)
			tc.draw(&ctx, buffer)
			cell, ok := buffer.CellOK(tc.x, tc.y)
			if !ok {
				t.Fatalf("cell (%d,%d) missing", tc.x, tc.y)
			}
			if cell.Style != tc.expected {
				t.Fatalf("cell (%d,%d) style = %#v, want %#v", tc.x, tc.y, cell.Style, tc.expected)
			}
		})
	}
}

// TestDefaultThemeFocusIsVisible pins the mono-safe default: a focused row
// stays distinguishable from a selected one without any color.
func TestDefaultThemeFocusIsVisible(t *testing.T) {
	draw := func(focused bool) render.Style {
		var ctx Context
		ctx.begun = true
		if focused {
			ctx.viewFocus = "t"
		} else {
			ctx.viewFocus = "other"
		}
		buffer := render.NewBuffer(12, 3)
		canvas := render.NewCanvas(buffer)
		rows := [][]string{{"alpha"}, {"beta"}}
		ctx.Table("t", canvas, render.Rect{W: 12, H: 3}, &TableState{Selected: 1},
			[]TableColumn{{Title: "name", Width: 12}}, len(rows),
			func(i int) []string { return rows[i] }, TableStyle{})
		cell, ok := buffer.CellOK(0, 2)
		if !ok {
			t.Fatal("selected row cell missing")
		}
		return cell.Style
	}

	focused, unfocused := draw(true), draw(false)
	if focused == unfocused {
		t.Fatalf("default theme must keep focus visible: focused %#v == selected %#v", focused, unfocused)
	}
	if !focused.Has(render.AttrUnderline) {
		t.Fatalf("focused row should underline, got %#v", focused)
	}
	if !focused.Has(render.AttrBold) || !unfocused.Has(render.AttrBold) {
		t.Fatalf("selection should stay bold: focused %#v, unfocused %#v", focused, unfocused)
	}
}

// TestThemeZeroFocusKeepsSelectedVisual pins compatibility: a theme without
// a Focus role keeps today's rendering — the focused row falls back to the
// selected visual.
func TestThemeZeroFocusKeepsSelectedVisual(t *testing.T) {
	theme := Theme{Accent: render.Style{Foreground: render.ANSIColor(3)}}
	var ctx Context
	ctx.begun = true
	ctx.viewFocus = "t"
	ctx.UseTheme(theme)
	buffer := render.NewBuffer(12, 3)
	canvas := render.NewCanvas(buffer)
	rows := [][]string{{"alpha"}, {"beta"}}
	ctx.Table("t", canvas, render.Rect{W: 12, H: 3}, &TableState{Selected: 1},
		[]TableColumn{{Title: "name", Width: 12}}, len(rows),
		func(i int) []string { return rows[i] }, TableStyle{})
	cell, ok := buffer.CellOK(0, 2)
	if !ok {
		t.Fatal("selected row cell missing")
	}
	if cell.Style != theme.Accent {
		t.Fatalf("focused row without a Focus role = %#v, want Accent %#v", cell.Style, theme.Accent)
	}
}

// TestThemeExplicitFocusedWinsOverFocusRole pins explicit-style-wins for the
// new role.
func TestThemeExplicitFocusedWinsOverFocusRole(t *testing.T) {
	var ctx Context
	ctx.begun = true
	ctx.viewFocus = "t"
	ctx.UseTheme(focusTestTheme)
	explicit := render.Style{Foreground: render.ANSIColor(5)}
	buffer := render.NewBuffer(12, 3)
	canvas := render.NewCanvas(buffer)
	rows := [][]string{{"alpha"}, {"beta"}}
	ctx.Table("t", canvas, render.Rect{W: 12, H: 3}, &TableState{Selected: 1},
		[]TableColumn{{Title: "name", Width: 12}}, len(rows),
		func(i int) []string { return rows[i] }, TableStyle{Focused: explicit})
	cell, ok := buffer.CellOK(0, 2)
	if !ok {
		t.Fatal("selected row cell missing")
	}
	if cell.Style != explicit {
		t.Fatalf("explicit Focused = %#v, want %#v", cell.Style, explicit)
	}
}
