package backup

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

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

// scan walks through the location directory and builds an index of files to
// backup. The file info from the walk is kept so each file is only stat'ed once.
func (l *Location) scan(pv *tui.ProgressView) error {
	l.index = make([]indexEntry, 0)
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

			// Lstat the entry (DirEntry.Info does not follow symlinks)
			info, err := d.Info()
			if err != nil {
				l.warnings = append(l.warnings, err.Error())
				return nil
			}

			l.index = append(l.index, indexEntry{path: path, info: info})

			// Calculate total size for progress tracking
			if info.Mode().IsRegular() {
				l.totalSize += info.Size()
			}

			return nil
		},
	)

	if err != nil {
		return fmt.Errorf("directory walk failed: %w", err)
	}

	return nil
}

// preparedEntry is an index entry made ready for writing by a reader worker
type preparedEntry struct {
	indexEntry
	link    string   // Symlink target
	data    []byte   // Content of small regular files
	file    *os.File // Open handle for large regular files, streamed by the writer
	skip    bool     // Entry is left out of the archive
	warning string
}

// prefetch prepares index entries on a pool of workers so opening and reading
// small files overlaps with compression. Results are delivered in index order;
// each channel read from the returned channel yields exactly one entry.
func (l *Location) prefetch(ctx context.Context) <-chan chan preparedEntry {
	type job struct {
		entry indexEntry
		out   chan preparedEntry
	}

	// The window bounds memory to roughly window * smallFileLimit
	const window = 64
	ordered := make(chan chan preparedEntry, window)
	jobs := make(chan job, window)

	go func() {
		defer close(ordered)
		defer close(jobs)
		for _, entry := range l.index {
			out := make(chan preparedEntry, 1)
			select {
			case ordered <- out:
			case <-ctx.Done():
				return
			}
			jobs <- job{entry, out}
		}
	}()

	for range ioWorkers {
		go func() {
			for j := range jobs {
				j.out <- prepareEntry(ctx, j.entry)
			}
		}()
	}

	return ordered
}

// prepareEntry reads what the writer needs for one entry. Entries that can't
// be read or aren't supported are marked as skipped with a warning.
func prepareEntry(ctx context.Context, e indexEntry) preparedEntry {
	p := preparedEntry{indexEntry: e}
	if ctx.Err() != nil {
		p.skip = true
		return p
	}

	switch mode := e.info.Mode(); {
	case mode.IsDir():
	case mode&os.ModeSymlink != 0:
		link, err := os.Readlink(e.path)
		if err != nil {
			p.skip, p.warning = true, err.Error()
		}
		p.link = link
	case mode.IsRegular():
		file, err := os.Open(e.path)
		if err != nil {
			p.skip, p.warning = true, err.Error()
			return p
		}
		if e.info.Size() > smallFileLimit {
			p.file = file
			return p
		}
		defer file.Close()

		// Read exactly the size recorded during the scan. If the file shrank
		// the rest stays zero, which keeps the archive valid.
		p.data = make([]byte, e.info.Size())
		if n, err := io.ReadFull(file, p.data); err != nil {
			p.warning = fmt.Sprintf("%s: changed during backup, archived copy is incomplete (read %d of %d bytes)", e.path, n, len(p.data))
		}
	default:
		p.skip = true
		p.warning = fmt.Sprintf("%s: unsupported file type (socket, named pipe or device)", e.path)
	}

	return p
}

// writeToArchive writes all indexed files to the archive
func (l *Location) writeToArchive(ctx context.Context, w *ArchiveWriter, pv *tui.ProgressView) error {
	ctx, cancel := context.WithCancel(ctx)
	ordered := l.prefetch(ctx)

	// On early return, stop the prefetcher and close files it already opened
	defer func() {
		cancel()
		for out := range ordered {
			if p := <-out; p.file != nil {
				p.file.Close()
			}
		}
	}()

	// Track content bytes as they are written, so large files show progress
	// while they are copied. Locations with only empty files and directories
	// count entries instead.
	byEntries := l.totalSize == 0
	total := l.totalSize
	if byEntries {
		total = int64(len(l.index))
	}
	tracker := startProgress(pv, l.Path, total)
	defer tracker.finish()

	for out := range ordered {
		p := <-out
		if err := ctx.Err(); err != nil {
			if p.file != nil {
				p.file.Close()
			}
			return err
		}

		pv.Message(p.path)

		if p.warning != "" {
			l.warnings = append(l.warnings, p.warning)
		}

		switch {
		case byEntries:
			tracker.add(1)
		case p.skip && p.info.Mode().IsRegular():
			// Count skipped files as done so progress still reaches the total
			tracker.add(p.info.Size())
		}

		if !p.skip {
			if err := l.writeEntry(ctx, w, p, tracker); err != nil {
				return fmt.Errorf("failed to write %s: %w", p.path, err)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Final update to ensure we show 100%
	tracker.finish()
	pv.Set(l.Path, 1.0, 0)

	return nil
}

// writeEntry writes a prepared file, directory or symlink entry to the archive
func (l *Location) writeEntry(ctx context.Context, w *ArchiveWriter, p preparedEntry, tracker *progressTracker) error {
	if p.file != nil {
		defer p.file.Close()
	}

	// Calculate relative path
	relPath, err := filepath.Rel(l.Path, p.path)
	if err != nil {
		return err
	}

	// Create tar header
	hdr, err := tar.FileInfoHeader(p.info, p.link)
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

	// Count content bytes for progress; the tracker ignores them when
	// counting entries
	cw := io.Writer(w)
	if l.totalSize > 0 {
		cw = countingWriter{w, tracker}
	}

	if p.data != nil {
		_, err := cw.Write(p.data)
		return err
	}
	if p.file == nil {
		return nil
	}

	// Stream large files, copying exactly the size recorded in the header;
	// growth is truncated
	n, err := io.CopyN(cw, ctxReader{ctx, p.file}, hdr.Size)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The file shrank or failed mid-read; pad so the archive stays valid
		if _, padErr := io.CopyN(cw, zeroReader{}, hdr.Size-n); padErr != nil {
			return padErr
		}
		l.warnings = append(l.warnings, fmt.Sprintf("%s: changed during backup, archived copy is incomplete (%v)", p.path, err))
	}

	return nil
}

// copyConfigToBackup copies the config file to the backup directory
func copyConfigToBackup(configPath, outputDir string) error {
	configPath, err := normalizePath(configPath)
	if err != nil {
		return err
	}

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
