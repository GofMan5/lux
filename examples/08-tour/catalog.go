package main

import (
	"fmt"

	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

var catalogModes = []string{"editable", "read-only", "disabled", "empty"}

// An in-memory widget stand, deliberately not a filesystem editor. Each sample
// keeps its own draft and bounded undo history when another leaf is opened.
type editingCatalog struct {
	tree       ui.TreeState
	modePicker ui.SelectState
	mode       int // committed editor mode; popup navigation is only a preview
	revert     int
	document   int
	drafts     [2]ui.TextArea
	empty      ui.TextArea
	status     string
	geometry   catalogGeometry
}

type catalogGeometry struct {
	tree, modePicker, editor render.Rect
}

func (c *editingCatalog) init() {
	c.tree.Expanded = map[string]bool{"samples": true}
	c.modePicker.PopupRows = len(catalogModes)
	c.drafts[0].MaxBytes, c.drafts[0].UndoBytes = 16<<10, 32<<10
	c.drafts[1].MaxBytes, c.drafts[1].UndoBytes = 16<<10, 32<<10
	c.empty.MaxBytes, c.empty.UndoBytes = 16<<10, 32<<10
	c.drafts[0].SetValue("# In-memory config sample\nname = lux-workbench\nworkers = 4\nregion = 東京\n# Paste multiple lines; Ctrl+Z undoes the edit.")
	c.drafts[1].SetValue("# Widget notes\nTree: Enter toggles a branch or opens a sample.\nSelect: Enter commits; Escape cancels.\nTextArea: text, paste, selection, undo and soft wrap.")
	for i := range c.drafts {
		c.drafts[i].Line, c.drafts[i].Offset = 0, 0
		c.drafts[i].AnchorLine, c.drafts[i].AnchorOffset = 0, 0
	}
	c.status = "In-memory samples only; nothing is written to disk."
}

func (c *editingCatalog) rows() []ui.TreeRow {
	rows := []ui.TreeRow{{ID: "samples", Label: "samples", HasChildren: true}}
	if c.tree.IsExpanded("samples") {
		rows = append(rows,
			ui.TreeRow{ID: "config", Label: "config.lux", Depth: 1},
			ui.TreeRow{ID: "notes", Label: "notes.txt", Depth: 1})
	}
	return rows
}

func (c *editingCatalog) editor() *ui.TextArea {
	if c.mode == 3 {
		return &c.empty
	}
	return &c.drafts[c.document]
}

func (c *editingCatalog) apply(action ui.Action) {
	switch action.Target {
	case "catalog-tree":
		rows := c.rows()
		if _, activated := c.tree.Apply(action, len(rows), func(i int) ui.TreeRow { return rows[i] }); activated {
			switch rows[c.tree.Selected].ID {
			case "config":
				c.document = 0
			case "notes":
				c.document = 1
			}
		}
	case "catalog-mode":
		if action.Kind == ui.Dismiss {
			c.modePicker.Selected, c.modePicker.Open = c.revert, false
			c.status = "Mode cancelled; editor unchanged."
			return
		}
		if !c.modePicker.Open {
			c.revert = c.modePicker.Selected
		}
		if _, committed := c.modePicker.Apply(action, len(catalogModes)); committed {
			c.mode = c.modePicker.Selected
			c.status = "Editor mode: " + catalogModes[c.mode]
		}
	case "catalog-editor":
		if c.mode != 1 && c.mode != 2 {
			c.editor().Apply(action)
		}
	}
}

func (c *editingCatalog) draw(ctx *ui.Context, canvas *render.Canvas, area render.Rect) {
	canvas = canvas.Clip(area)
	if area.W < 32 || area.H < 14 {
		c.geometry = catalogGeometry{}
		canvas.Text(area.X, area.Y, "Editing widgets - enlarge the terminal", render.Style{Foreground: ink, Background: background})
		return
	}
	text := render.Style{Foreground: ink, Background: background}
	label := render.Style{Foreground: ink, Background: background, Attrs: render.AttrDim}
	focus := render.Style{Foreground: background, Background: cyan, Attrs: render.AttrBold | render.AttrUnderline}
	canvas.Text(area.X, area.Y, "Editing widgets", render.Style{Foreground: ink, Background: background, Attrs: render.AttrBold})
	canvas.Text(area.X, area.Y+1, "In-memory samples: Tree / Select / TextArea", label)
	work := render.Rect{X: area.X, Y: area.Y + 3, W: area.W, H: max(area.H-5, 0)}
	var tree, editor render.Rect
	if area.W >= 100 {
		tree = render.Rect{X: work.X, Y: work.Y + 1, W: 28, H: max(work.H-1, 0)}
		editor = render.Rect{X: work.X + 31, Y: work.Y, W: max(work.W-31, 0), H: work.H}
	} else {
		tree = render.Rect{X: work.X, Y: work.Y + 1, W: work.W, H: min(3, max(work.H-5, 0))}
		editor = render.Rect{X: work.X, Y: tree.Bottom() + 1, W: work.W, H: max(work.Bottom()-tree.Bottom()-1, 0)}
	}
	canvas.Text(tree.X, tree.Y-1, "Tree — Enter expands / opens", label)
	rows := c.rows()
	ctx.Tree("catalog-tree", canvas, tree, &c.tree, len(rows), func(i int) ui.TreeRow { return rows[i] }, ui.TreeStyle{
		Normal: text, Selected: render.Style{Foreground: ink, Background: background, Attrs: render.AttrBold}, Focused: focus,
	})
	canvas.Text(editor.X, editor.Y, "Select — editor state", label)
	anchor := render.Rect{X: editor.X, Y: editor.Y + 1, W: min(editor.W, 24), H: 1}
	style := ui.SelectStyle{Normal: text, Focused: focus, Marker: "v", Popup: text, OptionNormal: text,
		OptionSelected: render.Style{Foreground: ink, Background: background, Attrs: render.AttrBold | render.AttrReverse}, OptionFocused: focus}
	if !c.modePicker.Open {
		ctx.Select("catalog-mode", canvas, anchor, &c.modePicker, catalogModes, style)
	}
	name := []string{"config.lux", "notes.txt"}[c.document]
	canvas.Text(editor.X, editor.Y+3, fmt.Sprintf("TextArea — %s (%s)", name, catalogModes[c.mode]), label)
	field := render.Rect{X: editor.X, Y: editor.Y + 4, W: editor.W, H: max(editor.H-4, 0)}
	ctx.TextArea("catalog-editor", canvas, field, c.editor(), ui.TextAreaStyle{
		Normal: text, Cursor: focus, ReadOnly: c.mode == 1, Disabled: c.mode == 2,
	})
	canvas.Text(area.X, area.Bottom()-2, c.status, label)
	canvas.Text(area.X, area.Bottom()-1, "Tab moves focus | Ctrl+Z undo | Ctrl+Y redo | empty: type to start", label)
	c.geometry = catalogGeometry{tree: tree, modePicker: anchor, editor: field}
	// Draw the popup last and scope it exactly like examples/11-select: no
	// duplicate ID, no outside-click-through, and no base-editor caret.
	if c.modePicker.Open {
		placement := ui.SelectPlacement(anchor, &c.modePicker, len(catalogModes), canvas.Bounds().H)
		ctx.BeginModal("catalog-mode", canvas, placement)
		ctx.Select("catalog-mode", canvas, anchor, &c.modePicker, catalogModes, style)
		ctx.EndModal()
	}
}
