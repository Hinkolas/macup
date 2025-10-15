package backup

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/klauspost/pgzip"
)

// Data contains backup configuration for multiple locations
type Data struct {
	Locations []Location `yaml:"location"`
}

// Location represents a directory to backup with ignore patterns
type Location struct {
	Path      string   `yaml:"path"`
	Ignore    []string `yaml:"ignore"`
	index     []string // Paths to include in backup
	totalSize int64    // Total size of files to backup
}

// ArchiveWriter wraps tar.Writer with compression
type ArchiveWriter struct {
	tar  *tar.Writer
	gzip *pgzip.Writer
	file *os.File
}

// normalizePath expands home directory and converts to absolute path
func normalizePath(path string) (string, error) {
	// Expand home directory
	if len(path) > 1 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home dir: %w", err)
		}
		path = filepath.Join(home, path[2:])
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
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	gzipWriter, err := pgzip.NewWriterLevel(
		file,
		pgzip.DefaultCompression,
	)
	if err != nil {
		file.Close()
		return nil, err
	}

	// 1MB blocks, use all CPU cores
	gzipWriter.SetConcurrency(1<<20, runtime.NumCPU())

	return &ArchiveWriter{
		tar:  tar.NewWriter(gzipWriter),
		gzip: gzipWriter,
		file: file,
	}, nil
}

// Close closes the archive writer and all underlying writers
func (w *ArchiveWriter) Close() error {
	return errors.Join(
		w.tar.Close(),
		w.gzip.Close(),
		w.file.Close(),
	)
}

// WriteHeader writes a tar header to the archive
func (w *ArchiveWriter) WriteHeader(hdr *tar.Header) error {
	return w.tar.WriteHeader(hdr)
}

// Write writes data to the archive
func (w *ArchiveWriter) Write(p []byte) (int, error) {
	return w.tar.Write(p)
}
