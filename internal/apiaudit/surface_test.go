package apiaudit

import (
	"fmt"
	"strings"
	"testing"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/capability"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/layout"
	"github.com/GofMan5/lux/motion"
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
	"github.com/GofMan5/lux/ui"
)

// all returns T{}, &T{} and (*T)(nil) for a struct type.
func all[T any]() []any {
	var zero T
	return []any{zero, &zero, (*T)(nil)}
}

func surface() []any {
	var values []any
	for _, group := range [][]any{
		all[lux.Frame](), all[lux.Metrics](), {lux.ProgressState(0), lux.Emit, lux.Quit},
		all[capability.Set](), {capability.ColorDepth(0), capability.Graphics(0), capability.Portable},
		all[event.Event](), all[event.KeyEvent](), all[event.MouseEvent](),
		{event.KeyAction(0), event.KeyCode(0), event.Kind(0), event.Mod(0), event.MouseAction(0), event.MouseButton(0),
			event.KeyPress, event.ResizeTo, event.RuneKey},
		all[layout.Constraint](), {layout.Axis(0), layout.Class(0), layout.Flex, layout.Classify},
		all[motion.Spring](), all[motion.Tween](), {motion.InOutCubic, motion.Linear, motion.OutBack, motion.OutCubic},
		all[render.Border](), all[render.Buffer](), all[render.Canvas](), all[render.Cell](), all[render.Change](),
		all[render.Color](), all[render.DoubleBuffer](), all[render.Link](), all[render.Rect](), all[render.Style](),
		all[render.TextMetrics](), all[render.WidthPolicy](), all[render.WrapOptions](),
		{render.Attr(0), render.ColorMode(0), render.UnderlineStyle(0), render.WrapMode(0),
			render.GraphemeWidth, render.GraphemeWidthWith, render.TextWidth, render.TextWidthWith,
			render.WrapLines, render.WrapLinesWith, render.NewBuffer, render.NewCanvas, render.NewCanvasWithPolicy,
			render.ANSIColor, render.IndexedColor, render.RGBColor, render.NewDoubleBuffer, render.Bounds,
			render.MeasureWrap, render.MeasureWrapWith},
		all[scene3d.Camera](), all[scene3d.Light](), all[scene3d.Mesh](), all[scene3d.Object](),
		all[scene3d.Renderer](), all[scene3d.Scene](), all[scene3d.Triangle](), all[scene3d.Vec3](), all[scene3d.Mat4](),
		{scene3d.Color(0), scene3d.ColorMode(0), scene3d.Degrees, scene3d.RGB, scene3d.Identity, scene3d.LookAt,
			scene3d.Perspective, scene3d.RotateX, scene3d.RotateY, scene3d.RotateZ, scene3d.Scale, scene3d.Translate,
			scene3d.Cube, scene3d.NewRenderer, scene3d.V3},
		all[ui.Action](), all[ui.ButtonStyle](), all[ui.CheckboxState](), all[ui.CheckboxStyle](), all[ui.Context](),
		all[ui.GaugeState](), all[ui.GaugeStyle](), all[ui.InputStyle](), all[ui.ListState](), all[ui.ListStyle](),
		all[ui.ModalStyle](), all[ui.Palette](), all[ui.RadioState](), all[ui.RadioStyle](), all[ui.RegionOptions](),
		all[ui.ScrollState](), all[ui.ScrollStyle](), all[ui.SelectState](), all[ui.SelectStyle](), all[ui.Shortcut](),
		all[ui.SparklineState](), all[ui.SparklineStyle](), all[ui.SpinnerState](), all[ui.SpinnerStyle](),
		all[ui.TableColumn](), all[ui.TableState](), all[ui.TableStyle](), all[ui.TabsState](), all[ui.TabsStyle](),
		all[ui.TextArea](), all[ui.TextAreaStyle](), all[ui.TextInput](), all[ui.Theme](), all[ui.TreeRow](),
		all[ui.TreeState](), all[ui.TreeStyle](), all[ui.VisibleRange](),
		{ui.ActionKind(0), ui.Flags(0), ui.ID(""), ui.ShortcutScope(0), ui.ColumnLayout, ui.SelectPlacement,
			ui.ReferencePalette, ui.DefaultTheme},
	} {
		values = append(values, group...)
	}
	return values
}

// TestSurfaceSweep pins the public contract that zero values are ready to
// use and nil pointer receivers are inert: no exported method or function
// panics or hangs on zero or nil arguments.
func TestSurfaceSweep(t *testing.T) {
	for _, mode := range []Mode{Nil, Zero} {
		if failures := Sweep(mode, surface()...); len(failures) > 0 {
			t.Errorf("mode %d: %d failures\n%s", mode, len(failures), strings.Join(failures, "\n"))
		}
	}
}

type probe struct{}

func (probe) Value()             {}
func (*probe) Panics()           { panic("boom") }
func (*probe) Hangs()            { select {} }
func (p *probe) Guarded() string { return fmt.Sprint(p == nil) }

func TestSweepReportsPanicsAndHangs(t *testing.T) {
	got := strings.Join(Sweep(Nil, (*probe)(nil)), "\n")
	want := "(*apiaudit.probe)(nil).Hangs: hung\n(*apiaudit.probe)(nil).Panics: boom"
	if got != want {
		t.Fatalf("Sweep = %q, want %q", got, want)
	}
}
