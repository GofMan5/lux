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
//	Danger disabled rows: the zero-value DisabledStyle of any widget
//
// Only zero-value style fields resolve through the theme; an explicit
// style always wins. Zero-value theme roles resolve through the widget's
// own default, so a deliberately empty role opts a role out.
type Theme struct {
	Text   render.Style
	Muted  render.Style
	Accent render.Style
	Danger render.Style
}

// DefaultTheme returns the built-in palette: terminal-default text, dim
// structure, bold accents, reverse-video disabled. Attributes only — no
// colors — so the default survives monochrome terminals and redirected
// output. It is a function, not a variable: restyling means passing your
// own Theme, never mutating a shared one.
func DefaultTheme() Theme {
	return Theme{
		Text:   render.Style{},
		Muted:  render.Style{Attrs: render.AttrDim},
		Accent: render.Style{Attrs: render.AttrBold},
		Danger: render.Style{Attrs: render.AttrReverse},
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
