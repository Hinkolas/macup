package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/pgzip"
)

// restoreLocation restores a single location from its archive
func restoreLocation(loc Location, backupDir string) error {
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

	// Extract the archive
	if err := extractArchive(archivePath, targetPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	return nil
}

// extractArchive extracts a tar.gz archive to the target directory
func extractArchive(archivePath, targetPath string) error {
	// Open the archive file
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	// Create gzip reader
	gzipReader, err := pgzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzipReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzipReader)

	// Get the parent directory where we'll extract
	parentDir := filepath.Dir(targetPath)

	// Extract all files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

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

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(extractPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", extractPath, err)
			}

		case tar.TypeReg:
			// Create parent directories if they don't exist
			if err := os.MkdirAll(filepath.Dir(extractPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create and write file
			if err := extractFile(tarReader, extractPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", extractPath, err)
			}

		case tar.TypeSymlink:
			// Create parent directories if they don't exist
			if err := os.MkdirAll(filepath.Dir(extractPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create symlink
			if err := os.Symlink(header.Linkname, extractPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", extractPath, err)
			}
		}
	}

	return nil
}

// extractFile extracts a single file from the tar reader
func extractFile(tarReader io.Reader, path string, mode os.FileMode) error {
	// Create the file
	outFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Copy content
	if _, err := io.Copy(outFile, tarReader); err != nil {
		return err
	}

	return nil
}
