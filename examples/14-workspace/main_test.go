package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

type scriptedWorkspace struct {
	files    []workspaceFile
	content  map[string]string
	listErr  error
	readErr  error
	writeErr error
	writes   map[string]string
}

func (s *scriptedWorkspace) List(context.Context) ([]workspaceFile, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]workspaceFile(nil), s.files...), nil
}
func (s *scriptedWorkspace) Read(_ context.Context, name string) (string, error) {
	if s.readErr != nil {
		return "", s.readErr
	}
	return s.content[name], nil
}
func (s *scriptedWorkspace) Write(_ context.Context, name, content string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[name] = content
	return nil
}

func workspaceFrame(app *workspaceApp) *render.Buffer {
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(72, 24)), Capabilities: capability.Set{Interactive: true, Unicode: true}}
	app.View(frame)
	return frame.Canvas.Buffer()
}

func workspaceRows(buffer *render.Buffer) string {
	var b strings.Builder
	for y := 0; y < buffer.Bounds().H; y++ {
		for x := 0; x < buffer.Bounds().W; x++ {
			b.WriteString(buffer.Cell(x, y).Grapheme)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestWorkspaceLoadsEditsPreviewsAndWritesThroughSeam(t *testing.T) {
	source := &scriptedWorkspace{
		files:   []workspaceFile{{Name: "config.lux"}, {Name: "notes.txt"}},
		content: map[string]string{"config.lux": "name = demo\nworkers = 2\n", "notes.txt": "hello\n"},
	}
	app := newWorkspaceApp(source)
	cmd := app.Update(event.ResizeTo(72, 24))
	if cmd == nil || app.phase != workspaceLoading {
		t.Fatalf("resize must start loading: phase=%v cmd=%v", app.phase, cmd != nil)
	}
	result := cmd(context.Background())
	if result.Kind != event.Message {
		t.Fatalf("load command kind=%v", result.Kind)
	}
	cmd = app.Update(result)
	if cmd == nil || app.phase != workspaceLoading {
		t.Fatalf("list result must start file read: phase=%v cmd=%v", app.phase, cmd != nil)
	}
	cmdResult := cmd(context.Background())
	app.Update(cmdResult)
	if app.phase != workspaceReady || app.file != "config.lux" {
		t.Fatalf("workspace not ready: phase=%v file=%q", app.phase, app.file)
	}
	if got := workspaceRows(workspaceFrame(app)); !strings.Contains(got, "Project workspace") || !strings.Contains(got, "config.lux") || !strings.Contains(got, "name = demo") {
		t.Fatalf("ready frame missing content:\n%s", got)
	}

	app.editor.SetValue("name = changed\nworkers = 3\n")
	app.dirty = true
	app.preview = true
	if got := workspaceRows(workspaceFrame(app)); !strings.Contains(got, "Preview (unsaved)") || !strings.Contains(got, "workers = 3") {
		t.Fatalf("preview missing:\n%s", got)
	}
	app.preview = false
	cmd = app.save()
	if cmd == nil || !app.saving {
		t.Fatalf("save must start command: cmd=%v saving=%v", cmd != nil, app.saving)
	}
	app.Update(cmd(context.Background()))
	if app.dirty || app.saving || source.writes["config.lux"] != "name = changed\nworkers = 3\n" {
		t.Fatalf("save did not commit: dirty=%v saving=%v writes=%v", app.dirty, app.saving, source.writes)
	}
}

func TestWorkspaceValidationUnsavedProtectionAndRecovery(t *testing.T) {
	source := &scriptedWorkspace{listErr: errors.New("permission denied")}
	app := newWorkspaceApp(source)
	cmd := app.Update(event.ResizeTo(72, 24))
	app.Update(cmd(context.Background()))
	if app.phase != workspaceError || !strings.Contains(app.notice, "permission denied") {
		t.Fatalf("error state missing: phase=%v notice=%q", app.phase, app.notice)
	}
	if got := workspaceRows(workspaceFrame(app)); !strings.Contains(got, "Project load failed") || !strings.Contains(got, "Ctrl+R retry") {
		t.Fatalf("error frame missing:\n%s", got)
	}

	source.listErr = nil
	source.files = []workspaceFile{{Name: "config.lux"}}
	source.content = map[string]string{"config.lux": "name = demo\nworkers = 2\n"}
	cmd = app.startList()
	if cmd == nil {
		t.Fatal("retry must issue a load command")
	}
	cmd = app.Update(cmd(context.Background()))
	if cmd == nil {
		t.Fatal("recovery list must issue a read command")
	}
	app.Update(cmd(context.Background()))
	if app.phase != workspaceReady {
		t.Fatalf("recovery phase=%v", app.phase)
	}

	app.editor.SetValue("name =\nworkers = 0\n")
	app.dirty = true
	if cmd := app.save(); cmd != nil || !strings.Contains(app.notice, "name is required") {
		t.Fatalf("invalid save must stay local: cmd=%v notice=%q", cmd != nil, app.notice)
	}
	app.ask = true
	app.pendingQuit = true
	if cmd := app.save(); cmd != nil {
		t.Fatal("save must be blocked while unsaved modal is open")
	}
	app.Update(event.RuneKey('n', 0))
	if app.ask || app.pendingQuit {
		t.Fatalf("negative unsaved answer must keep editing")
	}
}
