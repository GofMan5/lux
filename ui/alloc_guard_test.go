package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GofMan5/lux/render"
)

// TestWidgetDrawAllocationBudgets complements the render and encoder guards.
// Buffers, data, and editor layout are warmed outside the measured draws.
// Like the draw benchmarks, Context is unbegun (except Modal's scope): frame
// registration/commit, edits, cold layout, and application data loading are
// outside these budgets. Zero-allocation paths stay at zero; allocating paths
// have explicit ceilings, not timing thresholds or an automatically moving
// baseline. Calibrated on Windows/amd64, Go 1.25.5: counts <= 4 are pinned
// exactly; larger counts allow at most 20% growth, rounded down.
func TestWidgetDrawAllocationBudgets(t *testing.T) {
	canvas := func(w, h int) *render.Canvas {
		return render.NewCanvas(render.NewBuffer(w, h))
	}
	ctx := Context{focused: "bench"}
	buttonCanvas := canvas(24, 3)
	checkboxCanvas := canvas(24, 1)
	radioCanvas := canvas(20, 6)
	radioClip := radioCanvas.Clip(render.Rect{Y: 2, W: 20, H: 3})
	listCanvas := canvas(24, 6)
	tabsCanvas := canvas(120, 1)
	gaugeCanvas := canvas(80, 1)
	spinnerCanvas := canvas(1, 1)
	sparklineCanvas := canvas(80, 1)
	selectCanvas := canvas(30, 16)
	selectClip := selectCanvas.Clip(render.Rect{Y: 1, W: 30, H: 7})
	modalCanvas := canvas(32, 16).Clip(render.Rect{W: 32, H: 8})
	tableCanvas := canvas(120, 40)
	tableClip := canvas(24, 12).Clip(render.Rect{Y: 3, W: 24, H: 6})
	treeCanvas := canvas(40, 10)
	treeClip := treeCanvas.Clip(render.Rect{Y: 6, W: 40, H: 4})
	areaCanvas := canvas(40, 10).Clip(render.Rect{Y: 6, W: 40, H: 4})
	inputCanvas := canvas(40, 1)
	viewportCanvas := canvas(24, 8)

	checkbox := CheckboxState{Checked: true}
	radio := RadioState{Selected: 1}
	labels := []string{"ink", "violet", "muted", "gold", "rust", "slate"}
	list := ListState{Selected: 2}
	tabs := TabsState{Selected: 5}
	tabLabels := make([]string, 12)
	for i := range tabLabels {
		tabLabels[i] = "panel-0" + string(rune('a'+i))
	}
	gauge := GaugeState{Value: 62, Max: 100}
	now := time.Unix(1, 0)
	spinner := SpinnerState{Started: now.Add(-137 * time.Millisecond)}
	values := make([]float64, 80)
	for i := range values {
		values[i] = float64(i)
	}
	sparkline := SparklineState{Values: values}
	closed := SelectState{Selected: 2}
	open := SelectState{Selected: 2, Open: true}
	clipped := SelectState{Selected: 2, Open: true, PopupRows: 14}
	options := selectOptions()
	longOptions := clippedSelectOptions()
	modalCtx := Context{begun: true}
	table := TableState{Selected: 500_000}
	columns := []TableColumn{{Title: "ID", Width: 8}, {Title: "Name", Weight: 2}, {Title: "State"}}
	rowAt := func(int) []string {
		// Include the caller's per-visible-row slice, as BenchmarkTableDraw does.
		return []string{"00000000", "a moderately long process name", "running"}
	}
	tree := TreeState{Selected: 5}
	treeRows := make([]TreeRow, 30)
	for i := range treeRows {
		treeRows[i] = TreeRow{ID: strconv.Itoa(i), Label: "node-with-a-longer-name", Depth: i % 4}
	}
	treeRowAt := func(index int) TreeRow { return treeRows[index] }
	area := &TextArea{}
	area.SetValue(strings.Repeat("mixed 界é🙂 cluster rows wrap\taround forty\tcolumns\n", 6))
	value := strings.Repeat("mixed 界é→ input ", 6)
	input := &TextInput{Value: value, Cursor: len(value)}
	viewport := ScrollState{Offset: 500_000}

	for _, tc := range []struct {
		name     string
		baseline float64
		budget   float64
		draw     func()
	}{
		{"Button", 1, 1, func() { ctx.Button("bench", buttonCanvas, buttonCanvas.Bounds(), "Run task", ButtonStyle{}) }},
		{"Checkbox", 2, 2, func() {
			ctx.Checkbox("bench", checkboxCanvas, checkboxCanvas.Bounds(), &checkbox, "stream telemetry", CheckboxStyle{})
		}},
		{"RadioGroup", 7, 8, func() { ctx.RadioGroup("bench", radioCanvas, radioCanvas.Bounds(), &radio, labels, RadioStyle{}) }},
		{"RadioGroupClipped", 4, 4, func() { ctx.RadioGroup("bench", radioClip, radioCanvas.Bounds(), &radio, labels, RadioStyle{}) }},
		{"List", 2, 2, func() { ctx.List("bench", listCanvas, listCanvas.Bounds(), &list, labels, ListStyle{}) }},
		{"Tabs", 2, 2, func() { ctx.Tabs("bench", tabsCanvas, tabsCanvas.Bounds(), &tabs, tabLabels, TabsStyle{}) }},
		{"Gauge", 1, 1, func() { ctx.Gauge(gaugeCanvas, gaugeCanvas.Bounds(), &gauge, GaugeStyle{}) }},
		{"Spinner", 1, 1, func() {
			ctx.Spinner(spinnerCanvas, spinnerCanvas.Bounds(), &spinner, now, SpinnerStyle{Frames: SpinnerBraille})
		}},
		{"Sparkline", 1, 1, func() { ctx.Sparkline(sparklineCanvas, sparklineCanvas.Bounds(), &sparkline, SparklineStyle{}) }},
		{"SelectClosed", 1, 1, func() { ctx.Select("bench", selectCanvas, render.Rect{W: 30, H: 1}, &closed, options, SelectStyle{}) }},
		{"SelectOpen", 9, 10, func() { ctx.Select("bench", selectCanvas, render.Rect{W: 30, H: 1}, &open, options, SelectStyle{}) }},
		{"SelectClipped", 36, 43, func() {
			ctx.Select("bench", selectClip, render.Rect{W: 30, H: 1}, &clipped, longOptions, SelectStyle{})
		}},
		{"ModalClipped", 2, 2, func() {
			modalCtx.Modal("help", modalCanvas, render.Rect{X: 2, Y: 2, W: 24, H: 12}, "Dialog", ModalStyle{})
			modalCtx.EndModal()
		}},
		{"TableMillion", 41, 49, func() {
			ctx.Table("bench", tableCanvas, tableCanvas.Bounds(), &table, columns, 1_000_000, rowAt, TableStyle{})
		}},
		{"TableClipped", 32, 38, func() {
			ctx.Table("bench", tableClip, tableClip.Bounds(), &table, columns, 1_000_000, rowAt, TableStyle{})
		}},
		{"Tree", 11, 13, func() {
			ctx.Tree("bench", treeCanvas, treeCanvas.Bounds(), &tree, len(treeRows), treeRowAt, TreeStyle{})
		}},
		{"TreeClipped", 5, 6, func() { ctx.Tree("bench", treeClip, treeCanvas.Bounds(), &tree, len(treeRows), treeRowAt, TreeStyle{}) }},
		{"TextAreaWarmClipped", 0, 0, func() { ctx.TextArea("bench", areaCanvas, areaCanvas.Bounds(), area, TextAreaStyle{}) }},
		{"InputWarm", 0, 0, func() { ctx.Input("bench", inputCanvas, inputCanvas.Bounds(), input, InputStyle{}) }},
		{"ScrollViewport", 1, 1, func() {
			ctx.ScrollViewport("bench", viewportCanvas, viewportCanvas.Bounds(), &viewport, 1_000_000, ScrollStyle{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, tc.draw)
			t.Logf("allocs/draw = %.0f; baseline = %.0f; ceiling = %.0f", allocs, tc.baseline, tc.budget)
			if allocs > tc.budget {
				t.Fatalf("draw allocated %.0f times; want <= %.0f (warm paint only)", allocs, tc.budget)
			}
		})
	}
}
