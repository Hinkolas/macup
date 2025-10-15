package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/hinkolas/macup/internal/tui"
)

// BackupData creates compressed tar archives for all configured locations
func BackupData(config *Config) error {
	// Create progress view
	pv := tui.NewProgressView()

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
	for _, loc := range config.Data.Locations {
		if err := backupLocation(loc, config.Output, pv); err != nil {
			pv.Clear() // Clear on error
			return fmt.Errorf("failed to backup %s: %w", loc.Path, err)
		}
	}

	// Show final state with success message
	successMsg := fmt.Sprintf("✓ Backup successfully stored at %s", config.Output)
	pv.Finish(successMsg)

	return nil
}

// backupLocation creates a backup archive for a single location
func backupLocation(loc Location, outputDir string, pv *tui.ProgressView) error {
	// Generate filename hash from ORIGINAL config path (before normalization)
	// This ensures the hash is consistent regardless of which user restores
	filename := generateFilename(loc.Path)
	archivePath := filepath.Join(outputDir, filename)

	// Normalize path for actual file operations
	path, err := normalizePath(loc.Path)
	if err != nil {
		return err
	}
	loc.Path = path

	// Scan directory
	if err := loc.scan(pv); err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Create archive

	writer, err := newArchiveWriter(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer writer.Close()

	// Write files
	if err := loc.writeToArchive(writer, pv); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	// Clear message and mark as done
	pv.Message("")
	pv.Done(loc.Path, true)

	return nil
}

// scan walks through the location directory and builds an index of files to backup
func (l *Location) scan(pv *tui.ProgressView) error {
	l.index = make([]string, 0)
	l.totalSize = 0

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
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			l.index = append(l.index, path)

			// Calculate total size for progress tracking
			if !d.IsDir() {
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
func (l *Location) writeToArchive(w *ArchiveWriter, pv *tui.ProgressView) error {
	var bytesWritten int64
	startTime := time.Now()

	for i, path := range l.index {
		// Update message every 50 files to reduce flicker
		if i%50 == 0 {
			if err := l.writeEntry(w, path, pv); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		} else {
			if err := l.writeEntryNoMessage(w, path); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		}

		// Update progress
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			bytesWritten += info.Size()
		}

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

// writeEntry writes a single file or directory entry to the archive with message update
func (l *Location) writeEntry(w *ArchiveWriter, path string, pv *tui.ProgressView) error {
	// Update current file in progress view
	pv.Message(path)
	return l.writeEntryNoMessage(w, path)
}

// writeEntryNoMessage writes a single file or directory entry to the archive without updating the message
func (l *Location) writeEntryNoMessage(w *ArchiveWriter, path string) error {
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

// copyFileToArchive copies a file's contents to the archive
func copyFileToArchive(w io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	return err
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
	defer dst.Close()

	// Copy contents
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy config: %w", err)
	}

	return nil
}
