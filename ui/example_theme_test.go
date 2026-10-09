package ui_test

import (
	"fmt"

	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/ui"
)

// ExampleContext_UseTheme shows the theme filling zero-value styles: a tab
// strip painted with TabsStyle{} under the default theme draws its selected
// tab bold and its unselected tabs dim. Switching to an explicitly empty
// Theme opts every role out, and the same zero-value style falls back to the
// widget's own unstyled default — the restyle needs no code outside the
// UseTheme call.
func ExampleContext_UseTheme() {
	buffer := render.NewBuffer(12, 1)
	canvas := render.NewCanvas(buffer)
	var ctx ui.Context
	state := ui.TabsState{Selected: 1}
	labels := []string{"aa", "bb"}
	rect := render.Rect{W: 12, H: 1}

	ctx.Tabs("pages", canvas, rect, &state, labels, ui.TabsStyle{})
	selected, _ := buffer.CellOK(5, 0)
	unselected, _ := buffer.CellOK(1, 0)
	fmt.Println("default:", selected.Style.Has(render.AttrBold), unselected.Style.Has(render.AttrDim))

	ctx.UseTheme(ui.Theme{})
	ctx.Tabs("pages", canvas, rect, &state, labels, ui.TabsStyle{})
	selected, _ = buffer.CellOK(5, 0)
	unselected, _ = buffer.CellOK(1, 0)
	fmt.Println("opted out:", selected.Style.Has(render.AttrBold), unselected.Style.Has(render.AttrDim))
	// Output:
	// default: true true
	// opted out: false false
}
