package render

import "testing"

func TestBufferCaretLifecycle(t *testing.T) {
	buffer := NewBuffer(6, 3)
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("a fresh buffer must not request a caret")
	}
	buffer.SetCaret(2, 1)
	if x, y, visible := buffer.Caret(); !visible || x != 2 || y != 1 {
		t.Fatalf("caret = (%d,%d,%t)", x, y, visible)
	}

	// Out-of-range coordinates hide the caret instead of pointing at nothing.
	for _, spot := range [][2]int{{-1, 0}, {0, -1}, {6, 0}, {0, 3}} {
		buffer.SetCaret(2, 1)
		buffer.SetCaret(spot[0], spot[1])
		if _, _, visible := buffer.Caret(); visible {
			t.Fatalf("caret at %v must hide", spot)
		}
	}

	// A wide grapheme's continuation cell snaps back to the leading cell, where
	// a real terminal parks its cursor.
	NewCanvas(buffer).Put(1, 2, "界", Style{})
	buffer.SetCaret(2, 2)
	if x, y, visible := buffer.Caret(); !visible || x != 1 || y != 2 {
		t.Fatalf("continuation caret = (%d,%d,%t), want (1,2,true)", x, y, visible)
	}

	buffer.HideCaret()
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("HideCaret left the caret visible")
	}

	// Each frame restates its caret, so Reset drops it; a resize invalidates the
	// coordinates outright.
	buffer.SetCaret(3, 0)
	buffer.Reset()
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("Reset must hide the caret")
	}
	buffer.SetCaret(3, 0)
	buffer.Resize(9, 4)
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("Resize must hide the caret")
	}
	buffer.Resize(9, 4)
	buffer.SetCaret(3, 0)
	if _, _, visible := buffer.Caret(); !visible {
		t.Fatalf("a no-op resize must keep the caret")
	}
}

func TestBufferCaretTravelsWithCopies(t *testing.T) {
	source := NewBuffer(4, 2)
	source.SetCaret(3, 1)
	clone := source.Clone()
	if x, y, visible := clone.Caret(); !visible || x != 3 || y != 1 {
		t.Fatalf("clone caret = (%d,%d,%t)", x, y, visible)
	}
	target := NewBuffer(2, 2)
	target.CopyFrom(source)
	if x, y, visible := target.Caret(); !visible || x != 3 || y != 1 {
		t.Fatalf("copied caret = (%d,%d,%t)", x, y, visible)
	}
	target.CopyFrom(nil)
	if _, _, visible := target.Caret(); visible {
		t.Fatalf("copying nil must hide the caret")
	}

	// The caret is presentation state: it never appears in a cell diff.
	left, right := NewBuffer(3, 1), NewBuffer(3, 1)
	left.SetCaret(1, 0)
	if !left.Equal(right) || len(left.Diff(right)) != 0 {
		t.Fatalf("caret leaked into cell comparison")
	}
}

func TestCanvasSetCaretRespectsClipAndOrigin(t *testing.T) {
	buffer := NewBuffer(10, 6)
	canvas := NewCanvas(buffer)
	view := canvas.Subview(Rect{X: 3, Y: 2, W: 4, H: 2})
	view.SetCaret(1, 1)
	if x, y, visible := buffer.Caret(); !visible || x != 4 || y != 3 {
		t.Fatalf("subview caret = (%d,%d,%t), want (4,3,true)", x, y, visible)
	}

	buffer.HideCaret()
	view.SetCaret(4, 0)
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("a caret outside the subview must stay hidden")
	}

	clipped := canvas.Clip(Rect{X: 0, Y: 4, W: 10, H: 2})
	clipped.SetCaret(0, 0)
	if _, _, visible := buffer.Caret(); visible {
		t.Fatalf("a caret outside the clip must stay hidden")
	}
	clipped.SetCaret(2, 5)
	if x, y, visible := buffer.Caret(); !visible || x != 2 || y != 5 {
		t.Fatalf("clipped caret = (%d,%d,%t), want (2,5,true)", x, y, visible)
	}
}
