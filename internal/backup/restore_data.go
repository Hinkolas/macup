package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hinkolas/macup/internal/tui"
	"github.com/klauspost/pgzip"
)

// restoreLocation restores a single location from its archive
func restoreLocation(ctx context.Context, loc Location, backupDir string, pv *tui.ProgressView) error {
	// Generate the archive filename based on the ORIGINAL config path (before normalization)
	// This must match the hash used during backup creation
	archiveName := generateFilename(loc.Path)
	archivePath := filepath.Join(backupDir, archiveName)

	// Normalize the target path for actual file operations
	targetPath, err := normalizePath(loc.Path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}

	// Check if archive exists
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return fmt.Errorf("archive not found: %s", archivePath)
	}

	// Extract the archive with progress tracking
	if err := extractArchive(ctx, archivePath, targetPath, pv); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Mark as done
	pv.Message("")
	pv.Done(targetPath, true)

	return nil
}

// extractArchive extracts a tar.gz archive to the target directory with progress tracking
func extractArchive(ctx context.Context, archivePath, targetPath string, pv *tui.ProgressView) error {
	// Open the archive file
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	// Get archive size for progress tracking
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get archive info: %w", err)
	}
	archiveSize := fileInfo.Size()

	// Create gzip reader. pgzip otherwise reads the archive through a 4KB
	// buffer, which makes reading it syscall-bound.
	gzipReader, err := pgzip.NewReader(bufio.NewReaderSize(file, 1<<20))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzipReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzipReader)

	// Get the parent directory where we'll extract
	parentDir := filepath.Dir(targetPath)

	// Small files are written by a pool of workers, since creating many
	// small files one after another is bound by open/close latency
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fw := newFileWriterPool(ctx, cancel)
	defer fw.wait()

	// Directories already created, to avoid a MkdirAll per file
	dirs := make(map[string]struct{})
	ensureDir := func(dir string, mode os.FileMode) error {
		if _, ok := dirs[dir]; ok {
			return nil
		}
		if err := os.MkdirAll(dir, mode); err != nil {
			return err
		}
		dirs[dir] = struct{}{}
		return nil
	}

	// Track progress
	var bytesProcessed int64
	startTime := time.Now()
	fileCount := 0

	// Extract all files
	for {
		if err := ctx.Err(); err != nil {
			return fw.errOr(err)
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		fileCount++
		bytesProcessed += header.Size

		// Construct the full path for extraction
		// The archive contains paths like "foldername/subfolder/file.txt"
		// We want to extract to "parentDir/foldername/subfolder/file.txt"
		extractPath := filepath.Join(parentDir, header.Name)

		// Security check: ensure the path doesn't escape the target directory
		cleanPath := filepath.Clean(extractPath)
		cleanParent := filepath.Clean(parentDir)
		if !strings.HasPrefix(cleanPath, cleanParent+string(filepath.Separator)) &&
			cleanPath != cleanParent {
			return fmt.Errorf("illegal file path in archive: %s", header.Name)
		}

		// Update progress every 50 files
		if fileCount%50 == 0 {
			pv.Message(extractPath)

			// Calculate progress and ETA
			progress := float64(bytesProcessed) / float64(archiveSize)
			if progress > 1.0 {
				progress = 1.0
			}

			elapsed := time.Since(startTime)
			var eta time.Duration
			if progress > 0 && progress < 1.0 {
				totalTime := time.Duration(float64(elapsed) / progress)
				eta = totalTime - elapsed
				if eta < 0 {
					eta = 0
				}
			}

			pv.Set(targetPath, progress, eta)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := ensureDir(extractPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", extractPath, err)
			}

		case tar.TypeReg:
			// Create parent directories if they don't exist
			if err := ensureDir(filepath.Dir(extractPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			job := fileJob{
				path:    extractPath,
				mode:    os.FileMode(header.Mode),
				modTime: header.ModTime,
			}

			// Stream large files directly; buffer small ones for the workers
			if header.Size > smallFileLimit {
				if err := job.write(ctxReader{ctx, tarReader}); err != nil {
					return fmt.Errorf("failed to extract file %s: %w", extractPath, err)
				}
				continue
			}

			job.data = make([]byte, header.Size)
			if _, err := io.ReadFull(tarReader, job.data); err != nil {
				return fmt.Errorf("failed to read %s from archive: %w", header.Name, err)
			}
			if !fw.submit(job) {
				return fw.errOr(ctx.Err())
			}

		case tar.TypeSymlink:
			// Create parent directories if they don't exist
			if err := ensureDir(filepath.Dir(extractPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Replace any existing entry so restoring over an existing tree works
			if err := os.Remove(extractPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to replace %s: %w", extractPath, err)
			}

			// Create symlink
			if err := os.Symlink(header.Linkname, extractPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", extractPath, err)
			}
		}
	}

	// Wait for all queued files to be written
	if err := fw.wait(); err != nil {
		return err
	}

	// Final progress update
	pv.Set(targetPath, 1.0, 0)

	return nil
}

// fileJob is a regular file to be written during restore
type fileJob struct {
	path    string
	mode    os.FileMode
	modTime time.Time
	data    []byte
}

// write creates the file with content from r and restores its modification time
func (j fileJob) write(r io.Reader) error {
	outFile, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, j.mode)
	if err != nil {
		return err
	}

	// Copy content
	if _, err := io.Copy(outFile, r); err != nil {
		outFile.Close()
		return err
	}

	if err := outFile.Close(); err != nil {
		return err
	}

	return os.Chtimes(j.path, j.modTime, j.modTime)
}

// fileWriterPool writes buffered files concurrently. The first error cancels
// the restore.
type fileWriterPool struct {
	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan fileJob
	wg     sync.WaitGroup
	once   sync.Once
	mu     sync.Mutex
	err    error
}

func newFileWriterPool(ctx context.Context, cancel context.CancelFunc) *fileWriterPool {
	p := &fileWriterPool{
		ctx:    ctx,
		cancel: cancel,
		jobs:   make(chan fileJob, 2*ioWorkers),
	}
	for range ioWorkers {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				if err := job.write(bytes.NewReader(job.data)); err != nil {
					p.fail(fmt.Errorf("failed to extract file %s: %w", job.path, err))
				}
			}
		}()
	}
	return p
}

// submit queues a job and reports false if the restore was cancelled
func (p *fileWriterPool) submit(job fileJob) bool {
	select {
	case p.jobs <- job:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *fileWriterPool) fail(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.mu.Unlock()
	p.cancel()
}

// errOr returns the first worker error, or fallback if there was none
func (p *fileWriterPool) errOr(fallback error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	return fallback
}

// wait stops accepting jobs, waits for queued files and returns the first
// worker error. It is safe to call more than once.
func (p *fileWriterPool) wait() error {
	p.once.Do(func() { close(p.jobs) })
	p.wg.Wait()
	return p.errOr(nil)
}
