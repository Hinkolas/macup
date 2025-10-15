package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

// ClearLocations deletes all configured backup locations
func ClearLocations(config *Config) error {
	fmt.Println("\nStarting deletion...")

	for i, loc := range config.Data.Locations {
		// Normalize path
		path, err := normalizePath(loc.Path)
		if err != nil {
			return fmt.Errorf("failed to normalize path %s: %w", loc.Path, err)
		}

		fmt.Printf("[%d/%d] Deleting %s... ", i+1, len(config.Data.Locations), path)

		// Check if path exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Println("(already deleted)")
			continue
		}

		// Delete the location
		err = os.RemoveAll(path)
		if err != nil {
			fmt.Printf("ERROR\n")
			return fmt.Errorf("failed to delete %s: %w", path, err)
		}

		fmt.Println("✓")
	}

	return nil
}

// ClearSingleLocation deletes a single location (helper for selective clearing)
func ClearSingleLocation(path string) error {
	// Normalize path
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(normalizedPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", normalizedPath)
	}

	// Safety check: don't allow deleting root or home directory
	homeDir, _ := os.UserHomeDir()
	if normalizedPath == "/" || normalizedPath == homeDir {
		return fmt.Errorf("refusing to delete protected directory: %s", normalizedPath)
	}

	// Get absolute path for verification
	absPath, err := filepath.Abs(normalizedPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Additional safety: check path length (avoid deleting too high up)
	if len(filepath.Clean(absPath)) < 10 {
		return fmt.Errorf("path too short, refusing to delete: %s", absPath)
	}

	// Delete
	return os.RemoveAll(normalizedPath)
}
