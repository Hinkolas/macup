package backup

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/hinkolas/macup/internal/tui"
)

const (
	// progressInterval matches the progress view's redraw rate
	progressInterval = 100 * time.Millisecond
	// etaWindow is how far back the ETA looks to measure the current rate.
	// Throughput swings between directories of tiny files and large files
	// and when the destination stalls to flush, so the average since the
	// start overreacts early on and lags behind later.
	etaWindow = 20 * time.Second
	// etaWarmup is how long to measure before showing an ETA at all
	etaWarmup = 3 * time.Second
)

// progressTracker reports progress for one location. Work is counted in
// bytes and, where the number of entries is known up front, in entries.
// Progress is the average of both, since time depends on both: a directory
// of tiny files takes long but moves few bytes. The add methods are safe to
// call from any goroutine and cheap enough to call for every chunk of data;
// a background goroutine periodically turns the counts into a progress
// fraction and ETA.
type progressTracker struct {
	pv         *tui.ProgressView
	location   string
	totalBytes int64
	totalItems int64
	doneBytes  atomic.Int64
	doneItems  atomic.Int64
	start      time.Time
	stop       chan struct{}
	stopped    chan struct{}

	// Owned by the reporting goroutine
	samples []progressSample
	eta     time.Duration
}

// progressSample is the progress at one point in time
type progressSample struct {
	at       time.Time
	progress float64
}

// startProgress starts reporting progress for location out of totalBytes
// bytes and totalItems entries. A total of zero leaves that measure out.
func startProgress(pv *tui.ProgressView, location string, totalBytes, totalItems int64) *progressTracker {
	t := &progressTracker{
		pv:         pv,
		location:   location,
		totalBytes: totalBytes,
		totalItems: totalItems,
		start:      time.Now(),
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}

	go func() {
		defer close(t.stopped)
		ticker := time.NewTicker(progressInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				t.report(now)
			case <-t.stop:
				return
			}
		}
	}()

	return t
}

// add counts n bytes of completed work
func (t *progressTracker) add(n int64) {
	t.doneBytes.Add(n)
}

// addItems counts n completed entries
func (t *progressTracker) addItems(n int64) {
	t.doneItems.Add(n)
}

// progress returns the completed fraction of the work
func (t *progressTracker) progress() float64 {
	var sum float64
	var measures int
	if t.totalBytes > 0 {
		sum += float64(t.doneBytes.Load()) / float64(t.totalBytes)
		measures++
	}
	if t.totalItems > 0 {
		sum += float64(t.doneItems.Load()) / float64(t.totalItems)
		measures++
	}
	if measures == 0 {
		return 0
	}
	return sum / float64(measures)
}

// report sends the current progress and ETA to the progress view. Progress
// is kept just below 1.0 because the view treats 1.0 as done, which only the
// caller can decide.
func (t *progressTracker) report(now time.Time) {
	progress := min(t.progress(), 0.999)

	// Keep the samples within the window, plus the newest one before it so
	// the rate always spans the full window
	t.samples = append(t.samples, progressSample{now, progress})
	for len(t.samples) > 2 && now.Sub(t.samples[1].at) >= etaWindow {
		t.samples = t.samples[1:]
	}

	// Estimate from the rate over the window. While nothing moves, keep the
	// last estimate rather than jumping to infinity.
	oldest := t.samples[0]
	if now.Sub(t.start) >= etaWarmup && progress > oldest.progress {
		rate := (progress - oldest.progress) / now.Sub(oldest.at).Seconds()
		t.eta = time.Duration((1 - progress) / rate * float64(time.Second))
	}

	t.pv.Set(t.location, progress, t.eta)
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
