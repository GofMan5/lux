// Real pty plumbing: the harness talks to every framework through a pty
// pair exactly like a terminal would, so input parsing, resizes
// (SIGWINCH), and output writes all traverse the real kernel paths.
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// burstGap is the read silence that separates two wire bursts. A painted
// frame is one write (a few reads at most, back to back), and consecutive
// frames are separated by the tick interval, so 4ms is a safe cut.
const burstGap = 4 * time.Millisecond

// ptySession owns a real pty pair. The child process is a session leader
// (the parent spawns it with Setsid), so making the slave the controlling
// terminal delivers real SIGWINCH on the framework's input path.
type ptySession struct {
	master *os.File
	slave  *os.File
}

func openPTY(w, h int) (*ptySession, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("pty.Open: %w", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
		master.Close()
		slave.Close()
		return nil, fmt.Errorf("pty.Setsize: %w", err)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, slave.Fd(), unix.TIOCSCTTY, 0); errno != 0 {
		master.Close()
		slave.Close()
		return nil, fmt.Errorf("TIOCSCTTY: %s", errno)
	}
	return &ptySession{master: master, slave: slave}, nil
}

func (p *ptySession) write(b []byte) {
	_, _ = p.master.Write(b)
}

func (p *ptySession) resize(w, h int) error {
	return pty.Setsize(p.master, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
}

func (p *ptySession) close() {
	_ = p.slave.Close()
	_ = p.master.Close()
}

// masterReader drains the pty master: every byte a framework writes to the
// slave lands here, which is the wire truth for all lanes. Reads separated
// by more than burstGap start a new burst — approximately one painted frame.
//
// When delay > 0 the reader sleeps before the first read of each burst,
// emulating a slow link: the framework's writes still succeed immediately
// into the kernel buffer, but the data is consumed at link speed.
type masterReader struct {
	m     *os.File
	delay time.Duration

	active       atomic.Bool
	bytesTotal   atomic.Int64
	bursts       atomic.Int64
	lastReadNano atomic.Int64
	lastSize     atomic.Int64

	mu          sync.Mutex
	burstStarts []int64 // unix nano of the first read of each burst

	done chan struct{}
}

func newMasterReader(m *os.File, delay time.Duration) *masterReader {
	return &masterReader{m: m, delay: delay, done: make(chan struct{})}
}

// start launches the drain goroutine. It exits when the master is closed.
func (r *masterReader) start() {
	go func() {
		defer close(r.done)
		buf := make([]byte, 64*1024)
		for {
			if r.delay > 0 && time.Since(r.lastReadTime()) > 10*time.Millisecond {
				time.Sleep(r.delay)
			}
			n, err := r.m.Read(buf)
			if n > 0 {
				r.record(n)
			}
			if err != nil {
				// EIO/EOF is the normal end once the slave closes.
				return
			}
		}
	}()
}

func (r *masterReader) record(n int) {
	now := time.Now().UnixNano()
	prev := r.lastReadNano.Swap(now)
	r.lastSize.Store(int64(n))
	if !r.active.Load() {
		return
	}
	r.bytesTotal.Add(int64(n))
	if prev == 0 || now-prev >= int64(burstGap) {
		r.bursts.Add(1)
		r.mu.Lock()
		r.burstStarts = append(r.burstStarts, now)
		r.mu.Unlock()
	}
}

// reset zeroes every counter and begins counting the measured phase.
func (r *masterReader) reset() {
	r.mu.Lock()
	r.burstStarts = nil
	r.mu.Unlock()
	r.bytesTotal.Store(0)
	r.bursts.Store(0)
	r.lastSize.Store(0)
	r.active.Store(true)
}

func (r *masterReader) lastReadTime() time.Time {
	nano := r.lastReadNano.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (r *masterReader) stats() (bytes, bursts int64) {
	return r.bytesTotal.Load(), r.bursts.Load()
}

// waitQuiet blocks until no master bytes arrived for quiet, or timeout.
// Returns false on timeout, which callers record as a note.
func (r *masterReader) waitQuiet(quiet, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		last := r.lastReadTime()
		if !last.IsZero() && time.Since(last) >= quiet {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// frameGaps returns millisecond gaps between consecutive burst starts.
func (r *masterReader) frameGaps() []float64 {
	r.mu.Lock()
	starts := r.burstStarts
	r.mu.Unlock()
	gaps := make([]float64, 0, len(starts))
	for i := 1; i < len(starts); i++ {
		gaps = append(gaps, float64(starts[i]-starts[i-1])/1e6)
	}
	return gaps
}

func (r *masterReader) waitDrained() {
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
	}
}

// usageSnapshot captures process CPU (user+system over all threads) and
// cumulative Go heap allocation. Deltas attribute a phase honestly because
// each measured run is its own process.
type usageSnapshot struct {
	cpuMicros int64
	alloc     uint64
	mallocs   uint64
}

func snapshotUsage() usageSnapshot {
	var ru unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &ru)
	user := ru.Utime.Sec*1_000_000 + int64(ru.Utime.Usec)
	sysTime := ru.Stime.Sec*1_000_000 + int64(ru.Stime.Usec)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return usageSnapshot{
		cpuMicros: user + sysTime,
		alloc:     ms.TotalAlloc,
		mallocs:   ms.Mallocs,
	}
}
