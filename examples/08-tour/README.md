# Guided tour

The tour keeps nine top-level tabs. Choose **7 Widgets**, then press **F2**
to switch between the original controls stand and **Editing widgets**.
The footer advertises the switch; F1 opens the key guide and F4 opens the
compact workbench inspection.

## Workbench inspection

Press **F4** from a stand to inspect the last committed input route without
leaving the tour. The screen keeps the source focus, action target and handled
state, target bounds, viewport size, and the latest presented-frame metrics
(target FPS, present time, and dropped frames). The route and bounds are
captured from the public UI action that committed with the preceding frame;
`Present` arrival by itself is not treated as a routing barrier.

The inspection lists the negotiated `Frame.Capabilities` separately from four
**SIMULATED capability profiles** (Monochrome, ANSI16, ANSI256, and TrueColor).
Those rows are preview labels only: they do not emulate a terminal protocol,
change backend capabilities, or claim support for graphics/image adapters.
The inspection itself does not schedule hidden decorative animation. Press
**F4** or **Escape** to return to the exact stand, editor focus, caret, and
draft.

## Workbench settings

Press **F3** from any stand, including a focused editor. Tab moves between
Theme, Density, and Reduced motion; arrows select a radio option, and
Space toggles the checkbox. **F3 or Escape** returns to the prior stand,
restoring editor focus and caret without resetting drafts. An open picker
or help dialog must be closed first. Settings temporarily own keyboard input;
page-local Space shortcuts and F1 help do not act through this screen.

- **Theme:** `dark`, `light`, `terminal-default`, or `high-contrast`.
  App chrome and controls use a per-tour palette and `Context.UseTheme`,
  not mutable global colors. Focus is underlined as well as colored;
  terminal-default chrome assumes neither foreground nor background color.
  Illustrative color swatches, gradients, and 3D materials keep their data.
- **Density:** `compact` or `comfortable`. Comfortable adds bounded body
  gutters and, where height permits, vertical space; it retains the compact
  editor layout at 80×24. It changes placement, not widget behavior.
- **Reduced motion:** freezes decorative animation; turning it off cannot
  override runtime reduced-motion policy. Work progress remains live.
  The settings screen does not schedule hidden decorative animation.

Preferences last for this run only; the example does not write a config file.

## Editing widgets

This is an **in-memory widget stand**, not a filesystem editor. It never
writes either sample to disk.

- **Tree:** Tab to the tree. Enter or Space collapses/expands `samples`;
  arrows select a leaf, and Enter opens `config.lux` or `notes.txt`.
  Each sample keeps its own draft, caret, scroll position, and undo history.
- **Select:** Tab to the editor-state picker. Enter opens it; arrows change
  the preview. Enter commits; Escape or an outside press cancels without
  clicking through. Tab stays inside the open popup. Focus returns to the
  picker when it closes.
- **TextArea:** Tab to the editor to type or paste multiple lines. Selection,
  soft wrap, Ctrl+Z undo, and Ctrl+Y redo use the existing widget. Typing `q`
  or a tab digit in an editable field does not quit or switch pages.
- **States:** `editable` edits the selected sample; `read-only` remains
  focusable but rejects edits and hides the caret; `disabled` is dimmed and
  skipped by focus navigation. `empty` starts a separate blank draft that
  accepts text without discarding either sample.
- **Limits:** each draft is capped at 16 KiB with a 32 KiB undo budget.
  Switching leaves or pressing F2 does not reset drafts.

At **80×24** the tree sits above the picker and editor; all three controls
are visible without scrolling the page. At **120×40** and **240×80** the
tree is beside the editor. Smaller viewports show an enlargement hint when
there is not enough room for usable controls. Zero-size terminals do not
present frames; restoring the viewport preserves the drafts.

F2 works even while the editor is focused, but does not switch through an
open popup or help dialog. Ctrl+Q is the reliable quit shortcut from editors.
The editing stand does not keep the original spinner/gauge timer running.

## Run

From a checkout, with Go 1.23 or newer:

```sh
# Unix
cd /path/to/lux-tui
go run ./examples/08-tour
```

```powershell
# PowerShell
Set-Location E:\Projects\lux-tui
go run ./examples/08-tour
```

```bat
:: cmd.exe (including a drive change)
cd /d E:\Projects\lux-tui
go run ./examples/08-tour
```

## Deterministic checks

```sh
go test ./examples/08-tour -run 'TestWidgets(Catalog|EditingCatalog)|TestWorkbench' -count=20
go test -race ./examples/08-tour -run 'TestWidgets(Catalog|EditingCatalog)|TestWorkbench' -count=5
```

The tests run the real model through `lux.Run` and the existing public
Backend seam. Their beacon runs **after the frame's router snapshot has
committed**, not merely after a buffer reaches `Present`. Each scripted
input waits for a new committed frame. The zero-size test waits for the
restored frame to acknowledge both resizes, not nonexistent zero-size output.

Saved fixtures at 80×24, 120×40, and 240×80 include exact text positioning,
all-body-cell hashes (graphemes, widths, styles, and links), representative
cell values, hardware caret, focus, and widget geometry. Timing-dependent
header/footer metrics are not included. They verify cell contracts, not
pixel-identical host fonts or a manual live-terminal visual pass.

For an intentional layout/style change, regenerate and inspect the fixture
diff, then rerun without the update flag:

```sh
LUX_UPDATE_SNAPSHOTS=1 go test ./examples/08-tour -run '^TestWidgetsCatalogGeometryAndCellSnapshots$'
go test ./examples/08-tour -run '^TestWidgetsCatalogGeometryAndCellSnapshots$'
```

```powershell
$env:LUX_UPDATE_SNAPSHOTS='1'
go test ./examples/08-tour -run '^TestWidgetsCatalogGeometryAndCellSnapshots$'
Remove-Item Env:LUX_UPDATE_SNAPSHOTS
go test ./examples/08-tour -run '^TestWidgetsCatalogGeometryAndCellSnapshots$'
```

F4 now provides simulated capability preview rows and compact committed-route/frame
diagnostics; these are honest application diagnostics, not backend emulation.
Loading/error catalog states and the separate real project/config application
remain production-plan work. This stand does not claim to complete those
requirements.
