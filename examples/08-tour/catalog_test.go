package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// The wrapper captures immutable state in View, then publishes it only AFTER
// tour.View has queued Context.End's commit. Present returning alone does not
// prove that the input tree is live. No model state is written by the callback.
type catalogBeacon struct {
	input                            int
	frame                            *render.Buffer
	focus                            ui.ID
	geometry                         catalogGeometry
	text                             string
	mode                             int
	open, editing                    bool
	settingsOpen, reduced, scheduled bool
	theme, density                   int
}

type catalogProbe struct {
	*tour
	input     int
	committed chan catalogBeacon
}

func (p *catalogProbe) Update(next event.Event) lux.Command {
	if next.Kind != event.Frame {
		p.input++
	}
	return p.tour.Update(next)
}

func (p *catalogProbe) View(frame *lux.Frame) {
	p.tour.View(frame)
	beacon := catalogBeacon{input: p.input, frame: frame.Canvas.Buffer().Clone(),
		geometry: p.catalog.geometry, text: p.catalog.editor().Text(), mode: p.catalog.mode,
		open: p.catalog.modePicker.Open, editing: p.editing,
		settingsOpen: p.settings.open, reduced: p.reduced, theme: p.settings.theme.Selected, density: p.settings.density.Selected}
	_, beacon.scheduled = p.wantsFrame(frame.Now)
	frame.OnPresented(func() {
		for _, id := range []ui.ID{"tabs", "catalog-tree", "catalog-mode", "catalog-editor", "settings-theme", "settings-density", "settings-motion"} {
			if p.ui.Focused(id) {
				beacon.focus = id
			}
		}
		select {
		case p.committed <- beacon:
		default:
		}
	})
}

func startCatalogTour(t *testing.T, w, h int, options ...lux.Option) (*probeBackend, *catalogProbe, catalogBeacon) {
	t.Helper()
	backend := newProbeBackend(w, h)
	app := newTour()
	app.page, app.tabs.Selected = 6, 6
	app.sync.Started = time.Time{}
	probe := &catalogProbe{tour: app, committed: make(chan catalogBeacon, 64)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	options = append([]lux.Option{lux.WithBackend(backend), lux.WithMaxFPS(240)}, options...)
	go func() { done <- lux.Run(ctx, probe, options...) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("run: %v", err)
			}
		case <-time.After(hangTimeout):
			t.Error("tour did not exit after cancellation")
		}
	})
	return backend, probe, awaitCatalog(t, probe, -1)
}

func awaitCatalog(t *testing.T, app *catalogProbe, input int) catalogBeacon {
	t.Helper()
	bound := time.NewTimer(hangTimeout)
	defer bound.Stop()
	for {
		select {
		case beacon := <-app.committed:
			if beacon.input >= input {
				return beacon
			}
		case <-bound.C:
			t.Fatalf("no committed frame acknowledged input %d", input)
		}
	}
}

func sendCatalog(t *testing.T, backend *probeBackend, app *catalogProbe, before catalogBeacon, next event.Event) catalogBeacon {
	t.Helper()
	next.At = time.Now()
	backend.events <- next
	return awaitCatalog(t, app, before.input+1)
}

func catalogKey(t *testing.T, b *probeBackend, app *catalogProbe, before catalogBeacon, code event.KeyCode) catalogBeacon {
	t.Helper()
	return sendCatalog(t, b, app, before, event.KeyPress(code, 0))
}

func TestWidgetsCatalogEditorAndPopupCommittedRouting(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	for _, id := range []ui.ID{"catalog-tree", "catalog-mode", "catalog-editor"} {
		frame = catalogKey(t, backend, app, frame, event.KeyTab)
		if frame.focus != id {
			t.Fatalf("Tab focused %q; want %q", frame.focus, id)
		}
	}
	if _, _, visible := frame.frame.Caret(); !visible {
		t.Fatal("focused TextArea has no hardware caret")
	}
	original := frame.text
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Paste, Text: "q9\n界🙂"})
	if frame.text == original || !strings.Contains(frame.text, "q9\n界🙂") {
		t.Fatal("multiline mixed-cluster paste was not one editor action")
	}
	frame = sendCatalog(t, backend, app, frame, event.RuneKey('z', event.ModCtrl))
	if frame.text != original {
		t.Fatal("Ctrl+Z did not restore the whole paste")
	}
	frame = sendCatalog(t, backend, app, frame, event.RuneKey('y', event.ModCtrl))
	if frame.text == original {
		t.Fatal("Ctrl+Y did not redo the paste")
	}
	frame = sendCatalog(t, backend, app, frame, event.RuneKey('q', 0))
	frame = sendCatalog(t, backend, app, frame, event.RuneKey('9', 0))
	if !frame.editing || !strings.Contains(frame.text, "q9") {
		t.Fatal("editor runes escaped into tab/quit fallback")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyBackTab)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	if !frame.open || frame.focus != "catalog-mode" {
		t.Fatal("Enter did not open the captured Select")
	}
	if _, _, visible := frame.frame.Caret(); visible {
		t.Fatal("popup left the covered editor's caret visible")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEscape)
	if frame.open || frame.mode != 0 || frame.focus != "catalog-mode" {
		t.Fatal("Escape did not cancel the mode and restore select focus")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	// A press on the editor cancels, but must not focus/edit through the popup.
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Mouse, Mouse: event.MouseEvent{
		X: frame.geometry.editor.X, Y: frame.geometry.editor.Bottom() - 1, Action: event.MousePress, Button: event.MouseLeft,
	}})
	if frame.open || frame.mode != 0 || frame.focus != "catalog-mode" {
		t.Fatal("outside press clicked through or committed the preview")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	if frame.mode != 1 || frame.open {
		t.Fatal("Enter did not commit read-only mode")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	before := frame.text
	frame = sendCatalog(t, backend, app, frame, event.RuneKey('x', 0))
	if frame.text != before || frame.focus != "catalog-editor" {
		t.Fatal("read-only editor accepted text or lost focus")
	}
	if _, _, visible := frame.frame.Caret(); visible {
		t.Fatal("read-only editor painted an editable caret")
	}
}

func TestWidgetsCatalogTreePreservesDraftsAndStates(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	frame = catalogKey(t, backend, app, frame, event.KeyTab)   // tree
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // collapse
	if bufferContains(frame.frame, "notes.txt") {
		t.Fatal("collapsed Tree still paints its leaves")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // expand
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // notes
	if !strings.Contains(frame.text, "# Widget notes") {
		t.Fatal("Tree leaf did not open notes")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab) // mode
	frame = catalogKey(t, backend, app, frame, event.KeyTab) // editor
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Text, Text: "draft: "})
	draft := frame.text
	frame = catalogKey(t, backend, app, frame, event.KeyBackTab)
	frame = catalogKey(t, backend, app, frame, event.KeyBackTab)
	frame = catalogKey(t, backend, app, frame, event.KeyUp)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // config
	if strings.Contains(frame.text, "draft: ") {
		t.Fatal("draft leaked to another leaf")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // notes
	if frame.text != draft {
		t.Fatal("switching leaves discarded the draft")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // disabled
	if frame.mode != 2 {
		t.Fatal("Select did not commit disabled state")
	}
	cell := frame.frame.Cell(frame.geometry.editor.X, frame.geometry.editor.Y)
	if cell.Style.Attrs&render.AttrDim == 0 {
		t.Fatal("disabled TextArea has no non-color distinction")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	if frame.focus != "tabs" {
		t.Fatalf("disabled editor remained in focus order: %s", frame.focus)
	}
	frame = catalogKey(t, backend, app, frame, event.KeyBackTab) // mode
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	frame = catalogKey(t, backend, app, frame, event.KeyEnd)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter) // empty
	if frame.text != "" || frame.mode != 3 {
		t.Fatal("empty sample did not start blank")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Text, Text: "new sample"})
	if frame.text != "new sample" {
		t.Fatal("empty TextArea did not accept first text")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	if frame.editing || !bufferContains(frame.frame, "FORM CONTROLS") {
		t.Fatal("F2 did not return to the original widget stand")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	if frame.text != "new sample" {
		t.Fatal("disclosure discarded editing state")
	}
}

func TestWidgetsCatalogIdleStopsDecorationSchedule(t *testing.T) {
	app := newTour()
	app.page, app.tabs.Selected = 6, 6
	frame := &lux.Frame{Canvas: render.NewCanvas(render.NewBuffer(120, 40)), Now: time.Unix(100, 0)}
	app.View(frame) // makes the legacy spinner and gauge visible
	app.editing = true
	app.View(frame)
	if delay, scheduled := app.wantsFrame(frame.Now); scheduled {
		t.Fatalf("hidden decoration still scheduled in %s", delay)
	}
}

func TestWidgetsCatalogResizeAndModalTrap(t *testing.T) {
	backend, app, frame := startCatalogTour(t, 80, 24)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	for range 2 {
		frame = catalogKey(t, backend, app, frame, event.KeyTab)
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	frame = catalogKey(t, backend, app, frame, event.KeyF2)
	if !frame.open || !frame.editing || frame.focus != "catalog-mode" {
		t.Fatal("popup lost focus trap or F2 toggled through it")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEscape)
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	before := frame.text
	for _, size := range [][2]int{{120, 40}, {12, 3}, {240, 80}, {80, 24}} {
		if size[0] == 240 {
			// Zero-size intentionally has no View/Present; acknowledge BOTH
			// queued resizes from the restored frame rather than waiting on
			// an output that the runtime contract says cannot exist.
			backend.events <- event.Event{Kind: event.Resize, Width: 0, Height: 0}
			backend.events <- event.Event{Kind: event.Resize, Width: size[0], Height: size[1]}
			frame = awaitCatalog(t, app, frame.input+2)
		} else {
			frame = sendCatalog(t, backend, app, frame, event.Event{Kind: event.Resize, Width: size[0], Height: size[1]})
		}
		if frame.text != before {
			t.Fatal("resize discarded editor content")
		}
		if x, y, visible := frame.frame.Caret(); visible {
			if (render.Rect{X: x, Y: y, W: 1, H: 1}).Intersect(frame.frame.Bounds()).Empty() {
				t.Fatal("resize painted an out-of-bounds caret")
			}
		}
	}
	if !bufferContains(frame.frame, "Editing widgets") || !bufferContains(frame.frame, "TextArea") {
		t.Fatal("catalog did not recover after tiny/zero resize")
	}
}

func TestWidgetsCatalogGeometryAndCellSnapshots(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {240, 80}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			backend, app, frame := startCatalogTour(t, size[0], size[1])
			frame = catalogKey(t, backend, app, frame, event.KeyF2)
			for range 3 {
				frame = catalogKey(t, backend, app, frame, event.KeyTab)
			}
			bounds := frame.frame.Bounds()
			for name, rect := range map[string]render.Rect{"tree": frame.geometry.tree, "select": frame.geometry.modePicker, "editor": frame.geometry.editor} {
				if rect.Empty() || rect.Intersect(bounds) != rect || rect.Bottom() > size[1]-2 {
					t.Fatalf("%s geometry outside body: %#v", name, rect)
				}
			}
			x, y, visible := frame.frame.Caret()
			if !visible || (render.Rect{X: x, Y: y, W: 1, H: 1}).Intersect(frame.geometry.editor).Empty() {
				t.Fatalf("caret (%d,%d,%v) outside editor", x, y, visible)
			}
			var snapshot strings.Builder
			fmt.Fprintf(&snapshot, "viewport=%dx%d\ngeometry=%+v\nfocus=%s caret=%d,%d,%v\n", size[0], size[1], frame.geometry, frame.focus, x, y, visible)
			var cells []render.Cell
			for row := 2; row < size[1]-2; row++ {
				var text strings.Builder
				for x := 0; x < size[0]; x++ {
					cell := frame.frame.Cell(x, row)
					text.WriteString(cell.Grapheme)
					cells = append(cells, cell)
				}
				fmt.Fprintf(&snapshot, "%02d %q\n", row, strings.TrimRight(text.String(), " "))
			}
			encoded, err := json.Marshal(cells)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&snapshot, "all-body-cells-sha256=%x\n", sha256.Sum256(encoded))
			for _, point := range [][2]int{{frame.geometry.tree.X, frame.geometry.tree.Y}, {frame.geometry.modePicker.X, frame.geometry.modePicker.Y}, {x, y}} {
				fmt.Fprintf(&snapshot, "cell(%d,%d)=%#v\n", point[0], point[1], frame.frame.Cell(point[0], point[1]))
			}
			path := fmt.Sprintf("testdata/catalog-%dx%d.txt", size[0], size[1])
			if os.Getenv("LUX_UPDATE_SNAPSHOTS") == "1" {
				if err := os.MkdirAll("testdata", 0755); err != nil {
					t.Fatal(err)
				}
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

func TestWidgetsEditingCatalogReachableAtCompactSize(t *testing.T) {
	backend, app, start := startCatalogTour(t, 80, 24)
	frame := sendCatalog(t, backend, app, start, event.KeyPress(event.KeyF2, 0))
	for _, label := range []string{"Editing widgets", "Tree", "Select", "TextArea"} {
		if !bufferContains(frame.frame, label) {
			t.Fatalf("80x24 Widgets / F2 lost %q", label)
		}
	}
	if !bufferContainsRow(frame.frame, 23, "F2") {
		t.Fatal("the compact catalog lost its back-navigation hint")
	}
}

func TestWidgetsCatalogShowsLoadingErrorAndRetryStates(t *testing.T) {
	backend, app, start := startCatalogTour(t, 80, 24)
	frame := sendCatalog(t, backend, app, start, event.KeyPress(event.KeyF2, 0))
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyDown)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	for _, label := range []string{"Loading", "Ready", "Empty", "Error", "Retry"} {
		if !bufferContains(frame.frame, label) {
			t.Fatalf("state catalog missing %q", label)
		}
	}
	for range 4 {
		frame = catalogKey(t, backend, app, frame, event.KeyTab)
	}
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	if !bufferContains(frame.frame, "Error: permission denied") {
		t.Fatal("error state did not expose its diagnostic copy")
	}
	frame = catalogKey(t, backend, app, frame, event.KeyTab)
	frame = catalogKey(t, backend, app, frame, event.KeyEnter)
	if !bufferContains(frame.frame, "Ready: rows available") {
		t.Fatal("retry did not restore ready state")
	}
}
