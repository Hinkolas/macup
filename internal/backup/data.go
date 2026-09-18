package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/klauspost/pgzip"
)

const (
	// Files up to this size are read into memory so several can be read or
	// written concurrently; larger files are streamed
	smallFileLimit = 1 << 20
	// Number of goroutines reading files during backup or writing them during
	// restore. More workers mostly add file system lock contention on APFS.
	ioWorkers = 4
	// Compressed chunks queued for the archive file. Destinations such as
	// USB drives accept writes in bursts and then stall while flushing; the
	// queue lets compression continue through a stall instead of waiting.
	archiveWriteQueue = 64
)

// Data contains backup configuration for multiple locations
type Data struct {
	Locations []Location `mapstructure:"locations"`
}

// Location represents a directory to backup with ignore patterns
type Location struct {
	Path      string       `mapstructure:"path"`
	Ignore    []string     `mapstructure:"ignore"`
	index     []indexEntry // Entries to include in backup
	totalSize int64        // Total size of files to backup
	warnings  []string     // Files skipped during scan or write
}

// indexEntry is a path found during the scan with its (non-followed) file info
type indexEntry struct {
	path string
	info os.FileInfo
}

// ArchiveWriter wraps tar.Writer with compression. The archive is written to
// a temporary file and only moved to its final path by a successful Close.
type ArchiveWriter struct {
	tar   *tar.Writer
	gzip  *pgzip.Writer
	queue *queuedWriter
	file  *os.File
	path  string
}

// normalizePath expands home directory and converts to absolute path
func normalizePath(path string) (string, error) {
	// Expand home directory
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home dir: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// generateFilename creates a unique filename based on the path
func generateFilename(path string) string {
	h := sha256.New()
	h.Write([]byte(path))
	return fmt.Sprintf(
		"%s-%x.tar.gz",
		filepath.Base(path),
		h.Sum(nil),
	)
}

// newArchiveWriter creates a new compressed tar archive writer
func newArchiveWriter(path string) (*ArchiveWriter, error) {
	file, err := os.Create(path + ".tmp")
	if err != nil {
		return nil, err
	}

	queue := newQueuedWriter(file, archiveWriteQueue)
	gzipWriter, err := pgzip.NewWriterLevel(
		queue,
		pgzip.DefaultCompression,
	)
	if err != nil {
		queue.Close()
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}

	// 1MB blocks, use all CPU cores
	gzipWriter.SetConcurrency(1<<20, runtime.NumCPU())

	return &ArchiveWriter{
		tar:   tar.NewWriter(gzipWriter),
		gzip:  gzipWriter,
		queue: queue,
		file:  file,
		path:  path,
	}, nil
}

// Close flushes and closes all underlying writers, then moves the archive to
// its final path. On any error the temporary file is removed.
func (w *ArchiveWriter) Close() error {
	err := errors.Join(
		w.tar.Close(),
		w.gzip.Close(),
		w.queue.Close(),
		w.file.Close(),
	)
	if err == nil {
		err = os.Rename(w.file.Name(), w.path)
	}
	if err != nil {
		os.Remove(w.file.Name())
	}
	return err
}

// Abort closes the archive and removes the temporary file without keeping it
func (w *ArchiveWriter) Abort() {
	w.tar.Close()
	w.gzip.Close()
	w.queue.Close()
	w.file.Close()
	os.Remove(w.file.Name())
}

// WriteHeader writes a tar header to the archive
func (w *ArchiveWriter) WriteHeader(hdr *tar.Header) error {
	return w.tar.WriteHeader(hdr)
}

// Write writes data to the archive
func (w *ArchiveWriter) Write(p []byte) (int, error) {
	return w.tar.Write(p)
}

// queuedWriter writes to w from a background goroutine, holding up to size
// writes in a queue. A write error is returned by a later Write or by Close.
type queuedWriter struct {
	w      io.Writer
	queue  chan []byte
	done   chan struct{}
	mu     sync.Mutex
	err    error
	closed bool
}

func newQueuedWriter(w io.Writer, size int) *queuedWriter {
	q := &queuedWriter{
		w:     w,
		queue: make(chan []byte, size),
		done:  make(chan struct{}),
	}

	go func() {
		defer close(q.done)
		// Keep draining after an error so Write never blocks on a full queue
		for p := range q.queue {
			if q.error() != nil {
				continue
			}
			if _, err := q.w.Write(p); err != nil {
				q.mu.Lock()
				q.err = err
				q.mu.Unlock()
			}
		}
	}()

	return q
}

func (q *queuedWriter) error() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

// Write queues a copy of p, since callers may reuse p once Write returns
func (q *queuedWriter) Write(p []byte) (int, error) {
	if err := q.error(); err != nil {
		return 0, err
	}
	q.queue <- append([]byte(nil), p...)
	return len(p), nil
}

// Close waits until all queued writes are done and returns the first error.
// It is safe to call more than once.
func (q *queuedWriter) Close() error {
	if !q.closed {
		q.closed = true
		close(q.queue)
	}
	<-q.done
	return q.error()
}

// ctxReader makes reads fail once ctx is cancelled, so copying a single large
// file can be interrupted
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// zeroReader yields an endless stream of zero bytes
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
