package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hinkolas/macup/internal/backup"
	"github.com/spf13/cobra"
)

func init() {

	// Env-Command Flags
	envCmd.Flags().StringP("config", "c", "~/.config/macup/config.yaml", "Specify the path to the config file")
	envCmd.Flags().StringSliceP("path", "p", nil, "Search these directories instead of the configured locations")
	envCmd.Flags().StringP("output", "o", "./env-backup", "Output directory for the copied files")
	envCmd.Flags().StringSlice("pattern", backup.DefaultEnvPatterns, "File name patterns to copy")

	rootCmd.AddCommand(envCmd)

}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Copy all .env files into a folder without compression",
	Long: `Search the configured locations (or the directories given with --path) for
.env files and copy them as plain files into the output directory, so single
files can be picked up later without restoring a whole backup.

Copies keep their path relative to your home directory and are only readable
by you, since they usually contain secrets.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		paths, _ := cmd.Flags().GetStringSlice("path")
		patterns, _ := cmd.Flags().GetStringSlice("pattern")
		output := cmd.Flag("output").Value.String()

		var locations []backup.Location
		if len(paths) > 0 {
			for _, p := range paths {
				locations = append(locations, backup.Location{Path: p})
			}
		} else {
			config, err := backup.LoadConfig(cmd.Flag("config").Value.String())
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("Can't find a config file at", cmd.Flag("config").Value.String())
				} else if os.IsPermission(err) {
					fmt.Println("Can't access config file due to missing permissions.")
				} else {
					fmt.Println(err)
				}
				os.Exit(1)
			}
			locations = config.Data.Locations
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		err := backup.CopyEnvFiles(ctx, locations, output, patterns)
		exitOnError(err)

	},
}
