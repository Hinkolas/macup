package backup

import (
	"fmt"
	"path/filepath"
)

// Restore restores a backup from the specified backup directory
func Restore(backupDir string) error {
	// Load config from backup directory
	configPath := filepath.Join(backupDir, "config.yaml")
	config, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config from backup: %w", err)
	}

	// Restore each location
	for _, loc := range config.Data.Locations {
		if err := restoreLocation(loc, backupDir); err != nil {
			return fmt.Errorf("failed to restore %s: %w", loc.Path, err)
		}
	}

	return nil
}
