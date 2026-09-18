package backup

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/hinkolas/macup/internal/tui"
)

// newTestTracker returns a tracker without its reporting goroutine, so tests
// can drive report with their own clock
func newTestTracker(t *testing.T, totalBytes, totalItems int64) (*progressTracker, time.Time) {
	t.Helper()
	pv := tui.NewProgressView("Testing")
	t.Cleanup(func() { pv.Finish("") })
	start := time.Unix(0, 0)
	return &progressTracker{
		pv:         pv,
		location:   "loc",
		totalBytes: totalBytes,
		totalItems: totalItems,
		start:      start,
	}, start
}

func TestProgressAveragesBytesAndItems(t *testing.T) {
	tr, _ := newTestTracker(t, 100, 10)
	tr.add(50)
	if got := tr.progress(); got != 0.25 {
		t.Fatalf("progress = %v, want 0.25", got)
	}
	tr.addItems(10)
	if got := tr.progress(); got != 0.75 {
		t.Fatalf("progress = %v, want 0.75", got)
	}

	// Without a byte total, only entries count
	tr, _ = newTestTracker(t, 0, 4)
	tr.addItems(1)
	if got := tr.progress(); got != 0.25 {
		t.Fatalf("progress = %v, want 0.25", got)
	}
}

func TestETAUsesRecentRate(t *testing.T) {
	tr, start := newTestTracker(t, 1000, 0)
	at := func(d time.Duration) time.Time { return start.Add(d) }

	// No estimate during warm-up
	tr.add(10)
	tr.report(at(time.Second))
	if tr.eta != 0 {
		t.Fatalf("eta during warm-up = %v, want 0", tr.eta)
	}

	// A slow first minute (1 unit/s) followed by a fast phase (10 units/s):
	// the estimate follows the current rate, not the average since start
	for s := 2; s <= 60; s++ {
		tr.add(1)
		tr.report(at(time.Duration(s) * time.Second))
	}
	for s := 61; s <= 100; s++ {
		tr.add(10)
		tr.report(at(time.Duration(s) * time.Second))
	}
	// 70 + 400 done, 530 left at 10 units/s
	if want := 53 * time.Second; tr.eta < want-time.Second || tr.eta > want+time.Second {
		t.Fatalf("eta = %v, want about %v", tr.eta, want)
	}

	// A stall raises the estimate as the recent rate drops, and once nothing
	// moved for the whole window the last estimate is kept
	var before time.Duration
	for s := 101; s <= 130; s++ {
		if s == 125 {
			before = tr.eta
		}
		tr.report(at(time.Duration(s) * time.Second))
	}
	if before <= 53*time.Second || tr.eta != before {
		t.Fatalf("eta after stall = %v (at 125s: %v), want it raised and then held", tr.eta, before)
	}
}

func TestQueuedWriter(t *testing.T) {
	var buf bytes.Buffer
	q := newQueuedWriter(&buf, 2)
	p := []byte("ab")
	for range 100 {
		if _, err := q.Write(p); err != nil {
			t.Fatal(err)
		}
		p[0], p[1] = p[1], p[0] // Callers may reuse the buffer
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if want := bytes.Repeat([]byte("abba"), 50); !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("wrote %q, want %q", buf.Bytes(), want)
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestQueuedWriterError(t *testing.T) {
	errDisk := errors.New("disk full")
	q := newQueuedWriter(failingWriter{errDisk}, 1)

	// Writes never block after a failure, and the error surfaces
	var err error
	for range 100 {
		if _, err = q.Write([]byte("x")); err != nil {
			break
		}
	}
	if err := q.Close(); !errors.Is(err, errDisk) {
		t.Fatalf("Close = %v, want %v", err, errDisk)
	}
	if _, err := q.Write([]byte("x")); !errors.Is(err, errDisk) {
		t.Fatalf("Write after failure = %v, want %v", err, errDisk)
	}
}
