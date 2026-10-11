package ui

import (
	"testing"

	"github.com/GofMan5/lux/render"
)

func TestDefaultThemeIsAttributesOnly(t *testing.T) {
	theme := DefaultTheme()
	if theme.Text != (render.Style{}) {
		t.Fatalf("Text: %#v", theme.Text)
	}
	if !theme.Muted.Has(render.AttrDim) || theme.Muted.Attrs != render.AttrDim {
		t.Fatalf("Muted: %#v", theme.Muted)
	}
	if theme.Accent.Attrs != render.AttrBold {
		t.Fatalf("Accent: %#v", theme.Accent)
	}
	if theme.Danger.Attrs != render.AttrReverse {
		t.Fatalf("Danger: %#v", theme.Danger)
	}
}

func TestThemeDefaultIsContextScoped(t *testing.T) {
	var overridden Context
	custom := Theme{Accent: render.Style{Foreground: render.ANSIColor(3)}}
	overridden.UseTheme(custom)

	var fresh Context
	if got := fresh.Theme(); got != DefaultTheme() {
		t.Fatalf("fresh context theme = %#v, want DefaultTheme", got)
	}
	if got := overridden.Theme().Accent; got != custom.Accent {
		t.Fatalf("overridden Accent = %#v, want %#v", got, custom.Accent)
	}
	if got := fresh.Theme().Accent; got == custom.Accent {
		t.Fatalf("overriding one context leaked into another: %#v", got)
	}
}

func TestThemedStyleResolution(t *testing.T) {
	explicit := render.Style{Foreground: render.ANSIColor(1)}
	role := render.Style{Foreground: render.ANSIColor(2)}
	base := render.Style{Foreground: render.ANSIColor(3)}
	zero := render.Style{}

	if got := themedStyle(explicit, role, base); got != explicit {
		t.Fatalf("explicit should win: %#v", got)
	}
	if got := themedStyle(zero, role, base); got != role {
		t.Fatalf("role should follow explicit: %#v", got)
	}
	if got := themedStyle(zero, zero, base); got != base {
		t.Fatalf("base should follow a zero role: %#v", got)
	}
	if got := themedStyle(zero, zero, zero); got != (render.Style{}) {
		t.Fatalf("all zero must stay zero: %#v", got)
	}
}

func TestThemeGaugeOverride(t *testing.T) {
	custom := Theme{
		Accent: render.Style{Foreground: render.ANSIColor(3)},
		Muted:  render.Style{Foreground: render.ANSIColor(4)},
	}

	draw := func(theme Theme) (fill, track render.Style) {
		var ctx Context
		ctx.UseTheme(theme)
		buffer := render.NewBuffer(10, 1)
		canvas := render.NewCanvas(buffer)
		state := &GaugeState{Value: 5, Max: 10}
		ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, state, GaugeStyle{})
		fillCell, ok := buffer.CellOK(0, 0)
		if !ok {
			t.Fatal("fill cell missing")
		}
		trackCell, ok := buffer.CellOK(6, 0)
		if !ok {
			t.Fatal("track cell missing")
		}
		return fillCell.Style, trackCell.Style
	}

	fill, track := draw(custom)
	if fill != custom.Accent {
		t.Fatalf("themed fill = %#v, want %#v", fill, custom.Accent)
	}
	if track != custom.Muted {
		t.Fatalf("themed track = %#v, want %#v", track, custom.Muted)
	}

	defaultFill, defaultTrack := draw(Theme{})
	if defaultFill != (render.Style{}) || defaultTrack != (render.Style{}) {
		t.Fatalf("zero theme should keep widget defaults: %#v %#v", defaultFill, defaultTrack)
	}
}

func TestThemeExplicitStyleWins(t *testing.T) {
	var ctx Context
	ctx.UseTheme(DefaultTheme())
	buffer := render.NewBuffer(10, 1)
	canvas := render.NewCanvas(buffer)
	explicit := render.Style{Foreground: render.ANSIColor(5)}
	ctx.Gauge(canvas, render.Rect{X: 0, Y: 0, W: 10, H: 1}, &GaugeState{Value: 5, Max: 10}, GaugeStyle{Fill: explicit})
	cell, ok := buffer.CellOK(0, 0)
	if !ok || cell.Style != explicit {
		t.Fatalf("explicit fill = %#v, want %#v", cell.Style, explicit)
	}
}

func TestThemeCheckboxMarkerAccent(t *testing.T) {
	var ctx Context
	buffer := render.NewBuffer(12, 1)
	canvas := render.NewCanvas(buffer)
	ctx.Checkbox("opt-in", canvas, render.Rect{W: 12, H: 1}, &CheckboxState{Checked: true}, "deploy", CheckboxStyle{})

	marker, ok := buffer.CellOK(1, 0) // 'x' inside "[x]"
	if !ok {
		t.Fatal("marker cell missing")
	}
	if !marker.Style.Has(render.AttrBold) {
		t.Fatalf("marker under DefaultTheme should be bold, got %#v", marker.Style)
	}
	label, ok := buffer.CellOK(6, 0) // 'e' in "deploy"
	if !ok {
		t.Fatal("label cell missing")
	}
	if label.Style.Has(render.AttrBold) {
		t.Fatalf("label should stay body text, got %#v", label.Style)
	}
}

func TestThemeTableHeaderAndSelected(t *testing.T) {
	var ctx Context
	buffer := render.NewBuffer(12, 3)
	canvas := render.NewCanvas(buffer)
	columns := []TableColumn{{Title: "name", Width: 12}}
	rows := [][]string{{"alpha"}, {"beta"}}
	rowAt := func(index int) []string { return rows[index] }
	ctx.Table("t", canvas, render.Rect{W: 12, H: 3}, &TableState{Selected: 1}, columns, len(rows), rowAt, TableStyle{})

	header, ok := buffer.CellOK(0, 0)
	if !ok {
		t.Fatal("header cell missing")
	}
	if !header.Style.Has(render.AttrDim) || !header.Style.Has(render.AttrUnderline) {
		t.Fatalf("header under DefaultTheme should be dim+underline, got %#v", header.Style)
	}
	selected, ok := buffer.CellOK(0, 2)
	if !ok {
		t.Fatal("selected row cell missing")
	}
	if !selected.Style.Has(render.AttrBold) {
		t.Fatalf("selected row under DefaultTheme should be bold, got %#v", selected.Style)
	}
	body, ok := buffer.CellOK(0, 1)
	if !ok {
		t.Fatal("body row cell missing")
	}
	if body.Style != (render.Style{}) {
		t.Fatalf("body row under DefaultTheme should stay default, got %#v", body.Style)
	}
}

func TestThemeDefaultPaintsBoldSelectedTabs(t *testing.T) {
	var ctx Context
	buffer := render.NewBuffer(12, 1)
	canvas := render.NewCanvas(buffer)
	state := &TabsState{Selected: 1}
	ctx.Tabs("theme-tabs", canvas, render.Rect{X: 0, Y: 0, W: 12, H: 1}, state, []string{"aa", "bb"}, TabsStyle{})

	cell, ok := buffer.CellOK(5, 0) // middle of "[ bb ]"
	if !ok {
		t.Fatal("selected tab cell missing")
	}
	if !cell.Style.Has(render.AttrBold) {
		t.Fatalf("selected tab under DefaultTheme should be bold, got %#v", cell.Style)
	}
	muted, ok := buffer.CellOK(1, 0) // middle of "[ aa ]"
	if !ok {
		t.Fatal("unselected tab cell missing")
	}
	if !muted.Style.Has(render.AttrDim) {
		t.Fatalf("unselected tab under DefaultTheme should be dim, got %#v", muted.Style)
	}
}

func TestReferencePalettePinsTheSharedScreenColors(t *testing.T) {
	want := Palette{
		Background: render.RGBColor(8, 10, 18),
		Ink:        render.RGBColor(229, 231, 235),
		Muted:      render.RGBColor(107, 114, 128),
		Violet:     render.RGBColor(139, 92, 246),
		Cyan:       render.RGBColor(34, 211, 238),
	}
	if got := ReferencePalette(); got != want {
		t.Fatalf("ReferencePalette() = %#v, want the palette both reference screens run on %#v", got, want)
	}

	theme := ReferencePalette().Theme()
	if theme.Text != (render.Style{Foreground: want.Ink, Background: want.Background}) {
		t.Fatalf("Text role = %#v, want ink on background", theme.Text)
	}
	if theme.Muted != (render.Style{Foreground: want.Muted, Background: want.Background}) {
		t.Fatalf("Muted role = %#v, want muted on background", theme.Muted)
	}
	if theme.Accent != (render.Style{Foreground: want.Background, Background: want.Violet}) {
		t.Fatalf("Accent role = %#v, want background on violet", theme.Accent)
	}
	if theme.Focus != (render.Style{Foreground: want.Background, Background: want.Cyan}) {
		t.Fatalf("Focus role = %#v, want background on cyan", theme.Focus)
	}
	if theme.Danger != (render.Style{}) {
		t.Fatalf("Danger role = %#v, want the zero opt-out: the reference screens show no disabled controls", theme.Danger)
	}
}

func TestPaletteThemeIsAPureRecipe(t *testing.T) {
	// A different palette must map through the same recipe, not the
	// shared constants: selection and focus paint the palette's own
	// background on its fill color, proving Theme() derives from the
	// value it is called on.
	p := Palette{
		Background: render.RGBColor(1, 2, 3),
		Ink:        render.RGBColor(4, 5, 6),
		Muted:      render.RGBColor(7, 8, 9),
		Violet:     render.RGBColor(10, 11, 12),
		Cyan:       render.RGBColor(13, 14, 15),
	}
	theme := p.Theme()
	if theme.Text != (render.Style{Foreground: p.Ink, Background: p.Background}) {
		t.Fatalf("Text recipe = %#v", theme.Text)
	}
	if theme.Muted != (render.Style{Foreground: p.Muted, Background: p.Background}) {
		t.Fatalf("Muted recipe = %#v", theme.Muted)
	}
	if theme.Accent != (render.Style{Foreground: p.Background, Background: p.Violet}) {
		t.Fatalf("Accent recipe = %#v", theme.Accent)
	}
	if theme.Focus != (render.Style{Foreground: p.Background, Background: p.Cyan}) {
		t.Fatalf("Focus recipe = %#v", theme.Focus)
	}
	if theme.Danger != (render.Style{}) {
		t.Fatalf("Danger recipe = %#v, want zero (palette carries no disabled color)", theme.Danger)
	}
}
