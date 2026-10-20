package backup

import (
	"context"
	"fmt"
	"os"
)

// Create creates a backup of all configured locations
func Create(ctx context.Context, config *Config, configPath string) error {
	// Expand "~" ourselves: the output from the config never passes through a shell
	output, err := normalizePath(config.Output)
	if err != nil {
		return err
	}
	config.Output = output

	// Create output directory
	err = os.MkdirAll(config.Output, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Copy config file to backup directory
	if err := copyConfigToBackup(configPath, config.Output); err != nil {
		return fmt.Errorf("failed to copy config: %w", err)
	}

	// Backup all data locations
	if err := BackupData(ctx, config); err != nil {
		return err
	}

	return nil
}
