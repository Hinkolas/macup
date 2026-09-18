package backup

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/hinkolas/macup/internal/tui"
)

// BackupData creates compressed tar archives for all configured locations
func BackupData(ctx context.Context, config *Config) error {
	// Create progress view with "Archiving" prefix
	pv := tui.NewProgressView("Archiving")

	// Initialize all locations in progress view
	for _, loc := range config.Data.Locations {
		// Normalize path for display
		displayPath := loc.Path
		if normalized, err := normalizePath(loc.Path); err == nil {
			displayPath = normalized
		}
		pv.Add(displayPath, 0.0, 0)
	}

	// Backup each location
	var warnings []string
	for _, loc := range config.Data.Locations {
		locWarnings, err := backupLocation(ctx, loc, config.Output, pv)
		if err != nil {
			pv.Clear() // Clear on error
			return fmt.Errorf("failed to backup %s: %w", loc.Path, err)
		}
		warnings = append(warnings, locWarnings...)
	}

	// Show final state with success message
	successMsg := fmt.Sprintf("✓ Backup successfully stored at %s", config.Output)
	pv.Finish(successMsg)

	// List skipped files below the progress view so rendering isn't disturbed
	if len(warnings) > 0 {
		fmt.Printf("\n⚠ %d file(s) skipped or changed during backup:\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	return nil
}

// backupLocation creates a backup archive for a single location and returns
// warnings for files that were skipped or changed while being archived
func backupLocation(ctx context.Context, loc Location, outputDir string, pv *tui.ProgressView) ([]string, error) {
	// Generate filename hash from ORIGINAL config path (before normalization)
	// This ensures the hash is consistent regardless of which user restores
	filename := generateFilename(loc.Path)
	archivePath := filepath.Join(outputDir, filename)

	// Normalize path for actual file operations
	path, err := normalizePath(loc.Path)
	if err != nil {
		return nil, err
	}
	loc.Path = path

	// Scan directory
	if err := loc.scan(pv); err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Create archive
	writer, err := newArchiveWriter(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	// Write files
	if err := loc.writeToArchive(ctx, writer, pv); err != nil {
		writer.Abort()
		return nil, fmt.Errorf("write failed: %w", err)
	}

	// Flush and move the archive into place; a failure here means it is incomplete
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize archive: %w", err)
	}

	// Clear message and mark as done
	pv.Message("")
	pv.Done(loc.Path, true)

	return loc.warnings, nil
}

// scan walks through the location directory and builds an index of files to backup
func (l *Location) scan(pv *tui.ProgressView) error {
	l.index = make([]string, 0)
	l.totalSize = 0
	l.warnings = nil

	err := filepath.WalkDir(
		l.Path,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// A missing or unreadable location root is a real error
				if path == l.Path {
					return err
				}
				// Skip unreadable entries below the root
				l.warnings = append(l.warnings, err.Error())
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip root directory
			if path == l.Path {
				return nil
			}

			// Check ignore patterns
			if slices.Contains(l.Ignore, d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			l.index = append(l.index, path)

			// Calculate total size for progress tracking
			if d.Type().IsRegular() {
				if info, err := d.Info(); err == nil {
					l.totalSize += info.Size()
				}
			}

			return nil
		},
	)

	if err != nil {
		return fmt.Errorf("directory walk failed: %w", err)
	}

	return nil
}

// writeToArchive writes all indexed files to the archive
func (l *Location) writeToArchive(ctx context.Context, w *ArchiveWriter, pv *tui.ProgressView) error {
	var bytesWritten int64
	startTime := time.Now()

	for i, path := range l.index {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Update message every 50 files to reduce flicker
		if i%50 == 0 {
			pv.Message(path)
		}

		size, err := l.writeEntry(ctx, w, path)
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}

		// Update progress
		bytesWritten += size

		// Calculate progress (handle edge case of empty directories)
		var progress float64
		if l.totalSize > 0 {
			progress = float64(bytesWritten) / float64(l.totalSize)
			if progress > 1.0 {
				progress = 1.0
			}
		} else {
			// For empty directories, use file count
			progress = float64(i+1) / float64(len(l.index))
		}

		// Calculate ETA
		elapsed := time.Since(startTime)
		var eta time.Duration
		if progress > 0 && progress < 1.0 {
			totalTime := time.Duration(float64(elapsed) / progress)
			eta = totalTime - elapsed
			if eta < 0 {
				eta = 0
			}
		}

		// Update progress view (the view itself will decide if it needs to re-render)
		pv.Set(l.Path, progress, eta)
	}

	// Final update to ensure we show 100%
	pv.Set(l.Path, 1.0, 0)

	return nil
}

// writeEntry writes a single file, directory or symlink entry to the archive
// and returns the number of content bytes written. Entries that can't be read
// or aren't supported are skipped with a warning instead of failing the backup.
func (l *Location) writeEntry(ctx context.Context, w *ArchiveWriter, path string) (int64, error) {
	// Lstat so symlinks are archived as links rather than followed
	info, err := os.Lstat(path)
	if err != nil {
		l.warnings = append(l.warnings, err.Error())
		return 0, nil
	}

	var link string
	switch mode := info.Mode(); {
	case mode.IsRegular(), mode.IsDir():
	case mode&os.ModeSymlink != 0:
		if link, err = os.Readlink(path); err != nil {
			l.warnings = append(l.warnings, err.Error())
			return 0, nil
		}
	default:
		l.warnings = append(l.warnings, fmt.Sprintf("%s: unsupported file type (socket, named pipe or device)", path))
		return 0, nil
	}

	// Open regular files before writing the header so an unreadable file can
	// still be skipped
	var file *os.File
	if info.Mode().IsRegular() {
		if file, err = os.Open(path); err != nil {
			l.warnings = append(l.warnings, err.Error())
			return 0, nil
		}
		defer file.Close()
	}

	// Calculate relative path
	relPath, err := filepath.Rel(l.Path, path)
	if err != nil {
		return 0, err
	}

	// Create tar header
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return 0, err
	}

	// Prepend original directory name so extraction creates proper folder structure
	hdr.Name = filepath.Join(filepath.Base(l.Path), relPath)
	hdr.Format = tar.FormatPAX

	// Write header
	if err := w.WriteHeader(hdr); err != nil {
		return 0, err
	}

	if file == nil {
		return 0, nil
	}

	// Copy exactly the size recorded in the header; growth is truncated
	n, err := io.CopyN(w, ctxReader{ctx, file}, hdr.Size)
	if err != nil {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		// The file shrank or failed mid-read; pad so the archive stays valid
		if _, padErr := io.CopyN(w, zeroReader{}, hdr.Size-n); padErr != nil {
			return n, padErr
		}
		l.warnings = append(l.warnings, fmt.Sprintf("%s: changed during backup, archived copy is incomplete (%v)", path, err))
	}

	return hdr.Size, nil
}

// copyConfigToBackup copies the config file to the backup directory
func copyConfigToBackup(configPath, outputDir string) error {
	// Open source config file
	src, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer src.Close()

	// Create destination file
	destPath := filepath.Join(outputDir, "config.yaml")
	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create config copy: %w", err)
	}

	// Copy contents
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("failed to copy config: %w", err)
	}

	// Close explicitly so a failed flush is reported
	if err := dst.Close(); err != nil {
		return fmt.Errorf("failed to write config copy: %w", err)
	}

	return nil
}
