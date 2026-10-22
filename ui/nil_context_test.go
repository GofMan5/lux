package ui

import (
	"strings"
	"testing"
	"time"

	lux "github.com/GofMan5/lux"
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

func TestNilContextPaintsWithoutRouting(t *testing.T) {
	var c *Context
	buffer := render.NewBuffer(40, 12)
	canvas := render.NewCanvas(buffer)
	frame := &lux.Frame{Canvas: canvas}
	box := render.Rect{X: 0, Y: 0, W: 20, H: 6}

	c.Begin(frame)
	c.Button("button", canvas, render.Rect{Y: 10, W: 10, H: 1}, "OK", ButtonStyle{})
	c.Checkbox("check", canvas, box, &CheckboxState{}, "check", CheckboxStyle{})
	c.RadioGroup("radio", canvas, box, &RadioState{}, []string{"a", "b"}, RadioStyle{})
	c.List("list", canvas, box, &ListState{}, []string{"a", "b"}, ListStyle{})
	c.Tabs("tabs", canvas, box, &TabsState{}, []string{"a", "b"}, TabsStyle{})
	c.Select("select", canvas, box, &SelectState{Open: true}, []string{"a", "b"}, SelectStyle{})
	c.Table("table", canvas, box, &TableState{}, tableColumns(), 3, func(int) []string { return []string{"1", "n", "s"} }, TableStyle{})
	c.Tree("tree", canvas, box, &TreeState{}, 2, func(i int) TreeRow { return TreeRow{ID: "n", Label: "node"} }, TreeStyle{})
	c.Input("input", canvas, render.Rect{Y: 8, W: 10, H: 1}, &TextInput{Value: "typed"}, InputStyle{})
	c.TextArea("area", canvas, render.Rect{Y: 9, W: 10, H: 1}, &TextArea{Lines: []string{"notes"}}, TextAreaStyle{})
	c.Gauge(canvas, box, &GaugeState{}, GaugeStyle{})
	c.Sparkline(canvas, box, &SparklineState{}, SparklineStyle{})
	c.Spinner(canvas, box, &SpinnerState{}, time.Now(), SpinnerStyle{})
	c.ScrollViewport("scroll", canvas, box, &ScrollState{}, 40, ScrollStyle{})
	c.Shortcut(Shortcut{Target: "button", Kind: Activate})
	if modal := c.Modal("modal", canvas, box, "Title", ModalStyle{}); modal == nil {
		t.Fatal("Modal through a nil Context returned no content canvas")
	}
	c.EndModal()
	c.End(frame)

	for y, want := range map[int]string{8: "typed", 9: "notes", 10: "OK"} {
		if row := bufferRow(buffer, y); !strings.Contains(row, want) {
			t.Errorf("row %d = %q; nil Context did not paint %q", y, row, want)
		}
	}
	if action, consumed := c.Handle(event.KeyPress(event.KeyEnter, 0)); consumed || action != (Action{}) {
		t.Fatalf("Handle = %+v, %v; want nothing routed", action, consumed)
	}
	if c.Focus("button") || c.Focused("button") || c.Hovered("button") || c.Pressed("button") || c.ModalOpen() || c.FocusDelta("scroll") != 0 {
		t.Fatal("nil Context reported routing state")
	}
}
