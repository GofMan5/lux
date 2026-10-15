package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

func TestWorkbenchSettingsCellSnapshots(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {240, 80}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			backend, app, frame := startCatalogTour(t, size[0], size[1])
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			var snapshot strings.Builder
			for theme, name := range workbenchThemes {
				if theme > 0 {
					frame = catalogKey(t, backend, app, frame, event.KeyDown)
				}
				if frame.theme != theme || frame.focus != "settings-theme" || frame.scheduled {
					t.Fatal("settings state/focus/idle policy is wrong")
				}
				x, y, caret := frame.frame.Caret()
				if caret {
					t.Fatal("settings exposed the covered editor's caret")
				}
				fmt.Fprintf(&snapshot, "theme=%s viewport=%dx%d focus=%s caret=%d,%d,%v\n", name, size[0], size[1], frame.focus, x, y, caret)
				var cells []render.Cell
				for row := 2; row < size[1]-2; row++ {
					var text strings.Builder
					for col := 0; col < size[0]; col++ {
						cell := frame.frame.Cell(col, row)
						cells = append(cells, cell)
						text.WriteString(cell.Grapheme)
						if theme == 2 && (cell.Style.Foreground != (render.Color{}) || cell.Style.Background != (render.Color{})) {
							t.Fatal("terminal-default settings assumes a terminal color")
						}
					}
					if strings.TrimSpace(text.String()) != "" {
						fmt.Fprintf(&snapshot, "%02d %q\n", row, strings.TrimRight(text.String(), " "))
					}
				}
				encoded, err := json.Marshal(cells)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&snapshot, "all-body-cells-sha256=%x\n", sha256.Sum256(encoded))
			}
			path := fmt.Sprintf("testdata/settings-%dx%d.txt", size[0], size[1])
			if os.Getenv("LUX_UPDATE_SNAPSHOTS") == "1" {
				if err := os.WriteFile(path, []byte(snapshot.String()), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.String() != string(want) {
				t.Fatalf("cell snapshot differs from %s", path)
			}
		})
	}
}

func TestWorkbenchThemeSelectionRestyles(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	dark := frame.frame.Cell(0, 0).Style.Background
	frame = catalogKey(t, backend, app, frame, event.KeyF3)
	frame = catalogKey(t, backend, app, frame, event.KeyDown) // light
	if frame.frame.Cell(0, 0).Style.Background == dark {
		t.Fatal("theme radio changed its marker but not the screen")
	}
}

func TestWorkbenchDensityChangesCatalogGeometry(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {240, 80}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			backend, app, frame := startCatalogTour(t, size[0], size[1])
			frame = catalogKey(t, backend, app, frame, event.KeyF2)
			for range 3 {
				frame = catalogKey(t, backend, app, frame, event.KeyTab)
			}
			draft, compact := frame.text, frame.geometry
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			frame = catalogKey(t, backend, app, frame, event.KeyTab)
			frame = catalogKey(t, backend, app, frame, event.KeyDown)
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			if frame.geometry == compact {
				t.Fatal("comfortable density did not change catalog spacing")
			}
			if frame.text != draft || frame.focus != "catalog-editor" {
				t.Fatal("density changed editor behavior or draft")
			}
			for _, rect := range []render.Rect{frame.geometry.tree, frame.geometry.modePicker, frame.geometry.editor} {
				if rect.Empty() || rect.Intersect(frame.frame.Bounds()) != rect {
					t.Fatalf("comfortable geometry outside body: %+v", rect)
				}
			}
		})
	}
}

func TestWorkbenchTerminalDefaultFocusIsDistinct(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF3)
	for range 2 {
		frame = catalogKey(t, backend, app, frame, event.KeyDown)
	}
	focused := frame.frame.Cell(1, 8).Style
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	selected := frame.frame.Cell(1, 8).Style
	if focused == selected {
		t.Fatal("terminal-default focus is indistinguishable from selection")
	}
}

func TestWorkbenchReducedMotionPolicy(t *testing.T) {
	for _, runtimeReduced := range []bool{false, true} {
		t.Run(fmt.Sprint(runtimeReduced), func(t *testing.T) {
			backend, app, frame := startCatalogTour(t, 80, 24, lux.WithReducedMotion(runtimeReduced))
			frame = sendCatalog(t, backend, app, frame, event.RuneKey('6', 0))
			if frame.reduced != runtimeReduced || frame.scheduled == runtimeReduced {
				t.Fatal("3D baseline motion policy is wrong")
			}
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			if frame.scheduled {
				t.Fatal("hidden 3D animation schedules settings frames")
			}
			for range 2 {
				frame = catalogKey(t, backend, app, frame, event.KeyTab)
			}
			frame = sendCatalog(t, backend, app, frame, event.RuneKey(' ', 0))
			frame = catalogKey(t, backend, app, frame, event.KeyEscape)
			if !frame.reduced || frame.scheduled || !bufferContains(frame.frame, "REDUCED MOTION") {
				t.Fatal("local preference did not freeze decoration")
			}
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			for range 2 {
				frame = catalogKey(t, backend, app, frame, event.KeyTab)
			}
			frame = sendCatalog(t, backend, app, frame, event.RuneKey(' ', 0))
			frame = catalogKey(t, backend, app, frame, event.KeyF3)
			if frame.reduced != runtimeReduced || frame.scheduled == runtimeReduced {
				t.Fatal("local toggle overrode runtime preference or failed to resume")
			}
		})
	}
}

func TestWorkbenchSettingsReachableFromEditor(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	for range 3 {
		frame = catalogKey(t, backend, app, frame, event.KeyTab)
	}
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Text, Text: "draft: "})
	draft := frame.text
	frame = catalogKey(t, backend, app, frame, event.KeyF3)
	for _, label := range []string{"Workbench settings", "Theme", "Density", "Reduced motion"} {
		if !bufferContains(frame.frame, label) {
			t.Fatalf("80x24 F3 settings lost %q", label)
		}
	}
	frame = catalogKey(t, backend, app, frame, event.KeyF1)
	if !frame.settingsOpen || frame.focus != "settings-theme" {
		t.Fatal("help shortcut stole the settings focus scope")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEscape)
	if frame.text != draft || frame.focus != "catalog-editor" || !bufferContains(frame.frame, "Editing widgets") {
		t.Fatal("closing settings lost the editing stand, focus or its draft")
	}
	if _, _, visible := frame.frame.Caret(); !visible {
		t.Fatal("settings left the editor caret hidden")
	}
}
