# Migration guide

How to move an application between LUX TUI releases. Per the
[v0.x policy](../CHANGELOG.md#versioning-policy-for-v0x), a `0.x.0` minor may
break API and patches never do; every break is listed here with the edit it
needs. [CHANGELOG.md](../CHANGELOG.md) has the full list of changes.

## v0.1.0 → v0.2.0 (candidate: v0.2.0-rc.2)

**No source change is required for code written against v0.1.0 in the usual
style.** No exported identifier was removed, renamed, or re-typed, and no
signature changed. Evidence: the v0.1.0 `examples/` tree, compiled
unchanged in a scratch module that replaces `github.com/GofMan5/lux` with the
current tree, builds and vets clean, and its own tests pass
(`03-textedit`, `04-table`, `05-widgets`, `06-showcase`); the SSH example
vets clean for Linux.

### Compile-time: unkeyed struct literals

New fields were appended to these exported structs. Keyed literals
(`ui.Theme{Text: …}`) are unaffected; positional literals no longer compile.
Switch them to keyed fields:

| Type | New fields |
| --- | --- |
| `lux.Metrics` | `PreparationTime`, `UpdateTime`, `ViewTime`, `DiffEncodeTime`, `EncodeTime`, `FlushTime`, `BackendTiming` |
| `event.Event` | `Data` |
| `ui.ListStyle` | `DisabledAt` |
| `ui.TableState` | `FreeScroll` |
| `ui.Theme` | `Focus` |

`event.Kind` gained `Message` after `Resume`; existing constant values are
unchanged, so stored or logged kinds keep their meaning. A `switch` over
`event.Kind` with no `default` simply ignores `Message` until the app sends
one.

### Behavior changes to check

- **Keyboard focus is now visible under `DefaultTheme`.** The new `Focus`
  role styles the focused row, tab, option, or control distinctly from a
  selected-but-unfocused one. Golden snapshots of focused widgets change. A
  custom `Theme` that leaves `Focus` zero renders exactly as before.
- **Row callbacks run only for rows the clip can paint.** Under a tighter
  `Canvas.ClipRect`, `ui.Table` and `ui.Tree` call `rowAt`, and `ui.List`
  calls `DisabledAt`, only for visible rows. Drawn output, hit testing, and
  Tab order are unchanged; code that counted callback invocations sees fewer.
- **SIGINT and SIGTERM end `Run` with an error on Unix.** The runtime restores
  the terminal and returns `lux.ErrInterrupt` or `lux.ErrTerminated`; map
  them to exit statuses 130 and 143 with `errors.Is`. A Ctrl+C typed in the
  terminal is still a key event delivered to the model.
- **A nil `*ui.Context` is inert.** Its routing methods return false or zero
  and widgets drawn through it paint without taking input. Previously the
  interactive widgets panicked on it, and `Input` and `TextArea` drew
  nothing. `(*lux.Frame)(nil).RequestFrame` no longer panics.
- **A table header click no longer activates the selected row.** The press
  sorts; the release that completes the same click is dropped instead of
  being reported as `Activate`.

### New API worth adopting

- `event.Message` with `event.Event.Data`: a `Command` returns
  `event.Event{Kind: event.Message, Data: result}` to deliver an
  asynchronous application result to `Update` without ending the run
  ([examples/12-async](../examples/12-async/main.go)). Keep `event.Error`
  for transport failures.
- `Frame.OnPresentedWithMetrics` and the per-phase `lux.Metrics` durations:
  measure exactly the frame that reached the terminal.
- `ui.Palette`, `ui.ReferencePalette` and `Palette.Theme`: build a whole
  theme from five colors.
- `render.Canvas.DrawableRect`, `HideCaret` and `PutMeasured` for custom
  controls (see [CUSTOM-CONTROL.md](CUSTOM-CONTROL.md)); `ui.SelectPlacement`
  for laying out around an open select; `Context.ModalOpen` for waiting on
  the router outside `View`.

### Checking your own app

Build and test against the new tree before bumping:

```sh
go get github.com/GofMan5/lux@v0.2.0-rc.2
go build ./... && go vet ./... && go test ./...
```

Then grep for positional literals of the types above and re-record any
focused-widget snapshots.
