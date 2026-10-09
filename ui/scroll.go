package ui

import (
	"github.com/GofMan5/lux/event"
	"github.com/GofMan5/lux/render"
)

// ScrollState stores the first visible fixed-height row.
type ScrollState struct {
	Offset int
}

// VisibleRange is a half-open range [First, Last).
type VisibleRange struct {
	First int
	Last  int
}

// Range clamps the state to the current content and returns visible rows.
func (s *ScrollState) Range(content, rows int) VisibleRange {
	content, rows = max(content, 0), max(rows, 0)
	if s == nil {
		return VisibleRange{}
	}
	s.Offset = min(max(s.Offset, 0), max(content-rows, 0))
	return VisibleRange{First: s.Offset, Last: s.Offset + min(rows, content-s.Offset)}
}

// Apply scrolls in response to a routed Scroll action. False for a movement at
// a boundary lets the caller offer that same key or wheel action to a parent
// viewport; pointer presses only focus and must not be propagated this way.
// Key releases are inert and report no movement.
func (s *ScrollState) Apply(action Action, content, rows int) bool {
	if s == nil {
		return false
	}
	if action.Event.Kind == event.Key && action.Event.Key.Action == event.ActionRelease {
		return false
	}
	before := s.Offset
	s.Range(content, rows)
	limit := max(max(content, 0)-max(rows, 0), 0)
	if action.Kind != Scroll {
		return s.Offset != before
	}

	delta, absolute := 0, -1
	switch action.Event.Kind {
	case event.Key:
		switch action.Event.Key.Code {
		case event.KeyUp:
			delta = -1
		case event.KeyDown:
			delta = 1
		case event.KeyHome:
			absolute = 0
		case event.KeyEnd:
			absolute = limit
		case event.KeyPageUp:
			delta = -max(rows, 1)
		case event.KeyPageDown:
			delta = max(rows, 1)
		}
	case event.Mouse:
		if action.Event.Mouse.Action == event.MouseWheel {
			switch action.Event.Mouse.Button {
			case event.MouseWheelUp:
				delta = -max(rows/3, 1)
			case event.MouseWheelDown:
				delta = max(rows/3, 1)
			}
		}
	}

	if absolute >= 0 {
		s.Offset = absolute
	} else if delta < 0 {
		s.Offset -= min(-delta, s.Offset)
	} else if delta > 0 {
		s.Offset += min(delta, limit-s.Offset)
	}
	return s.Offset != before
}

// EnsureVisible minimally scrolls until index is inside the viewport.
func (s *ScrollState) EnsureVisible(index, content, rows int) {
	if s == nil {
		return
	}
	range_ := s.Range(content, rows)
	if content <= 0 || rows <= 0 {
		return
	}
	index = min(max(index, 0), content-1)
	if index < range_.First {
		s.Offset = index
	} else if index >= range_.Last {
		s.Offset = index - rows + 1
	}
	s.Range(content, rows)
}

// ScrollStyle configures viewport interaction. A Passive viewport routes the
// wheel without joining Tab traversal, which is what a container whose children
// own the keyboard needs.
type ScrollStyle struct {
	Disabled bool
	Passive  bool
}

// ScrollViewport registers a fixed-row scrolling region and returns its
// clipped local canvas. Draw only the range returned by state.Range.
func (c *Context) ScrollViewport(id ID, canvas *render.Canvas, rect render.Rect, state *ScrollState, content int, style ScrollStyle) *render.Canvas {
	if canvas == nil || state == nil {
		return nil
	}
	view := canvas.Subview(rect)
	_, rows := view.Size()
	// Tab can move focus to a control this viewport has scrolled out of view, so
	// reveal it before the visible range is measured.
	state.Offset += c.revealDelta(id)
	visible := state.Range(content, rows)
	flags := Focusable | Scrollable
	if style.Passive {
		flags = Scrollable
	}
	if style.Disabled {
		flags |= Disabled
	}
	c.RegionWithOptions(id, canvas, rect, flags, RegionOptions{
		ContentHeight: visible.Last - visible.First,
		AtTop:         state.Offset <= 0,
		AtBottom:      state.Offset >= max(max(content, 0)-max(rows, 0), 0),
	})
	return view
}
