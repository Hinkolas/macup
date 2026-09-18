package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClearLocations deletes all configured backup locations. Every location is
// checked before anything is deleted, so one unsafe path aborts the whole run.
func ClearLocations(config *Config) error {
	paths := make([]string, 0, len(config.Data.Locations))
	for _, loc := range config.Data.Locations {
		// Normalize path
		path, err := normalizePath(loc.Path)
		if err != nil {
			return fmt.Errorf("failed to normalize path %s: %w", loc.Path, err)
		}
		if err := checkSafeToDelete(path); err != nil {
			return err
		}
		paths = append(paths, path)
	}

	fmt.Println("\nStarting deletion...")

	for i, path := range paths {
		fmt.Printf("[%d/%d] Deleting %s... ", i+1, len(paths), path)

		// Check if path exists
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			fmt.Println("(already deleted)")
			continue
		}

		// Delete the location
		if err := os.RemoveAll(path); err != nil {
			fmt.Printf("ERROR\n")
			return fmt.Errorf("failed to delete %s: %w", path, err)
		}

		fmt.Println("✓")
	}

	return nil
}

// checkSafeToDelete refuses to delete the filesystem root, top-level
// directories, the home directory, or any directory that contains it
func checkSafeToDelete(path string) error {
	path = filepath.Clean(path)

	// Require at least two components below root (e.g. refuse /Users, /System)
	if strings.Count(strings.Trim(path, string(filepath.Separator)), string(filepath.Separator)) < 1 {
		return fmt.Errorf("refusing to delete top-level directory: %s", path)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}
	home = filepath.Clean(home)

	if path == home || strings.HasPrefix(home, path+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete home directory or one of its parents: %s", path)
	}

	return nil
}
