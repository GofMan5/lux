package main

import (
	"context"
	"strings"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// screen renders one frame and returns its rows as plain text.
func screen(app *studio, width, height int) []string {
	buffer := render.NewBuffer(width, height)
	app.View(&lux.Frame{Canvas: render.NewCanvas(buffer), Capabilities: capability.Set{Interactive: true, Unicode: true}})
	rows := make([]string, height)
	for y := range rows {
		var row strings.Builder
		for x := 0; x < width; x++ {
			row.WriteString(buffer.Cell(x, y).Grapheme)
		}
		rows[y] = row.String()
	}
	return rows
}

func contains(rows []string, text string) bool {
	return strings.Contains(strings.Join(rows, "\n"), text)
}

func TestEveryPageRendersItsContent(t *testing.T) {
	app := newStudio()
	want := map[int][]string{
		pageDashboard: {"LUX STUDIO", "CPU", "REQUESTS", "Deploy pipeline", "verify", "studio started"},
		pageServices:  {"Services 12/12", "inventory", "Edge router", "https://github.com/GofMan5/lux", "Restart"},
		pageFiles:     {"Project", "README.md", "# LUX Studio", "Ln 1, Col 1"},
		pageMotion:    {"Motion lab", "SPRING", "OutCubic", "Pause", "scene3d"},
		pageSettings:  {"Aurora", "reduce motion", "500 ms", "CAPABILITIES", "Event inspector"},
	}
	for page, texts := range want {
		app.Update(event.RuneKey(rune('1'+page), 0))
		if app.tabs.Selected != page {
			t.Fatalf("digit %d selected page %d", page+1, app.tabs.Selected)
		}
		rows := screen(app, 120, 36)
		for _, text := range texts {
			if !contains(rows, text) {
				t.Errorf("%s page lacks %q:\n%s", pages[page], text, strings.Join(rows, "\n"))
			}
		}
	}
}

func TestSmallTerminalAsksForRoom(t *testing.T) {
	if rows := screen(newStudio(), 80, 24); !contains(rows, "needs 96x28") {
		t.Fatalf("no size hint:\n%s", strings.Join(rows, "\n"))
	}
}

func TestDeployRunsEveryStageAndIgnoresStaleTicks(t *testing.T) {
	app := newStudio()
	cmd := app.Update(event.RuneKey('d', 0))
	if cmd == nil || !app.deploy.running {
		t.Fatal("d did not start the deploy")
	}
	first := cmd(context.Background())
	if tick, ok := first.Data.(jobTick); !ok || tick.id != app.deploy.id {
		t.Fatalf("command produced %#v, want this run's jobTick", first.Data)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := cmd(ctx); got.Kind != 0 || got.Data != nil {
		t.Fatalf("cancelled command produced %#v", got)
	}
	for next := first; app.Update(next) != nil; {
		next = event.Event{Kind: event.Message, Data: jobTick{id: app.deploy.id}}
	}
	if app.deploy.running || app.deploy.step != len(stages)*stageSteps || app.deploy.finished.IsZero() {
		t.Fatalf("deploy did not finish: %+v", app.deploy)
	}
	if app.notify[0] == "" || app.toast != "Deploy finished" {
		t.Fatalf("finish not announced: notify=%q toast=%q", app.notify, app.toast)
	}
	screen(app, 120, 36)
	if app.notify[0] != "" {
		t.Fatal("View did not flush the queued notification")
	}

	pending := app.Update(event.RuneKey('d', 0))
	stale := app.deploy.id
	app.Update(event.RuneKey('d', 0)) // cancel
	if got := pending(context.Background()); got.Data != nil {
		t.Fatalf("cancel left the pending tick running: %#v", got)
	}
	if !app.deploy.failed || app.Update(event.Event{Kind: event.Message, Data: jobTick{id: stale}}) != nil || app.deploy.step != 0 {
		t.Fatalf("a cancelled run kept ticking: %+v", app.deploy)
	}
}

func TestRestartAsksFirstThenRecovers(t *testing.T) {
	app := newStudio()
	app.tabs.Selected = pageServices
	app.filter.Value = "inventory"
	app.refilter()
	app.route(ui.Action{Target: "restart", Kind: ui.Activate})
	if app.confirm != "inventory" {
		t.Fatalf("restart opened no confirmation: %q", app.confirm)
	}
	if rows := screen(app, 120, 36); !contains(rows, "Restart inventory?") {
		t.Fatalf("confirm modal missing:\n%s", strings.Join(rows, "\n"))
	}
	app.route(ui.Action{Target: "confirm", Kind: ui.Dismiss})
	if app.confirm != "" {
		t.Fatal("Esc did not dismiss the confirmation")
	}
	app.route(ui.Action{Target: "restart", Kind: ui.Activate})
	cmd := app.route(ui.Action{Target: "confirm-yes", Kind: ui.Activate})
	if s, _ := app.current(); cmd == nil || s.status != "restarting" {
		t.Fatalf("confirm did not restart: status=%q", s.status)
	}
	app.route(ui.Action{Target: "restart", Kind: ui.Activate})
	if app.confirm != "" {
		t.Fatal("a restarting service offered a second restart")
	}
	app.Update(event.Event{Kind: event.Message, Data: restartDone{name: "inventory"}})
	if s, _ := app.current(); s.status != "healthy" || s.latency == 0 || len(app.restarting) != 0 {
		t.Fatalf("restart did not recover: %+v", s)
	}
}

func TestSortAndFilter(t *testing.T) {
	app := newStudio()
	app.table.SortActive, app.table.SortColumn, app.table.SortDesc = true, 3, true
	app.refilter()
	if s, _ := app.current(); s.name != "billing" {
		t.Fatalf("slowest first = %s, want billing", s.name)
	}
	app.table.SortDesc = false
	app.refilter()
	if s, _ := app.current(); s.name != "inventory" {
		t.Fatalf("fastest first = %s, want inventory (no latency)", s.name)
	}
	app.filter.Value = "DEGRADED"
	app.refilter()
	if len(app.view) != 2 {
		t.Fatalf("filter kept %d rows, want billing and notifier", len(app.view))
	}
}

func TestPaletteFuzzyRunsCommand(t *testing.T) {
	app := newStudio()
	// Ctrl+P is a shortcut, and shortcuts commit only once a frame is
	// presented, so drive its target directly.
	app.route(ui.Action{Target: "open-palette", Kind: ui.Activate})
	if !app.palette {
		t.Fatal("open-palette did not open the palette")
	}
	app.query.Value = "gtf"
	if found := app.matches(); len(found) != 1 || commands[found[0]].title != "Go to Files" {
		t.Fatalf("gtf matched %v", found)
	}
	if rows := screen(app, 120, 36); !contains(rows, "Go to Files") || contains(rows, "Go to Motion") {
		t.Fatalf("palette list not filtered:\n%s", strings.Join(rows, "\n"))
	}
	app.paletteKey(event.KeyPress(event.KeyEnter, 0))
	if app.palette || app.tabs.Selected != pageFiles {
		t.Fatalf("Enter did not run the command: palette=%v page=%d", app.palette, app.tabs.Selected)
	}
}

func TestThemeKeyRepaints(t *testing.T) {
	app := newStudio()
	app.Update(event.RuneKey('t', 0))
	if app.theme != 1 || app.themeRadio.Selected != 1 {
		t.Fatalf("t did not advance the theme: %d", app.theme)
	}
	buffer := render.NewBuffer(120, 36)
	app.View(&lux.Frame{Canvas: render.NewCanvas(buffer), Capabilities: capability.Set{Interactive: true}})
	if got := buffer.Cell(60, 20).Style.Background; got != themes[1].palette.Background {
		t.Fatalf("background %v, want the Ember palette", got)
	}
}

func TestEditsSurviveSwitchingFiles(t *testing.T) {
	app := newStudio()
	app.editor.SetValue("changed")
	app.openFile("config.yaml")
	app.openFile("README.md")
	if app.editor.Text() != "changed" || app.saved["README.md"] == "changed" {
		t.Fatalf("unsaved edit lost or saved early: %q", app.editor.Text())
	}
	app.route(ui.Action{Target: "editor-save", Kind: ui.Activate})
	if app.saved["README.md"] != "changed" {
		t.Fatal("save did not store the buffer")
	}
}

// TestIdlePagesSleep pins the frame budget: Files and Settings ask for no
// frames, a toast wakes once at its expiry, and only motion runs continuously.
func TestIdlePagesSleep(t *testing.T) {
	app := newStudio()
	now := time.Now()
	for _, page := range []int{pageFiles, pageSettings} {
		app.tabs.Selected = page
		if at, continuous := app.wakeup(now); !at.IsZero() || continuous {
			t.Fatalf("%s wakes at %v continuous=%v", pages[page], at, continuous)
		}
	}
	app.say("hello")
	if at, continuous := app.wakeup(now); !at.Equal(app.toastEnd) || continuous {
		t.Fatalf("toast wakes at %v, want %v", at, app.toastEnd)
	}
	app.toast, app.toastEnd = "", time.Time{}
	app.tabs.Selected = pageMotion
	if _, continuous := app.wakeup(now); !continuous {
		t.Fatal("the motion lab is not animating")
	}
	app.paused = true
	if at, continuous := app.wakeup(now); !at.IsZero() || continuous {
		t.Fatal("a paused motion lab still asks for frames")
	}
	app.paused, app.reduced = false, true
	if _, continuous := app.wakeup(now); continuous {
		t.Fatal("reduced motion still animates")
	}
}
