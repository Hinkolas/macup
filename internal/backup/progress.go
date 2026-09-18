package backup

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/hinkolas/macup/internal/tui"
)

// progressInterval matches the progress view's redraw rate
const progressInterval = 100 * time.Millisecond

// progressTracker reports progress for one location. Work is counted with
// add, which is safe to call from any goroutine and cheap enough to call for
// every chunk of data, and a background goroutine periodically turns the
// count into a progress fraction and ETA.
type progressTracker struct {
	pv       *tui.ProgressView
	location string
	total    int64
	done     atomic.Int64
	start    time.Time
	stop     chan struct{}
	stopped  chan struct{}
}

// startProgress starts reporting progress for location out of total units
func startProgress(pv *tui.ProgressView, location string, total int64) *progressTracker {
	t := &progressTracker{
		pv:       pv,
		location: location,
		total:    total,
		start:    time.Now(),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}

	go func() {
		defer close(t.stopped)
		ticker := time.NewTicker(progressInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.report()
			case <-t.stop:
				return
			}
		}
	}()

	return t
}

// add counts n units of completed work
func (t *progressTracker) add(n int64) {
	t.done.Add(n)
}

// report sends the current progress and ETA to the progress view. Progress
// is kept just below 1.0 because the view treats 1.0 as done, which only the
// caller can decide.
func (t *progressTracker) report() {
	if t.total <= 0 {
		return
	}

	progress := min(float64(t.done.Load())/float64(t.total), 0.999)

	var eta time.Duration
	if progress > 0 {
		elapsed := time.Since(t.start)
		eta = max(time.Duration(float64(elapsed)/progress)-elapsed, 0)
	}

	t.pv.Set(t.location, progress, eta)
}

// finish stops reporting. It is safe to call more than once.
func (t *progressTracker) finish() {
	select {
	case <-t.stop:
	default:
		close(t.stop)
	}
	<-t.stopped
}

// countingWriter counts bytes written through it
type countingWriter struct {
	w io.Writer
	t *progressTracker
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.t.add(int64(n))
	return n, err
}

// countingReader counts bytes read through it
type countingReader struct {
	r io.Reader
	t *progressTracker
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.t.add(int64(n))
	return n, err
}
