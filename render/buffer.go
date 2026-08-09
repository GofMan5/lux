package render

// Change is one changed cell in a frame diff.
type Change struct {
	X    int
	Y    int
	Cell Cell
}

// Buffer owns a rectangular, row-major grid of canonical cells.
type Buffer struct {
	width  int
	height int
	cells  []Cell
}

// NewBuffer returns a blank buffer. Negative dimensions are treated as zero.
func NewBuffer(width, height int) *Buffer {
	buffer := &Buffer{}
	buffer.Resize(width, height)
	return buffer
}

// Size returns the buffer dimensions in cells.
func (b *Buffer) Size() (width, height int) {
	if b == nil {
		return 0, 0
	}
	return b.width, b.height
}

// Bounds returns the whole buffer rectangle.
func (b *Buffer) Bounds() Rect {
	width, height := b.Size()
	return Bounds(width, height)
}

// Len returns the number of cells, including wide-grapheme continuations.
func (b *Buffer) Len() int {
	if b == nil {
		return 0
	}
	return len(b.cells)
}

// Cell returns a cell or the zero Cell when the coordinates are outside b.
// CellOK distinguishes an outside cell from a continuation cell.
func (b *Buffer) Cell(x, y int) Cell {
	cell, _ := b.CellOK(x, y)
	return cell
}

// CellOK returns a cell and whether the coordinates are inside b.
func (b *Buffer) CellOK(x, y int) (Cell, bool) {
	if b == nil || x < 0 || y < 0 || x >= b.width || y >= b.height {
		return Cell{}, false
	}
	return b.cells[y*b.width+x], true
}

// CellAt returns the row-major cell at index, or the zero Cell when outside b.
func (b *Buffer) CellAt(index int) Cell {
	if b == nil || index < 0 || index >= len(b.cells) {
		return Cell{}
	}
	return b.cells[index]
}

// EqualCell compares one coordinate in two buffers.
func (b *Buffer) EqualCell(other *Buffer, x, y int) bool {
	left, leftOK := b.CellOK(x, y)
	right, rightOK := other.CellOK(x, y)
	return leftOK == rightOK && (!leftOK || left == right)
}

// EqualIndex compares one row-major index in two buffers.
func (b *Buffer) EqualIndex(other *Buffer, index int) bool {
	if b == nil || other == nil || index < 0 || index >= len(b.cells) || index >= len(other.cells) {
		return b == nil && other == nil
	}
	return b.cells[index] == other.cells[index]
}

// Equal reports whether two buffers have identical dimensions and cells.
func (b *Buffer) Equal(other *Buffer) bool {
	if b == nil || other == nil {
		return b == nil && other == nil
	}
	if b.width != other.width || b.height != other.height {
		return false
	}
	for index := range b.cells {
		if b.cells[index] != other.cells[index] {
			return false
		}
	}
	return true
}

// Clone returns an independent copy of b.
func (b *Buffer) Clone() *Buffer {
	if b == nil {
		return nil
	}
	clone := &Buffer{width: b.width, height: b.height, cells: make([]Cell, len(b.cells))}
	copy(clone.cells, b.cells)
	return clone
}

// CopyFrom replaces b with an independent copy of source.
func (b *Buffer) CopyFrom(source *Buffer) {
	if b == nil || b == source {
		return
	}
	if source == nil {
		b.width, b.height, b.cells = 0, 0, nil
		return
	}
	b.width, b.height = source.width, source.height
	b.cells = append(b.cells[:0], source.cells...)
}

// Resize changes the dimensions while preserving their overlapping top-left
// area. A wide grapheme clipped by the new edge is replaced with blanks.
func (b *Buffer) Resize(width, height int) bool {
	if b == nil {
		return false
	}
	width, height = max(width, 0), max(height, 0)
	if width == b.width && height == b.height {
		return false
	}
	if height != 0 && width > int(^uint(0)>>1)/height {
		panic("render: buffer dimensions overflow")
	}
	cells := make([]Cell, width*height)
	fillCells(cells, BlankCell)
	copyWidth, copyHeight := min(width, b.width), min(height, b.height)
	for y := 0; y < copyHeight; y++ {
		copy(cells[y*width:y*width+copyWidth], b.cells[y*b.width:y*b.width+copyWidth])
	}
	b.width, b.height, b.cells = width, height, cells
	b.repair()
	return true
}

// Reset clears b to default-style blanks without reallocating.
func (b *Buffer) Reset() { b.Clear(Style{}) }

// Clear fills b with blank cells using style.
func (b *Buffer) Clear(style Style) {
	if b == nil {
		return
	}
	cell := BlankCell
	cell.Style = style
	fillCells(b.cells, cell)
}

// AppendDiff appends cells that differ from previous. If dimensions differ,
// every cell in b is appended; terminal adapters should resize first.
func (b *Buffer) AppendDiff(changes []Change, previous *Buffer) []Change {
	if b == nil {
		return changes
	}
	all := previous == nil || b.width != previous.width || b.height != previous.height
	for index, cell := range b.cells {
		if all || cell != previous.cells[index] {
			changes = append(changes, Change{X: index % b.width, Y: index / b.width, Cell: cell})
		}
	}
	return changes
}

// Diff returns cells that differ from previous.
func (b *Buffer) Diff(previous *Buffer) []Change { return b.AppendDiff(nil, previous) }

func (b *Buffer) put(x, y int, grapheme string, width int, style Style) bool {
	if b == nil || y < 0 || y >= b.height || x < 0 || x >= b.width || width < 1 || width > 2 || x+width > b.width {
		return false
	}
	b.eraseAt(x, y)
	if width == 2 {
		b.eraseAt(x+1, y)
	}
	index := y*b.width + x
	b.cells[index] = Cell{Grapheme: grapheme, Width: uint8(width), Style: style}
	if width == 2 {
		b.cells[index+1] = Cell{Width: 0, Style: style}
	}
	return true
}

func (b *Buffer) replacementFits(clip Rect, x, y, width int) bool {
	left, right := x, x+width-1
	for column := left; column <= right; column++ {
		cell, ok := b.CellOK(column, y)
		if !ok {
			return false
		}
		switch cell.Width {
		case 0:
			left = min(left, column-1)
		case 2:
			right = max(right, column+1)
		}
	}
	return clip.Contains(left, y) && clip.Contains(right, y)
}

func (b *Buffer) appendCombining(x, y int, grapheme string) bool {
	if b == nil || x < 0 || y < 0 || x >= b.width || y >= b.height || grapheme == "" {
		return false
	}
	index := y*b.width + x
	if b.cells[index].Width == 0 {
		if x == 0 {
			return false
		}
		index--
	}
	if b.cells[index].Width != 1 && b.cells[index].Width != 2 {
		return false
	}
	b.cells[index].Grapheme += grapheme
	return true
}

func (b *Buffer) eraseAt(x, y int) {
	if b == nil || x < 0 || y < 0 || x >= b.width || y >= b.height {
		return
	}
	index := y*b.width + x
	switch b.cells[index].Width {
	case 0:
		b.cells[index] = BlankCell
		if x > 0 && b.cells[index-1].Width == 2 {
			b.cells[index-1] = BlankCell
		}
	case 2:
		b.cells[index] = BlankCell
		if x+1 < b.width {
			b.cells[index+1] = BlankCell
		}
	default:
		b.cells[index] = BlankCell
	}
}

func (b *Buffer) repair() {
	for y := 0; y < b.height; y++ {
		for x := 0; x < b.width; x++ {
			index := y*b.width + x
			switch b.cells[index].Width {
			case 1:
				if b.cells[index].Grapheme == "" {
					b.cells[index].Grapheme = " "
				}
			case 2:
				if b.cells[index].Grapheme == "" || x+1 == b.width {
					b.cells[index] = BlankCell
					continue
				}
				b.cells[index+1] = Cell{Width: 0, Style: b.cells[index].Style}
				x++
			default:
				b.cells[index] = BlankCell
			}
		}
	}
}

func fillCells(cells []Cell, cell Cell) {
	for index := range cells {
		cells[index] = cell
	}
}

// DoubleBuffer keeps the last presented frame and the frame being drawn.
type DoubleBuffer struct {
	front *Buffer
	back  *Buffer
}

// NewDoubleBuffer returns two blank buffers with matching dimensions.
func NewDoubleBuffer(width, height int) *DoubleBuffer {
	return &DoubleBuffer{front: NewBuffer(width, height), back: NewBuffer(width, height)}
}

// Front is the last presented frame.
func (d *DoubleBuffer) Front() *Buffer {
	if d == nil {
		return nil
	}
	return d.front
}

// Back is the frame applications draw into.
func (d *DoubleBuffer) Back() *Buffer {
	if d == nil {
		return nil
	}
	return d.back
}

// AppendChanges appends cells needed to turn Front into Back.
func (d *DoubleBuffer) AppendChanges(changes []Change) []Change {
	if d == nil {
		return changes
	}
	return d.back.AppendDiff(changes, d.front)
}

// Changes returns cells needed to turn Front into Back.
func (d *DoubleBuffer) Changes() []Change { return d.AppendChanges(nil) }

// Swap marks Back as presented. The new Back retains the older frame so a
// caller can either reset it or redraw only changed regions.
func (d *DoubleBuffer) Swap() {
	if d != nil {
		d.front, d.back = d.back, d.front
	}
}

// Resize resizes both frames while preserving overlapping content.
func (d *DoubleBuffer) Resize(width, height int) {
	if d != nil {
		d.front.Resize(width, height)
		d.back.Resize(width, height)
	}
}

// Reset clears both frames without reallocating.
func (d *DoubleBuffer) Reset() {
	if d != nil {
		d.front.Reset()
		d.back.Reset()
	}
}
