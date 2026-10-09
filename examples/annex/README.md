# annex — LUX TUI sideband demo

Taskbar progress (OSC 9;4), desktop notifications (OSC 9;777), and a
write-only clipboard (OSC 52), queued from the frame. All three are
opt-in run options; this demo arms them all.

Run it in a terminal that implements the sequences to see the effects —
Windows Terminal draws the taskbar progress, iTerm2/kitty raise the
toast, tmux with `set-clipboard on` decodes the clipboard write. Run it
under `tmux pipe-pane` (or any raw capture) to see the bytes themselves:
the annexes ride the frame write, after the diff, and the leave sequence
ends with the `9;4;0` progress reset.

Keys: `space` walks the progress states (none → indeterminate → normal →
error → paused), `a` bumps the percent, `n` queues a toast, `c` queues a
clipboard write, `q` quits.

The View re-states the progress every frame — the state, not an event —
so the bar survives suspend/resume for free. Toasts and clipboard writes
are one-shot: Update records the intent, View flushes it exactly once.
