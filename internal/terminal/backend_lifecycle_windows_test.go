package terminal

import (
	"bytes"
	"os"
	"syscall"
	"testing"
)

// TestConsoleControlEventRestoresAndReports pins the Windows half of the
// termination contract: an external CTRL_C_EVENT or CTRL_BREAK_EVENT
// (os.Interrupt) and a close event (SIGTERM) both restore the console at
// once — win32-input mode included, which a killed process would leave on
// for the shell — and report their sentinel, and the deferred Close the run
// loop unwinds through must not write the leave a second time.
func TestConsoleControlEventRestoresAndReports(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  os.Signal
		want error
	}{
		{"interrupt", os.Interrupt, ErrInterrupt},
		{"close", syscall.SIGTERM, errConsoleClose},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, output := newSuspendBackend(t)
			backend.win32 = true
			backend.events = makeLifecycleChan()
			signals := make(chan os.Signal, 1)
			returned, cancel := dispatchLifecycleFor(backend, signals)
			defer cancel()

			signals <- tc.sig
			awaitReturn(t, returned)

			if !backend.suspended {
				t.Fatalf("%s did not restore the console: suspend latch not set", tc.name)
			}
			if written := writtenBytes(t, output); bytes.Count(written, []byte(win32Leave)) != 1 {
				t.Fatalf("%s wrote %q, want the win32 leave once", tc.name, written)
			}
			assertLifecycleEvent(t, backend, tc.want)

			if err := backend.Close(); err != nil {
				t.Fatalf("close after %s restore: %v", tc.name, err)
			}
			if written := writtenBytes(t, output); bytes.Count(written, []byte(win32Leave)) != 1 {
				t.Fatalf("Close wrote a second leave on top of the %s restore: %q", tc.name, written)
			}
		})
	}
}
