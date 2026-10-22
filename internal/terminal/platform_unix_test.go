//go:build !windows

package terminal

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// An idle read must block in poll and still return the moment its context
// is cancelled. The old loop re-polled every 100 ms just to look at ctx, so
// a cancel landed up to 100 ms late; five prompt returns in a row rule that
// cadence out with better than 99% confidence.
func TestReadInputWakesOnCancelWithoutPolling(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	for round := 0; round < 5; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		waker := newReadWaker(ctx)
		if waker == nil {
			t.Fatal("newReadWaker returned nil")
		}
		returned := make(chan error, 1)
		go func() {
			_, err := readInput(ctx, waker, reader, make([]byte, 16), -1)
			returned <- err
		}()
		time.Sleep(130 * time.Millisecond)
		start := time.Now()
		cancel()
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("round %d: err = %v, want context.Canceled", round, err)
			}
			if late := time.Since(start); late > 30*time.Millisecond {
				t.Fatalf("round %d: read returned %v after cancel", round, late)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("round %d: read did not return after cancel", round)
		}
		waker.close()
	}
}

func TestReadInputWithWakerStillReadsAndTimesOut(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waker := newReadWaker(ctx)
	defer waker.close()

	if _, err := readInput(ctx, waker, reader, make([]byte, 16), 20*time.Millisecond); !errors.Is(err, errReadTimeout) {
		t.Fatalf("empty read with timeout: err = %v, want errReadTimeout", err)
	}
	if _, err := writer.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 16)
	count, err := readInput(ctx, waker, reader, buffer, -1)
	if err != nil || string(buffer[:count]) != "q" {
		t.Fatalf("read = %q, %v; want \"q\", nil", buffer[:count], err)
	}
}

// close may race the cancellation callback; under -race this must be clean
// and must not write into a descriptor number that was already released.
func TestReadWakerCloseRacesCancel(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		waker := newReadWaker(ctx)
		go cancel()
		waker.close()
		cancel()
	}
}
