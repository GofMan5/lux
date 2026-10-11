package ui

import "github.com/GofMan5/lux/render"

// Theme is the palette a Context resolves zero-value widget styles against.
// It is a value: pass a copy to Context.UseTheme, and nothing in this
// package writes to a Theme it did not create — there is no global mutable
// style table that restyles someone else's application.
//
// Roles:
//
//	Text   body content: table cells, option rows, checkbox and radio rows
//	Muted  structure: headers, unselected tabs, popup frames, empty tracks
//	Accent selection and markers: selected rows and tabs, gauge and
//	       sparkline fills, spinner glyphs
//	Focus  keyboard focus: the focused row, tab, option, or control — a
//	       distinct role so focus stays visible next to a selection
//	       (Accent) even in one color, via attributes
//	Danger disabled rows: the zero-value DisabledStyle of any widget
//
// Only zero-value style fields resolve through the theme; an explicit
// style always wins. Zero-value theme roles resolve through the widget's
// own default, so a deliberately empty role opts a role out.
type Theme struct {
	Text   render.Style
	Muted  render.Style
	Accent render.Style
	Focus  render.Style
	Danger render.Style
}

// DefaultTheme returns the built-in palette: terminal-default text, dim
// structure, bold accents, bold+underlined focus, reverse-video disabled.
// Attributes only — no colors — so the default survives monochrome
// terminals and redirected output. Focus is a full style like every role:
// it repeats Accent's bold and adds an underline, so the focused row stays
// visibly selected and visibly focused in one hue. It is a function, not
// a variable: restyling means passing your own Theme, never mutating a
// shared one.
func DefaultTheme() Theme {
	return Theme{
		Text:   render.Style{},
		Muted:  render.Style{Attrs: render.AttrDim},
		Accent: render.Style{Attrs: render.AttrBold},
		Focus:  render.Style{Attrs: render.AttrBold | render.AttrUnderline},
		Danger: render.Style{Attrs: render.AttrReverse},
	}
}

// Palette is a named color set one screen is built from: the theme
// roles and the explicit chrome and glyph styles all read their colors
// from the same value, so swapping the palette restyles the whole
// screen. Fields keep the vocabulary the reference screens coined —
// ink, muted, violet, cyan, background — not the role names; Theme
// maps them onto the roles. A palette has no Danger color: screens
// that show disabled controls compose that role themselves.
type Palette struct {
	Background render.Color // the canvas fill behind everything
	Ink        render.Color // default text and body chrome
	Muted      render.Color // labels, status lines, structure chrome
	Violet     render.Color // selection fills — the Accent role source
	Cyan       render.Color // focus fills and titles — the Focus role source
}

// ReferencePalette returns the palette the reference applications
// (examples/09-procs and examples/05-widgets) converged on before it
// moved here: ink text and muted chrome on a near-black background,
// violet selection, cyan focus. Like DefaultTheme it is a function, not
// a variable: restyling means passing your own Palette or Theme, never
// mutating a shared one. It is one proven palette, not the start of a
// preset registry — a screen that needs other colors defines its own.
func ReferencePalette() Palette {
	return Palette{
		Background: render.RGBColor(8, 10, 18),
		Ink:        render.RGBColor(229, 231, 235),
		Muted:      render.RGBColor(107, 114, 128),
		Violet:     render.RGBColor(139, 92, 246),
		Cyan:       render.RGBColor(34, 211, 238),
	}
}

// Theme maps the palette onto the semantic roles: Text and Muted paint
// their color on the background, while Accent and Focus paint the
// background color on the palette's fill color — a solid block reads
// as selection and focus better than colored text does. Danger stays
// zero: the palette carries no disabled color, and a zero role is an
// opt-out.
func (p Palette) Theme() Theme {
	return Theme{
		Text:   render.Style{Foreground: p.Ink, Background: p.Background},
		Muted:  render.Style{Foreground: p.Muted, Background: p.Background},
		Accent: render.Style{Foreground: p.Background, Background: p.Violet},
		Focus:  render.Style{Foreground: p.Background, Background: p.Cyan},
	}
}

// UseTheme sets the palette this context resolves zero-value widget styles
// against. Call it any time — app start or mid-run — the next draw sees
// it; routing is untouched. The zero Theme opts every role out, falling
// back to each widget's own default.
func (c *Context) UseTheme(theme Theme) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.theme, c.themeSet = theme, true
}

// Theme returns the active palette, or DefaultTheme if none was set.
func (c *Context) Theme() Theme {
	if c == nil {
		return DefaultTheme()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.themeSet {
		return DefaultTheme()
	}
	return c.theme
}

// themedStyle resolves one widget style field: an explicit non-zero style
// always wins, then the theme role, then the widget's own base. Base is
// the style the widget already computed as its fallback (usually the
// zero Style, sometimes style.Normal).
func themedStyle(explicit, role, base render.Style) render.Style {
	if explicit != (render.Style{}) {
		return explicit
	}
	if role != (render.Style{}) {
		return role
	}
	return base
}
