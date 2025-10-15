package backup

import (
	"fmt"
	"os"
)

// Create creates a backup of all configured locations
func Create(config *Config, configPath string) error {
	// Create output directory
	err := os.MkdirAll(config.Output, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Copy config file to backup directory
	if err := copyConfigToBackup(configPath, config.Output); err != nil {
		return fmt.Errorf("failed to copy config: %w", err)
	}

	// Backup all data locations
	if err := BackupData(config); err != nil {
		return err
	}

	return nil
}
