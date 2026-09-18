package backup

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/hinkolas/macup/internal/tui"
)

// Restore restores a backup from the specified backup directory
func Restore(ctx context.Context, backupDir string) error {
	// Load config from backup directory
	configPath := filepath.Join(backupDir, "config.yaml")
	config, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config from backup: %w", err)
	}

	// Create progress view with "Extracting" prefix
	pv := tui.NewProgressView("Extracting")

	// Initialize all locations in progress view
	for _, loc := range config.Data.Locations {
		// Normalize path for display
		displayPath := loc.Path
		if normalized, err := normalizePath(loc.Path); err == nil {
			displayPath = normalized
		}
		pv.Add(displayPath, 0.0, 0)
	}

	// Restore each location
	for _, loc := range config.Data.Locations {
		if err := restoreLocation(ctx, loc, backupDir, pv); err != nil {
			pv.Clear() // Clear on error
			return fmt.Errorf("failed to restore %s: %w", loc.Path, err)
		}
	}

	// Show final state with success message
	pv.Finish("✓ Restore completed successfully!")

	return nil
}
