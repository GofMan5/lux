# LUX Studio

`16-studio` is the hands-on tour: one polished app that puts nearly every LUX
feature under your fingers at once. Start here if you want to see and touch
the library before reading any code.

```sh
go run ./examples/16-studio
```

It needs a 96×28 terminal or larger and asks for more room otherwise.

| Page | What to try |
| --- | --- |
| **1 Dashboard** | Live metric cards whose headline numbers ride springs to each sample, sparklines, threshold-colored gauges. `d` starts or cancels a five-stage deploy pipeline: an async command per step, a spinner, taskbar progress (OSC 9;4), a toast and a desktop notification when it finishes. |
| **2 Services** | A sortable table (click a header or use the table keys) with a type-to-filter field (`/` focuses it, `Esc` returns to the table). The detail pane wraps text and renders the URL as an OSC 8 hyperlink. *Copy URL* writes it to the system clipboard (OSC 52); *Restart* opens a confirmation modal, then recovers the service asynchronously. |
| **3 Files** | A project tree beside a `TextArea` editor: mouse and Shift+arrow selection, `Ctrl+Z`/`Ctrl+Y`, `Ctrl+C` copies the selection, `Ctrl+S` saves; unsaved edits survive switching files. Wide CJK, emoji and combining marks wrap correctly. `Esc` hands focus back to the tree. |
| **4 Motion** | Click or drag the spring track to retarget mid-flight with momentum, pick a tween easing, watch a software-rendered 3D cube or gem. `Space` pauses. Reduced motion jumps straight to targets. |
| **5 Settings** | Three themes (Aurora, Ember, Matrix) applied through `ui.Palette.Theme`, reduced motion, the stats toggle, the dashboard sample rate, Notify/Copy test buttons, the detected terminal capabilities, and a live event inspector for every key, mouse and resize event. |

Everywhere: `Ctrl+P` opens a fuzzy command palette, `t` cycles themes, `F1`
or `?` shows the key map, `Tab`/`Shift+Tab` move focus, and `q` or `Ctrl+Q`
quits. The header shows the runtime's own frame metrics.

The app sleeps when nothing changes: Files and Settings request no frames
while idle, a toast wakes the loop once at its expiry, and only the Motion lab
animates continuously (`TestIdlePagesSleep` pins this). Activity, event and
metric histories are capped, and `TestLongSessionMemoryIsBounded`
(`LUX_SOAK=1`) checks that a 150-cycle tour with resizes keeps the heap flat
and leaks no goroutines.
