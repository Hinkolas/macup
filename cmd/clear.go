package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hinkolas/macup/internal/backup"
	"github.com/spf13/cobra"
)

func init() {
	// Clear-Command Flags
	clearCmd.Flags().StringP("config", "c", "~/.config/macup/config.yaml", "Specify the path to the config file")
	clearCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")

	rootCmd.AddCommand(clearCmd)
}

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete all configured backup locations (for testing restore)",
	Long: `Delete all directories specified in the config file.
This is primarily intended for testing the restore functionality.

WARNING: This will permanently delete all files and directories listed in your config!
You will be asked to confirm before deletion unless --yes flag is used.`,
	Run: func(cmd *cobra.Command, args []string) {
		configPath := cmd.Flag("config").Value.String()
		skipConfirmation := cmd.Flag("yes").Changed && cmd.Flag("yes").Value.String() == "true"

		// Load config
		config, err := backup.LoadConfig(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Can't find a config file at %s\n", configPath)
			} else if os.IsPermission(err) {
				fmt.Println("Can't access config file due to missing permissions.")
			} else {
				fmt.Println(err)
			}
			os.Exit(1)
		}

		// Show what will be deleted
		fmt.Println("\n⚠️  WARNING: The following locations will be PERMANENTLY DELETED:")
		fmt.Println()
		for _, loc := range config.Data.Locations {
			fmt.Printf("  - %s\n", loc.Path)
		}
		fmt.Println()

		// Confirm deletion
		if !skipConfirmation {
			confirmed, err := confirmDeletion()
			if err != nil {
				fmt.Printf("Error reading confirmation: %v\n", err)
				os.Exit(1)
			}
			if !confirmed {
				fmt.Println("Deletion cancelled.")
				os.Exit(0)
			}
		}

		// Perform deletion
		err = backup.ClearLocations(config)
		if err != nil {
			fmt.Printf("Error during deletion: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("\n✓ All locations cleared successfully!")
	},
}

func confirmDeletion() (bool, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Type 'DELETE' to confirm (case-sensitive): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	input = strings.TrimSpace(input)
	return input == "DELETE", nil
}
