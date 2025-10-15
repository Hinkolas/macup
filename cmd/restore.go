package cmd

import (
	"fmt"
	"os"

	"github.com/hinkolas/macup/internal/backup"
	"github.com/spf13/cobra"
)

func init() {

	// Restore-Command Flags
	restoreCmd.Flags().BoolP("debug", "d", false, "Enable debug mode")
	restoreCmd.Flags().StringP("backup", "b", "", "Path to the backup directory (required)")

	// Mark backup flag as required
	restoreCmd.MarkFlagRequired("backup")

	rootCmd.AddCommand(restoreCmd)

}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a backup from the specified directory",
	Long: `Restore a backup from a directory containing the backup archives and config.yaml.
The restore command will read the config.yaml from the backup directory and extract
each archive to its original location as specified in the config.`,
	Run: func(cmd *cobra.Command, args []string) {

		backupDir := cmd.Flag("backup").Value.String()

		// Check if backup directory exists
		if _, err := os.Stat(backupDir); os.IsNotExist(err) {
			fmt.Printf("Backup directory not found: %s\n", backupDir)
			os.Exit(1)
		}

		// Restore the backup
		err := backup.Restore(backupDir)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	},
}
