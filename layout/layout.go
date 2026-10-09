// Package layout provides small, content-driven responsive layout primitives.
package layout

import "github.com/lux-tui/lux/render"

// Class is a coarse disclosure level derived from available cells.
type Class uint8

const (
	Tiny Class = iota
	Compact
	Medium
	Wide
)

// String names the class for logs and diagnostics.
func (c Class) String() string {
	switch c {
	case Compact:
		return "Compact"
	case Medium:
		return "Medium"
	case Wide:
		return "Wide"
	default:
		return "Tiny"
	}
}

// Classify uses both dimensions: wide but one-line terminals remain compact.
func Classify(area render.Rect) Class {
	if area.W < 12 || area.H < 3 {
		return Tiny
	}
	if area.W < 60 || area.H < 16 {
		return Compact
	}
	if area.W < 110 || area.H < 28 {
		return Medium
	}
	return Wide
}

// Axis selects rows or columns.
type Axis uint8

const (
	Horizontal Axis = iota
	Vertical
)

// Constraint describes a child's main-axis size. Basis is preferred cells,
// Min and Max clamp it, Grow shares spare cells, Shrink shares a deficit.
type Constraint struct {
	Basis  int
	Min    int
	Max    int
	Grow   int
	Shrink int
}

// Flex divides area deterministically. It uses caller storage and returns a
// slice of exactly len(items), making responsive layout allocation-free.
func Flex(into []render.Rect, area render.Rect, axis Axis, gap int, items ...Constraint) []render.Rect {
	if len(into) < len(items) {
		panic("layout: destination is smaller than constraints")
	}
	into = into[:len(items)]
	if len(items) == 0 {
		return into
	}
	gap = max(gap, 0)
	available := area.W
	if axis == Vertical {
		available = area.H
	}
	available = max(available, 0)
	if len(items) > 1 {
		// Reserve a cell per child before spending scarce space on decoration.
		gap = min(gap, max(available-len(items), 0)/(len(items)-1))
	}
	available = max(available-gap*(len(items)-1), 0)
	used := 0
	for index, item := range items {
		maximum := item.Max
		if maximum <= 0 {
			maximum = available
		}
		into[index].W = min(max(item.Basis, max(item.Min, 0)), maximum)
		used += into[index].W
	}
	delta := available - used
	if delta > 0 {
		distribute(into, items, delta, true)
	} else if delta < 0 {
		distribute(into, items, -delta, false)
		used = 0
		for index := range into {
			used += into[index].W
		}
		// At tiny sizes minimums become preferences: containment beats overflow.
		for index := len(into) - 1; used > available && index >= 0; index-- {
			cut := min(into[index].W, used-available)
			into[index].W -= cut
			used -= cut
		}
	}
	position := 0
	for index := range into {
		size := into[index].W
		if axis == Horizontal {
			into[index] = render.Rect{X: area.X + position, Y: area.Y, W: size, H: max(area.H, 0)}
		} else {
			into[index] = render.Rect{X: area.X, Y: area.Y + position, W: max(area.W, 0), H: size}
		}
		position += size + gap
	}
	return into
}

func distribute(sizes []render.Rect, items []Constraint, amount int, grow bool) {
	for amount > 0 {
		weight := 0
		for index, item := range items {
			if grow && item.Grow > 0 && (item.Max <= 0 || sizes[index].W < item.Max) {
				weight += item.Grow
			} else if !grow && item.Shrink > 0 && sizes[index].W > max(item.Min, 0) {
				weight += item.Shrink
			}
		}
		if weight == 0 {
			return
		}
		changed := 0
		for index, item := range items {
			w := item.Grow
			room := item.Max - sizes[index].W
			if item.Max <= 0 {
				room = amount
			}
			if !grow {
				w, room = item.Shrink, sizes[index].W-max(item.Min, 0)
			}
			if w <= 0 || room <= 0 {
				continue
			}
			share := max(1, amount*w/weight)
			share = min(share, room, amount-changed)
			if grow {
				sizes[index].W += share
			} else {
				sizes[index].W -= share
			}
			changed += share
			if changed == amount {
				break
			}
		}
		if changed == 0 {
			return
		}
		amount -= changed
	}
}
