// Package ui provides immediate-mode interaction state for LUX applications.
package ui

import (
	"sync"

	lux "github.com/lux-tui/lux"
	"github.com/lux-tui/lux/event"
	"github.com/lux-tui/lux/render"
)

// ID is a stable control identity. IDs must be unique within a frame.
type ID string

// Flags describe the interaction supported by a region.
type Flags uint8

const (
	Focusable Flags = 1 << iota
	Activatable
	Editable
	Scrollable
	Disabled
)

// RegionOptions customizes event-local coordinates without changing visual
// hit bounds. ContentHeight is useful for lists whose blank tail still accepts
// wheel scrolling but must not select a nonexistent row.
type RegionOptions struct {
	// ContentHeight limits press/click rows while preserving wheel routing.
	// Zero means no pressable content; Region uses -1 for the full bounds.
	ContentHeight int
}

// ActionKind describes a routed interaction.
type ActionKind uint8

const (
	None ActionKind = iota
	Focus
	Activate
	Input
	Scroll
	Dismiss
)

// Action is one interaction routed to a stable target. X and Y are relative
// to the target region for pointer actions.
type Action struct {
	Target ID
	Kind   ActionKind
	Event  event.Event
	Bounds render.Rect
	X      int
	Y      int
}

type region struct {
	id            ID
	bounds        render.Rect
	originX       int
	originY       int
	flags         Flags
	modal         ID
	contentHeight int
}

type modal struct {
	id     ID
	bounds render.Rect
}

type snapshot struct {
	regions   []region
	shortcuts []Shortcut
	modal     modal
	focus     ID
}

// Context owns focus and pointer routing. Its zero value is ready to use.
// Register regions in paint order between Begin and End; the last matching
// region receives pointer input. Context must not be copied after first use.
type Context struct {
	mu sync.Mutex

	generation  uint64
	sequence    uint64
	committedAt uint64
	width       int
	height      int
	sizeKnown   bool
	committed   snapshot

	building      []region
	shortcuts     []Shortcut
	buildingModal modal
	currentModal  ID
	buildingGen   uint64
	viewFocus     ID
	begun         bool

	focused      ID
	hovered      ID
	pressed      ID
	captured     ID
	captureLeft  bool
	restoreFocus ID

	pointerKnown bool
	pointerX     int
	pointerY     int
}

// Begin starts registration for frame. A viewport change immediately
// invalidates old hit geometry; focus identity is retained when possible.
func (c *Context) Begin(frame *lux.Frame) {
	if frame == nil {
		return
	}
	width, height := frame.Size()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resizeLocked(width, height)
	c.building = c.building[:0]
	c.shortcuts = c.shortcuts[:0]
	c.buildingModal = modal{}
	c.currentModal = ""
	c.buildingGen = c.generation
	c.viewFocus = c.focused
	if c.committed.modal.id != "" {
		// Paint the base with the focus it will regain if the modal is omitted.
		c.viewFocus = c.restoreFocus
	}
	c.begun = true
}

// Region registers a clipped terminal-space hit region in paint order.
func (c *Context) Region(id ID, canvas *render.Canvas, bounds render.Rect, flags Flags) {
	c.RegionWithOptions(id, canvas, bounds, flags, RegionOptions{ContentHeight: -1})
}

// RegionWithOptions registers a clipped region with optional content limits.
func (c *Context) RegionWithOptions(id ID, canvas *render.Canvas, bounds render.Rect, flags Flags, options RegionOptions) {
	if id == "" || canvas == nil {
		return
	}
	absolute := canvas.GlobalRect(bounds)
	if absolute.Empty() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.begun {
		return
	}
	for _, existing := range c.building {
		if existing.id == id {
			panic("ui: duplicate control ID " + string(id))
		}
	}
	contentHeight := options.ContentHeight
	originX, originY := canvas.GlobalPoint(bounds.X, bounds.Y)
	if contentHeight < 0 {
		contentHeight = -1
	} else {
		// Preserve local row coordinates when the region is clipped at its top.
		contentHeight = min(contentHeight, max(absolute.Bottom()-originY, 0))
	}
	next := region{id: id, bounds: absolute, originX: originX, originY: originY, flags: flags, modal: c.currentModal, contentHeight: contentHeight}
	c.building = append(c.building, next)
	if c.viewFocus == "" && focusable(next, c.currentModal) {
		c.viewFocus = id
	}
}

// End binds the registered geometry to this exact presented frame. Dropped,
// failed, and pre-resize frames never become interactive.
func (c *Context) End(frame *lux.Frame) {
	if frame == nil {
		return
	}
	c.mu.Lock()
	if !c.begun {
		c.mu.Unlock()
		return
	}
	c.sequence++
	sequence, generation := c.sequence, c.buildingGen
	next := snapshot{
		regions:   append([]region(nil), c.building...),
		shortcuts: append([]Shortcut(nil), c.shortcuts...),
		modal:     c.buildingModal,
		focus:     c.viewFocus,
	}
	if !focusValid(next, next.focus) {
		next.focus = firstFocus(next)
	}
	c.currentModal = ""
	c.begun = false
	c.mu.Unlock()

	frame.OnPresented(func() { c.commit(generation, sequence, next) })
}

// BeginModal starts a modal scope. Regions registered until EndModal receive
// input exclusively once this frame is presented.
func (c *Context) BeginModal(id ID, canvas *render.Canvas, bounds render.Rect) {
	if id == "" || canvas == nil {
		return
	}
	absolute := canvas.GlobalRect(bounds)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.begun {
		return
	}
	c.buildingModal = modal{id: id, bounds: absolute}
	c.currentModal = id
	if c.committed.modal.id == id {
		c.viewFocus = c.focused
	} else {
		c.viewFocus = ""
	}
}

// EndModal ends the current modal registration scope.
func (c *Context) EndModal() {
	c.mu.Lock()
	c.currentModal = ""
	c.mu.Unlock()
}

// Focus moves keyboard focus to a currently visible, enabled region.
func (c *Context) Focus(id ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !focusValid(c.committed, id) {
		return false
	}
	c.focused = id
	if c.begun {
		c.viewFocus = id
	}
	return true
}

// Focused reports whether id has keyboard focus. During View it reports the
// focus intended for the frame being built, so first paints are correct.
func (c *Context) Focused(id ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.begun {
		return id != "" && c.viewFocus == id
	}
	return id != "" && c.focused == id
}

// Hovered reports whether the pointer is over id in the last presented frame.
func (c *Context) Hovered(id ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return id != "" && c.hovered == id
}

// Pressed reports whether an activatable id owns the current left press.
func (c *Context) Pressed(id ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return id != "" && c.pressed == id
}

// Handle routes one terminal event against the last successfully presented
// snapshot. The bool reports whether the interaction was consumed.
func (c *Context) Handle(next event.Event) (Action, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch next.Kind {
	case event.Resize:
		c.resizeLocked(next.Width, next.Height)
		return Action{}, false
	case event.Focus:
		if !next.Focused {
			c.hovered, c.pressed, c.captured = "", "", ""
			c.captureLeft = false
			c.pointerKnown = false
		}
		return Action{}, false
	case event.Mouse:
		return c.handleMouseLocked(next)
	case event.Key:
		return c.handleKeyLocked(next)
	case event.Text, event.Paste:
		if target, ok := c.focusedRegionLocked(); ok && target.flags&Editable != 0 {
			return actionFor(target, Input, next), true
		}
	}
	if c.committed.modal.id != "" && (next.Kind == event.Key || next.Kind == event.Text || next.Kind == event.Paste) {
		return Action{}, true
	}
	return Action{}, false
}

func (c *Context) handleKeyLocked(next event.Event) (Action, bool) {
	key := next.Key
	plain := key.Mod&(event.ModCtrl|event.ModAlt|event.ModSuper) == 0
	if key.Code == event.KeyEscape && c.committed.modal.id != "" {
		return Action{Target: c.committed.modal.id, Kind: Dismiss, Event: next}, true
	}
	if shortcut, ok := matchPriorityShortcut(c.committed, key); ok {
		return shortcutAction(shortcut, next), true
	}
	if shortcut, ok := matchShortcut(c.committed, key, ScopeFocused, c.focused); ok {
		return shortcutAction(shortcut, next), true
	}
	if c.committed.modal.id != "" {
		if shortcut, ok := matchShortcut(c.committed, key, ScopeModal, c.committed.modal.id); ok {
			return shortcutAction(shortcut, next), true
		}
	}
	if plain && (key.Code == event.KeyTab || key.Code == event.KeyBackTab) {
		backward := key.Code == event.KeyBackTab || key.Mod&event.ModShift != 0
		if target, ok := c.stepFocusLocked(backward); ok {
			return actionFor(target, Focus, next), true
		}
		return Action{}, c.committed.modal.id != ""
	}
	target, focused := c.focusedRegionLocked()
	if focused {
		if target.flags&Activatable != 0 && plain && (key.Code == event.KeyEnter || key.Code == event.KeyRune && key.Rune == ' ') {
			return actionFor(target, Activate, next), true
		}
		if target.flags&Editable != 0 {
			return actionFor(target, Input, next), true
		}
		if target.flags&Scrollable != 0 && scrollKey(key.Code) {
			return actionFor(target, Scroll, next), true
		}
	}
	if c.committed.modal.id != "" {
		return Action{}, true
	}
	if shortcut, ok := matchShortcut(c.committed, key, ScopeApp, ""); ok {
		return shortcutAction(shortcut, next), true
	}
	return Action{}, false
}

func (c *Context) handleMouseLocked(next event.Event) (Action, bool) {
	mouse := next.Mouse
	c.pointerKnown, c.pointerX, c.pointerY = true, mouse.X, mouse.Y
	target, found := hit(c.committed, mouse.X, mouse.Y, 0)
	if found {
		c.hovered = target.id
	} else {
		c.hovered = ""
	}

	if mouse.Action == event.MouseWheel {
		if scrollTarget, ok := hit(c.committed, mouse.X, mouse.Y, Scrollable); ok {
			return actionFor(scrollTarget, Scroll, next), true
		}
		return Action{}, c.committed.modal.id != ""
	}

	if mouse.Action == event.MousePress {
		if c.outsideModalLocked(mouse.X, mouse.Y) {
			c.pressed, c.captured = "", ""
			c.captureLeft = false
			return Action{Target: c.committed.modal.id, Kind: Dismiss, Event: next, X: mouse.X - c.committed.modal.bounds.X, Y: mouse.Y - c.committed.modal.bounds.Y}, true
		}
		if !found {
			return Action{}, c.committed.modal.id != ""
		}
		if target.contentHeight >= 0 && mouse.Y-target.originY >= target.contentHeight {
			return Action{}, true
		}
		if mouse.Button != event.MouseLeft {
			return Action{}, true
		}
		c.captured, c.captureLeft = target.id, true
		if target.flags&Activatable != 0 {
			c.pressed = target.id
		}
		if target.flags&Focusable != 0 {
			changed := c.focused != target.id
			c.focused = target.id
			if changed && target.flags&(Editable|Scrollable) == 0 {
				return actionFor(target, Focus, next), true
			}
		}
		if target.flags&Editable != 0 {
			return actionFor(target, Input, next), true
		}
		if target.flags&Scrollable != 0 {
			return actionFor(target, Scroll, next), true
		}
		return Action{}, true
	}

	if mouse.Action == event.MouseRelease {
		captured, left := c.captured, c.captureLeft
		c.captured, c.pressed, c.captureLeft = "", "", false
		insideContent := target.contentHeight < 0 || mouse.Y-target.originY < target.contentHeight
		if left && found && target.id == captured && target.flags&Activatable != 0 && insideContent {
			return actionFor(target, Activate, next), true
		}
		return Action{}, captured != "" || c.committed.modal.id != ""
	}

	return Action{}, found || c.captured != "" || c.committed.modal.id != ""
}

func (c *Context) commit(generation, sequence uint64, next snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation || sequence <= c.committedAt {
		return
	}
	previousModal := c.committed.modal.id
	c.committed, c.committedAt = next, sequence

	switch {
	case previousModal == "" && next.modal.id != "":
		c.restoreFocus = c.focused
		c.focused = next.focus
	case previousModal != "" && next.modal.id == "":
		if focusValid(next, c.restoreFocus) {
			c.focused = c.restoreFocus
		} else {
			c.focused = next.focus
		}
		c.restoreFocus = ""
	case previousModal != next.modal.id:
		c.focused = next.focus
	case !focusValid(next, c.focused):
		c.focused = next.focus
	}
	if !focusValid(next, c.focused) {
		c.focused = firstFocus(next)
	}

	if c.pointerKnown {
		if target, ok := hit(next, c.pointerX, c.pointerY, 0); ok {
			c.hovered = target.id
		} else {
			c.hovered = ""
		}
	}
	if _, ok := regionByID(next, c.captured); !ok {
		c.captured, c.pressed, c.captureLeft = "", "", false
	}
}

func (c *Context) resizeLocked(width, height int) {
	if c.sizeKnown && c.width == width && c.height == height {
		return
	}
	c.sizeKnown, c.width, c.height = true, width, height
	c.generation++
	// Keep only modality and focus identity while terminal coordinates are stale.
	c.committed.regions = nil
	c.committed.shortcuts = nil
	c.committed.modal.bounds = render.Rect{}
	c.hovered, c.pressed, c.captured = "", "", ""
	c.captureLeft = false
	c.pointerKnown = false
}

func (c *Context) stepFocusLocked(backward bool) (region, bool) {
	regions := c.committed.regions
	eligible := func(item region) bool { return focusable(item, c.committed.modal.id) }
	start := -1
	for index, item := range regions {
		if eligible(item) && item.id == c.focused {
			start = index
			break
		}
	}
	for step := 1; step <= len(regions); step++ {
		index := start + step
		if backward {
			index = start - step
		}
		index %= len(regions)
		if index < 0 {
			index += len(regions)
		}
		if eligible(regions[index]) {
			c.focused = regions[index].id
			return regions[index], true
		}
	}
	return region{}, false
}

func (c *Context) focusedRegionLocked() (region, bool) {
	if target, ok := regionByID(c.committed, c.focused); ok && focusable(target, c.committed.modal.id) {
		return target, true
	}
	if target, ok := firstFocusRegion(c.committed); ok {
		c.focused = target.id
		return target, true
	}
	return region{}, false
}

func (c *Context) outsideModalLocked(x, y int) bool {
	active := c.committed.modal
	return active.id != "" && !active.bounds.Empty() && !active.bounds.Contains(x, y)
}

func hit(state snapshot, x, y int, require Flags) (region, bool) {
	for index := len(state.regions) - 1; index >= 0; index-- {
		item := state.regions[index]
		if state.modal.id != "" && item.modal != state.modal.id {
			continue
		}
		if !item.bounds.Contains(x, y) {
			continue
		}
		if item.flags&Disabled != 0 {
			return region{}, false
		}
		if item.flags&(Focusable|Activatable|Editable|Scrollable) != 0 && (require == 0 || item.flags&require != 0) {
			return item, true
		}
	}
	return region{}, false
}

func regionByID(state snapshot, id ID) (region, bool) {
	if id == "" {
		return region{}, false
	}
	for index := len(state.regions) - 1; index >= 0; index-- {
		item := state.regions[index]
		if item.id == id && item.flags&Disabled == 0 && (state.modal.id == "" || item.modal == state.modal.id) {
			return item, true
		}
	}
	return region{}, false
}

func focusable(item region, activeModal ID) bool {
	return item.flags&Focusable != 0 && item.flags&Disabled == 0 && (activeModal == "" || item.modal == activeModal)
}

func focusValid(state snapshot, id ID) bool {
	item, ok := regionByID(state, id)
	return ok && focusable(item, state.modal.id)
}

func firstFocus(state snapshot) ID {
	item, _ := firstFocusRegion(state)
	return item.id
}

func firstFocusRegion(state snapshot) (region, bool) {
	for _, item := range state.regions {
		if focusable(item, state.modal.id) {
			return item, true
		}
	}
	return region{}, false
}

func scrollKey(code event.KeyCode) bool {
	switch code {
	case event.KeyUp, event.KeyDown, event.KeyHome, event.KeyEnd, event.KeyPageUp, event.KeyPageDown:
		return true
	default:
		return false
	}
}

func actionFor(target region, kind ActionKind, next event.Event) Action {
	action := Action{Target: target.id, Kind: kind, Event: next, Bounds: target.bounds}
	if next.Kind == event.Mouse {
		action.X = next.Mouse.X - target.originX
		action.Y = next.Mouse.Y - target.originY
	}
	return action
}
