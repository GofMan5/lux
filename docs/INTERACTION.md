# Interaction model

Snapshot: **2026-08-11**. LUX TUI implements its own immediate-mode interaction
layer; it does not wrap Bubble Tea, tcell, tview, Ratatui, Textual, Ink,
OpenTUI, or another widget framework.

## Lifecycle

```go
func (app *app) Update(next event.Event) lux.Command {
    action, handled := app.UI.Handle(next)
    if handled {
        switch action.Target {
        case "name": app.Name.Apply(action)
        case "items": app.Items.Apply(action, len(app.Values), action.Bounds.H)
        }
        return nil
    }
    return nil
}

func (app *app) View(frame *lux.Frame) {
    app.UI.Begin(frame)
    defer app.UI.End(frame)
    app.UI.Input("name", frame.Canvas, render.Rect{W: 24, H: 1}, &app.Name, ui.InputStyle{})
}
```

Controls register stable IDs and clipped terminal-space rectangles in final
paint order. `End` attaches a snapshot through `Frame.OnPresented`. The router
commits it only after that exact buffer is written, so a moved control in a
dropped or stale frame cannot steal input.

## Implemented semantics

- one focus owner; Tab/BackTab traversal in registration order;
- topmost hit wins; disabled top regions block click-through;
- left pointer capture from press to release, canceled on focus loss, resize,
  removal, or release; activation requires release inside the captured target;
- Enter/Space button activation with Ctrl/Alt/Super rejection;
- arrows, Home/End, PageUp/PageDown, wheel, and click list selection;
- one modal scope with exclusive routing, Escape/outside dismissal, and focus
  restoration;
- grapheme-safe single-line editing, selection, mouse placement, bounded
  paste, horizontal viewport, placeholder, painted caret, read-only/disabled;
- left-button drag selection: the press sets the anchor, captured motion moves
  only the caret, and a drag past either edge walks one grapheme per event so a
  narrow field scrolls. Selecting is not editing, so no undo step is recorded;
- the real terminal cursor follows the focused editable caret, which is what
  IME candidate windows and screen readers track. A caret outside its canvas or
  clip leaves the cursor hidden, so a scrolled-out field cannot claim it;
- wheel scrolling resolves the topmost scrollable region that can still move
  and otherwise bubbles outward through regions that enclose it; arrows, Page
  keys, Home and End bubble the same way from the focused viewport;
- a control a container has scrolled out of view keeps its identity, so Tab
  still reaches it and the container scrolls it back into view before the next
  frame; it registers no hit area, so it cannot be clicked while hidden;
- resize generations invalidate coordinates before a new frame is presented.

The registry is a linear slice. That is the correct cost for normal terminal
control counts; add a spatial index only after a profile demonstrates a problem.

## Nesting without a widget tree

An immediate-mode registry has no parent pointers, so nesting is derived from
two facts the frame already carries. Bubbling uses geometry: a region hands the
wheel or a scroll key only to a scrollable region that fully encloses it.
Registration order alone cannot express nesting, because a region painted
underneath is indistinguishable from a container by order.

The consequence is deliberate: a region that fully contains another and is
painted below it is treated as its parent, which is also the intuitive result.
Partially overlapping neighbours are never treated as parents.

Revealing a focused control uses ownership instead, because geometry cannot
answer it: a control scrolled out of view has no visible rectangle at all, and a
neighbour drawn just below a viewport occupies the same columns as its children.
Every region therefore records the canvas clip it was drawn through, and a
container owns exactly the regions whose clip it contains. `Context.FocusDelta`
reports the rows a container must move; `ScrollViewport` applies it itself, once
per focus change, so scrolling a container away from the focused control is not
undone by the next event.

Controls draw in their own coordinates and let the canvas clip them, so a
control scrolled halfway out of a container stays aligned with its content
instead of sliding to the clip edge. A container that exists only to scroll its
children registers with `ScrollStyle{Passive: true}`: it accepts the wheel but
stays out of Tab traversal, leaving the keyboard to its children.

## Security and platform limits

Paste is untrusted input. The backend caps bracketed paste at 1 MiB; TextInput
repairs invalid UTF-8, converts CR/LF/tab to spaces, drops other controls, and
applies its own whole-grapheme `MaxBytes` cap. It does not emit OSC clipboard
commands. Password editing is omitted because immutable Go strings cannot offer
reliable erasure and cursor/word operations can leak structure.

Legacy VT exposes committed text but not a portable compositionstart/update/end
or candidate rectangle. IME preedit remains emulator-owned, but the terminal
cursor now sits on the caret, which is the position emulators use to place a
candidate window. Bidirectional text is stored and edited logically; visual
UAX #9 cursor behavior is not claimed.

## Deferred by evidence

Implemented follow-up slices:

- presentation-bound shortcuts with focused, modal, and app scopes; exact
  `(Code, Rune, Mod)` matching ignores repeat count, while explicit priority is
  reserved for lifecycle keys;
- fixed-row `ScrollViewport` with O(1) state, O(visible) drawing, range/clamp,
  EnsureVisible, Page/Home/End/wheel, and explicit parent fallback when a child
  cannot move at its boundary;
- TextInput Ctrl-word deletion and byte-bounded cloned undo/redo history;
- an app-supplied synchronous `Clipboard` port. Core ships no OS or OSC 52
  adapter; Copy/Cut/Paste errors remain visible and failed Cut preserves text;
- a hardware caret bound to the presented frame, left-button drag selection,
  and automatic wheel bubbling between enclosing scrollable regions;
- scroll keys bubbling the same way as the wheel, and focus-driven reveal so Tab
  reaches a control its container has scrolled out of view.

Still deferred by evidence:

1. variable-height rows;
2. double-click word selection and coalesced edit history;
3. Kitty associated-text/keyboard negotiation and restoration;
4. nested parent capture and bubbling for pointer input other than the wheel,
   and a semantic accessibility bridge;
5. active capability negotiation: colour depth, synchronized output and Unicode
   behaviour are still inferred from the environment rather than queried.

## Sources

Accessed 2026-08-09:

- Unicode grapheme and word boundaries: <https://www.unicode.org/reports/tr29/>
- Unicode database version: <https://www.unicode.org/Public/UCD/latest/ReadMe.txt>
- uniseg v0.4.7: <https://pkg.go.dev/github.com/rivo/uniseg>
- xterm mouse, focus, bracketed paste: <https://invisible-island.net/xterm/ctlseqs/ctlseqs.html>
- Kitty keyboard associated text: <https://sw.kovidgoyal.net/kitty/keyboard-protocol/>
- Windows VT input: <https://learn.microsoft.com/en-us/windows/console/console-virtual-terminal-sequences>
- Windows key records: <https://learn.microsoft.com/en-us/windows/console/key-event-record-str>
- Windows IME model: <https://learn.microsoft.com/en-us/windows/apps/design/input/input-method-editors>
- WezTerm IME constraints: <https://wezterm.org/config/lua/config/use_ime.html>
- WAI focus and keyboard principles (semantic reference, not a VT claim):
  <https://www.w3.org/WAI/ARIA/apg/practices/keyboard-interface/>
- OSC 52 clipboard security context: <https://invisible-island.net/xterm/ctlseqs/ctlseqs.html>
- tmux clipboard security: <https://github.com/tmux/tmux/wiki/Clipboard>
- WAI listbox semantics: <https://www.w3.org/WAI/ARIA/apg/patterns/listbox/>
- Flutter shortcut scopes: <https://api.flutter.dev/flutter/widgets/Shortcuts-class.html>
