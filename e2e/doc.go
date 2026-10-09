// Package e2e drives a real pseudo-terminal against the public lux API.
//
// The tests live in files constrained to Linux and macOS: they open a pty
// pair, spawn the child app in e2e/childapp attached to the pty as its
// controlling terminal, feed it keystrokes, stop and continue it with
// SIGTSTP/SIGCONT the way a shell does, resize the pty, and assert on the
// bytes that actually crossed the wire. Nothing here imports internal
// packages — the point is that the public API keeps its promises to a real
// kernel terminal, not that internal invariants hold.
//
// The dependency footprint is deliberate: creack/pty and this harness live
// behind this module's own go.mod, so the core module never carries a test
// dependency it does not use at runtime. On platforms without a pty lane
// the package still builds (this file is untagged) and simply has no tests.
package e2e
