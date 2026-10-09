// Command textedit demonstrates the multiline TextArea: soft wrap, caret and
// selection by extended grapheme, bounded undo, sanitized paste, and wheel
// scrolling, with the clipboard port kept in memory.
package main

import (
	"context"
	"fmt"
	"os"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
	"github.com/lux-tui/lux/ui"
	"github.com/rivo/uniseg"
)

var (
	ink        = render.RGBColor(229, 231, 235)
	muted      = render.RGBColor(107, 114, 128)
	violet     = render.RGBColor(139, 92, 246)
	cyan       = render.RGBColor(34, 211, 238)
	background = render.RGBColor(8, 10, 18)
)

// seed shows off grapheme integrity: the e\u0301 combining pair, CJK, and an
// emoji each stay whole under caret moves, wrap, and undo.
const seed = "Lux multiline editor\n" +
	"Caret moves by grapheme: \u00e9\u0301 \u4e16\u754c \U0001f600 stay whole.\n" +
	"Wrap follows the terminal width; edit anywhere and scroll."

type textedit struct {
	width, height int
	ui            ui.Context
	editor        ui.TextArea
	clipboard     memoryClipboard
	observe       chan texteditState
}

type texteditState struct {
	lines int
	line  int
	wrap  render.WrapMode
}

// memoryClipboard keeps the demo hermetic: the same explicit port surface a
// production adapter would implement against OSC 52 or the host clipboard.
type memoryClipboard struct{ text string }

func (c *memoryClipboard) Read(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.text, nil
}

func (c *memoryClipboard) Write(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.text = text
	return nil
}

func newTextedit() *textedit {
	app := &textedit{observe: make(chan texteditState, 16)}
	app.editor.SetValue(seed)
	return app
}

func (app *textedit) Update(next event.Event) lux.Command {
	if action, handled := app.ui.Handle(next); handled {
		switch action.Target {
		case "editor":
			app.editor.Apply(action)
		case "editor-copy":
			_ = app.editor.Copy(context.Background(), &app.clipboard)
		case "editor-cut":
			_ = app.editor.Cut(context.Background(), &app.clipboard)
		case "editor-paste":
			_ = app.editor.Paste(context.Background(), &app.clipboard)
		case "wrap":
			if app.editor.Wrap == render.WrapChar {
				app.editor.Wrap = render.WrapWord
			} else {
				app.editor.Wrap = render.WrapChar
			}
		case "quit":
			return lux.Quit()
		}
		return nil
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
	case event.Key:
		if next.Key.Code == event.KeyEscape {
			return lux.Quit()
		}
	}
	return nil
}

func (app *textedit) View(frame *lux.Frame) {
	canvas := frame.Canvas
	area := canvas.Bounds()
	canvas.Clear(render.Style{Background: background})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'w', Mod: event.ModCtrl}, Owner: "editor", Target: "wrap", Kind: ui.Activate, Scope: ui.ScopeFocused})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'c', Mod: event.ModCtrl}, Owner: "editor", Target: "editor-copy", Kind: ui.Activate, Scope: ui.ScopeFocused})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'x', Mod: event.ModCtrl}, Owner: "editor", Target: "editor-cut", Kind: ui.Activate, Scope: ui.ScopeFocused})
	app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'v', Mod: event.ModCtrl}, Owner: "editor", Target: "editor-paste", Kind: ui.Activate, Scope: ui.ScopeFocused})

	if area.W < 28 || area.H < 8 {
		canvas.Text(0, 0, "LUX textedit - enlarge terminal", render.Style{Foreground: violet})
		app.report()
		return
	}
	canvas.Border(render.Rect{X: 1, Y: 0, W: area.W - 2, H: area.H - 2}, render.RoundedBorder, render.Style{Foreground: violet, Background: background})
	canvas.Text(3, 0, " LUX TEXTEDIT ", render.Style{Foreground: cyan, Background: background, Attrs: render.AttrBold})

	style := ui.TextAreaStyle{
		Normal:    render.Style{Foreground: ink, Background: background},
		Selection: render.Style{Foreground: background, Background: cyan},
		Cursor:    render.Style{Foreground: background, Background: violet},
	}
	body := render.Rect{X: 2, Y: 1, W: area.W - 4, H: area.H - 4}
	app.ui.TextArea("editor", canvas, body, &app.editor, style)

	line := app.editor.Line + 1
	column := uniseg.GraphemeClusterCount(app.editor.Lines[app.editor.Line][:app.editor.Offset]) + 1
	wrapMode := "word"
	if app.editor.Wrap == render.WrapChar {
		wrapMode = "char"
	}
	status := fmt.Sprintf(" Ln %d/%d  Col %d  wrap %s ", line, len(app.editor.Lines), column, wrapMode)
	canvas.Text(2, area.H-2, status, render.Style{Foreground: muted, Background: background})
	footer := " [ctrl+w] wrap  [ctrl+z/alt+u] undo  [ctrl+y] redo  [ctrl+c/x/v] clipboard  [esc] quit "
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " [ctrl+z/alt+u] undo  [esc] quit "
	}
	if canvas.TextWidth(footer) > area.W-2 {
		footer = " [esc] quit "
	}
	canvas.Text(max((area.W-canvas.TextWidth(footer))/2, 0), area.H-1, footer, render.Style{Foreground: muted})
	app.report()
}

func (app *textedit) report() {
	if app.observe == nil {
		return
	}
	select {
	case app.observe <- texteditState{lines: len(app.editor.Lines), line: app.editor.Line, wrap: app.editor.Wrap}:
	default:
	}
}

func main() {
	if err := lux.Run(context.Background(), newTextedit()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
