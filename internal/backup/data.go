package backup

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/klauspost/pgzip"
)

// Data contains backup configuration for multiple locations
type Data struct {
	Locations []Location `yaml:"location"`
}

// Location represents a directory to backup with ignore patterns
type Location struct {
	Path   string   `yaml:"path"`
	Ignore []string `yaml:"ignore"`
	index  []string // Paths to include in backup
}

// ArchiveWriter wraps tar.Writer with compression
type ArchiveWriter struct {
	tar  *tar.Writer
	gzip *pgzip.Writer
	file *os.File
}

// BackupData creates compressed tar archives for all configured locations
func BackupData(config *Config) error {
	log.SetPrefix("[DATA] ")
	defer log.SetPrefix("")

	for _, loc := range config.Data.Locations {
		if err := backupLocation(loc, config.Output); err != nil {
			return fmt.Errorf("failed to backup %s: %w", loc.Path, err)
		}
	}

	log.Println("Backup completed successfully!")
	return nil
}

func backupLocation(loc Location, outputDir string) error {
	// Normalize path
	path, err := normalizePath(loc.Path)
	if err != nil {
		return err
	}
	loc.Path = path

	// Scan directory
	if err := loc.scan(); err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Create archive
	filename := generateFilename(loc.Path)
	archivePath := filepath.Join(outputDir, filename)

	writer, err := newArchiveWriter(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer writer.Close()

	// Write files
	if err := loc.writeToArchive(writer); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	return nil
}

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

func generateFilename(path string) string {
	h := sha256.New()
	h.Write([]byte(path))
	return fmt.Sprintf(
		"%s-%x.tar.gz",
		filepath.Base(path),
		h.Sum(nil),
	)
}

func (l *Location) scan() error {
	log.Printf("Scanning %s", l.Path)

	l.index = make([]string, 0)

	err := filepath.WalkDir(
		l.Path,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip root directory
			if path == l.Path {
				return nil
			}

			// Check ignore patterns
			if slices.Contains(l.Ignore, d.Name()) {
				log.Printf("Skipping %s", d.Name())
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			l.index = append(l.index, path)
			return nil
		},
	)

	if err != nil {
		return fmt.Errorf("directory walk failed: %w", err)
	}

	log.Printf("Found %d files/directories", len(l.index))
	return nil
}

func (l *Location) writeToArchive(w *ArchiveWriter) error {
	for _, path := range l.index {
		if err := l.writeEntry(w, path); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}
	return nil
}

func (l *Location) writeEntry(w *ArchiveWriter, path string) error {
	// Get current file info
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Calculate relative path
	relPath, err := filepath.Rel(l.Path, path)
	if err != nil {
		return err
	}

	// Create tar header
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	// Prepend original directory name so extraction creates proper folder structure
	hdr.Name = filepath.Join(filepath.Base(l.Path), relPath)
	hdr.Format = tar.FormatPAX

	log.Printf("Writing %s", relPath)

	// Write header
	if err := w.WriteHeader(hdr); err != nil {
		return err
	}

	// Write file content
	if !info.IsDir() {
		if err := copyFileToArchive(w, path); err != nil {
			return err
		}
	}

	return nil
}

func copyFileToArchive(w io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	return err
}

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

func (w *ArchiveWriter) Close() error {
	return errors.Join(
		w.tar.Close(),
		w.gzip.Close(),
		w.file.Close(),
	)
}

func (w *ArchiveWriter) WriteHeader(hdr *tar.Header) error {
	return w.tar.WriteHeader(hdr)
}

func (w *ArchiveWriter) Write(p []byte) (int, error) {
	return w.tar.Write(p)
}
