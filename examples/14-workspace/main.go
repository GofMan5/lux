// Command workspace is a small project/config reference flow. It reads a real
// directory through one seam, validates config.lux before save, previews the
// draft, protects unsaved edits, and writes through a same-directory replace.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

type workspacePhase uint8

const (
	workspaceLoading workspacePhase = iota
	workspaceReady
	workspaceEmpty
	workspaceError
)

func (p workspacePhase) String() string {
	switch p {
	case workspaceLoading:
		return "loading"
	case workspaceReady:
		return "ready"
	case workspaceEmpty:
		return "empty"
	case workspaceError:
		return "error"
	default:
		return "unknown"
	}
}

type workspaceFile struct{ Name string }

type workspaceSource interface {
	List(context.Context) ([]workspaceFile, error)
	Read(context.Context, string) (string, error)
	Write(context.Context, string, string) error
}

type fsWorkspaceSource struct{ root string }

func (s fsWorkspaceSource) List(ctx context.Context) ([]workspaceFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	files := make([]workspaceFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		files = append(files, workspaceFile{Name: entry.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func (s fsWorkspaceSource) Read(ctx context.Context, name string) (string, error) {
	path, err := s.path(name)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	contents, err := os.ReadFile(path)
	return string(contents), err
}

func (s fsWorkspaceSource) Write(ctx context.Context, name, contents string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return atomicWrite(path, []byte(contents))
}

func (s fsWorkspaceSource) path(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("workspace file name is invalid")
	}
	return filepath.Join(s.root, name), nil
}

// atomicWrite stages beside the target and renames it into place. Windows
// cannot replace an existing file with os.Rename, so the fallback swaps a
// backup and restores it if the second rename fails.
func atomicWrite(path string, contents []byte) (err error) {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".lux-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err == nil {
		return nil
	}

	backup := path + ".lux-backup"
	_ = os.Remove(backup)
	if err = os.Rename(path, backup); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	if removeErr := os.Remove(backup); removeErr != nil {
		return removeErr
	}
	return nil
}

type workspaceListResult struct {
	files []workspaceFile
	err   error
	id    int
}

type workspaceReadResult struct {
	name string
	text string
	err  error
	id   int
}

type workspaceWriteResult struct {
	name string
	err  error
	id   int
}

type workspaceApp struct {
	width, height int
	ui            ui.Context
	source        workspaceSource

	phase   workspacePhase
	notice  string
	files   []workspaceFile
	file    string
	editor  ui.TextArea
	dirty   bool
	preview bool
	saving  bool
	request int

	tree        ui.TreeState
	ask         bool
	pendingFile string
	pendingQuit bool
}

func newWorkspaceApp(source workspaceSource) *workspaceApp {
	app := &workspaceApp{source: source, phase: workspaceLoading}
	app.editor.MaxBytes, app.editor.UndoBytes = 64<<10, 64<<10
	app.tree.Expanded = map[string]bool{"workspace": true}
	return app
}

func (app *workspaceApp) Update(next event.Event) lux.Command {
	action, handled := app.ui.Handle(next)
	if handled {
		switch action.Target {
		case "workspace-save":
			return app.save()
		case "workspace-preview":
			if !app.saving && app.phase == workspaceReady {
				app.preview = !app.preview
			}
		case "workspace-reload":
			return app.startList()
		case "workspace-quit":
			if app.dirty {
				app.ask, app.pendingQuit = true, true
				return nil
			}
			return lux.Quit()
		case "workspace-unsaved-discard":
			return app.discardUnsaved()
		case "workspace-unsaved-keep", "workspace-unsaved":
			app.ask, app.pendingQuit, app.pendingFile = false, false, ""
		case "workspace-tree":
			return app.applyTree(action)
		case "workspace-editor":
			if !app.saving && app.phase == workspaceReady && app.editor.Apply(action) {
				app.dirty = true
			}
		}
		return nil
	}

	// This fallback only covers an input racing the first modal frame. Once a
	// modal is committed, routing owns these keys like every other shortcut.
	if app.ask && next.Kind == event.Key && next.Key.Action != event.ActionRelease {
		switch strings.ToLower(string(next.Key.Rune)) {
		case "n":
			app.ask, app.pendingQuit, app.pendingFile = false, false, ""
		case "y":
			return app.discardUnsaved()
		}
	}
	switch next.Kind {
	case event.Resize:
		app.width, app.height = next.Width, next.Height
		if app.phase == workspaceLoading && app.request == 0 {
			return app.startList()
		}
	case event.Message:
		switch result := next.Data.(type) {
		case workspaceListResult:
			if result.id != app.request {
				return nil
			}
			if result.err != nil {
				app.phase, app.notice = workspaceError, result.err.Error()
				return nil
			}
			app.files = append(app.files[:0], result.files...)
			if len(app.files) == 0 {
				app.phase, app.notice = workspaceEmpty, "No files in this workspace."
				return nil
			}
			app.phase, app.notice = workspaceLoading, ""
			app.file = app.files[0].Name
			return app.startRead(app.file)
		case workspaceReadResult:
			if result.id != app.request || result.name != app.file {
				return nil
			}
			if result.err != nil {
				app.phase, app.notice = workspaceError, result.err.Error()
				return nil
			}
			app.editor.SetValue(result.text)
			app.dirty, app.preview, app.saving = false, false, false
			app.phase, app.notice = workspaceReady, "Loaded "+app.file
		case workspaceWriteResult:
			if result.id != app.request || result.name != app.file {
				return nil
			}
			app.saving = false
			if result.err != nil {
				app.notice = "Save failed: " + result.err.Error()
				return nil
			}
			app.dirty, app.notice = false, "Saved "+app.file
		}
	}
	return nil
}

func (app *workspaceApp) startList() lux.Command {
	if app.source == nil {
		app.phase, app.notice = workspaceError, "No workspace source configured."
		return nil
	}
	app.request++
	id, source := app.request, app.source
	app.phase, app.notice = workspaceLoading, "Loading project files..."
	return func(ctx context.Context) event.Event {
		files, err := source.List(ctx)
		return event.Event{Kind: event.Message, Data: workspaceListResult{files: files, err: err, id: id}}
	}
}

func (app *workspaceApp) startRead(name string) lux.Command {
	app.request++
	id, source := app.request, app.source
	app.phase, app.notice = workspaceLoading, "Loading "+name+"..."
	return func(ctx context.Context) event.Event {
		text, err := source.Read(ctx, name)
		return event.Event{Kind: event.Message, Data: workspaceReadResult{name: name, text: text, err: err, id: id}}
	}
}

func (app *workspaceApp) save() lux.Command {
	if app.ask || app.saving || app.phase != workspaceReady || app.preview || app.file == "" {
		return nil
	}
	if err := validateWorkspaceFile(app.file, textAreaValue(&app.editor)); err != nil {
		app.notice = "Validation failed: " + err.Error()
		return nil
	}
	app.request++
	id, name, text, source := app.request, app.file, textAreaValue(&app.editor), app.source
	app.saving, app.notice = true, "Saving "+name+"..."
	return func(ctx context.Context) event.Event {
		err := source.Write(ctx, name, text)
		return event.Event{Kind: event.Message, Data: workspaceWriteResult{name: name, err: err, id: id}}
	}
}

func (app *workspaceApp) applyTree(action ui.Action) lux.Command {
	rows := app.rows()
	_, activated := app.tree.Apply(action, len(rows), func(i int) ui.TreeRow { return rows[i] })
	if !activated || app.tree.Selected < 0 || app.tree.Selected >= len(rows) || rows[app.tree.Selected].HasChildren {
		return nil
	}
	name := rows[app.tree.Selected].ID
	if name == app.file || app.phase != workspaceReady {
		return nil
	}
	if app.dirty {
		app.ask, app.pendingFile = true, name
		return nil
	}
	app.file = name
	return app.startRead(name)
}

func (app *workspaceApp) discardUnsaved() lux.Command {
	app.dirty, app.ask = false, false
	if app.pendingQuit {
		app.pendingQuit = false
		return lux.Quit()
	}
	name := app.pendingFile
	app.pendingFile = ""
	if name == "" {
		return nil
	}
	app.file = name
	return app.startRead(name)
}

func (app *workspaceApp) rows() []ui.TreeRow {
	rows := []ui.TreeRow{{ID: "workspace", Label: "workspace", HasChildren: true}}
	if app.tree.IsExpanded("workspace") {
		for _, file := range app.files {
			rows = append(rows, ui.TreeRow{ID: file.Name, Label: file.Name, Depth: 1})
		}
	}
	return rows
}

func (app *workspaceApp) View(frame *lux.Frame) {
	canvas := frame.Canvas
	canvas.Clear(render.Style{Background: render.RGBColor(8, 10, 18)})
	app.ui.Begin(frame)
	defer app.ui.End(frame)
	if !app.ask {
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyEscape}, Target: "workspace-quit", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 's', Mod: event.ModCtrl}, Target: "workspace-save", Kind: ui.Activate, Scope: ui.ScopeApp, Priority: true})
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'p', Mod: event.ModCtrl}, Target: "workspace-preview", Kind: ui.Activate, Scope: ui.ScopeApp})
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'r', Mod: event.ModCtrl}, Target: "workspace-reload", Kind: ui.Activate, Scope: ui.ScopeApp})
	} else {
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'y'}, Target: "workspace-unsaved-discard", Kind: ui.Activate, Scope: ui.ScopeModal, Owner: "workspace-unsaved"})
		app.ui.Shortcut(ui.Shortcut{Key: event.KeyEvent{Code: event.KeyRune, Rune: 'n'}, Target: "workspace-unsaved-keep", Kind: ui.Activate, Scope: ui.ScopeModal, Owner: "workspace-unsaved"})
	}

	ink := render.Style{Foreground: render.RGBColor(229, 231, 235), Background: render.RGBColor(8, 10, 18)}
	muted := render.Style{Foreground: render.RGBColor(156, 163, 175), Background: render.RGBColor(8, 10, 18)}
	accent := render.Style{Foreground: render.RGBColor(8, 10, 18), Background: render.RGBColor(34, 211, 238), Attrs: render.AttrBold}
	canvas.Text(2, 1, "Project workspace", render.Style{Foreground: render.RGBColor(34, 211, 238), Background: render.RGBColor(8, 10, 18), Attrs: render.AttrBold})
	canvas.Text(2, 2, "filesystem-backed config editor", muted)
	if canvas.Bounds().W < 48 || canvas.Bounds().H < 12 {
		canvas.Text(2, 4, "Enlarge terminal to edit workspace", ink)
		return
	}
	if app.phase == workspaceLoading {
		canvas.Text(3, 6, app.notice, ink)
		canvas.Text(3, 8, "Ctrl+R retry", muted)
		return
	}
	if app.phase == workspaceError {
		canvas.Text(3, 5, "Project load failed", render.Style{Foreground: render.RGBColor(248, 113, 113), Background: render.RGBColor(8, 10, 18), Attrs: render.AttrBold})
		canvas.Text(3, 7, app.notice, ink)
		canvas.Text(3, 9, "Ctrl+R retry", muted)
		return
	}
	if app.phase == workspaceEmpty {
		canvas.Text(3, 6, "No project files found", ink)
		canvas.Text(3, 8, "Ctrl+R retry", muted)
		return
	}

	left := render.Rect{X: 2, Y: 4, W: 22, H: max(canvas.Bounds().H-8, 1)}
	right := render.Rect{X: 26, Y: 4, W: max(canvas.Bounds().W-28, 1), H: max(canvas.Bounds().H-8, 1)}
	canvas.Text(left.X, left.Y-1, "Files", muted)
	rows := app.rows()
	app.ui.Tree("workspace-tree", canvas, left, &app.tree, len(rows), func(i int) ui.TreeRow { return rows[i] }, ui.TreeStyle{Normal: ink, Selected: render.Style{Foreground: render.RGBColor(34, 211, 238), Background: render.RGBColor(8, 10, 18), Attrs: render.AttrBold}, Focused: accent})
	name := app.file
	if app.dirty {
		name += " *"
	}
	canvas.Text(right.X, right.Y-1, name, muted)
	if app.preview {
		canvas.Text(right.X, right.Y+1, "Preview (unsaved)", accent)
		for i, line := range strings.Split(textAreaValue(&app.editor), "\n") {
			if right.Y+2+i >= right.Bottom() {
				break
			}
			canvas.Text(right.X, right.Y+2+i, line, ink)
		}
	} else {
		app.ui.TextArea("workspace-editor", canvas, right, &app.editor, ui.TextAreaStyle{Normal: ink, Cursor: accent, Disabled: app.saving})
	}
	status := app.notice
	if app.dirty && status == "" {
		status = "Unsaved changes"
	}
	canvas.Text(2, canvas.Bounds().H-3, status, muted)
	canvas.Text(2, canvas.Bounds().H-2, "Tab focus  Ctrl+S save  Ctrl+P preview  Esc quit", muted)
	if app.ask {
		modal := render.Rect{X: 8, Y: canvas.Bounds().H/2 - 2, W: canvas.Bounds().W - 16, H: 5}
		app.ui.BeginModal("workspace-unsaved", canvas, modal)
		canvas.Text(modal.X+2, modal.Y+1, "Unsaved changes", render.Style{Foreground: render.RGBColor(248, 113, 113), Background: render.RGBColor(8, 10, 18), Attrs: render.AttrBold})
		canvas.Text(modal.X+2, modal.Y+2, "Discard them? [y] discard  [n] keep editing", ink)
		app.ui.EndModal()
	}
}

func textAreaValue(editor *ui.TextArea) string {
	if editor == nil {
		return ""
	}
	return strings.Join(editor.Lines, "\n")
}

func validateWorkspaceFile(name, text string) error {
	if name != "config.lux" {
		return nil
	}
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("line must use key = value")
		}
		values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if values["name"] == "" {
		return errors.New("name is required")
	}
	workers, err := strconv.Atoi(values["workers"])
	if err != nil || workers < 1 {
		return errors.New("workers must be a positive integer")
	}
	return nil
}

func main() {
	root := os.Getenv("LUX_WORKSPACE_ROOT")
	if root == "" {
		root = "."
	}
	app := newWorkspaceApp(fsWorkspaceSource{root: root})
	if err := lux.Run(context.Background(), app); err != nil {
		fmt.Fprintln(os.Stderr, "workspace:", err)
		os.Exit(1)
	}
}
