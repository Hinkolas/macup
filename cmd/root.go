package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Version: fmt.Sprintf("%s, %s/%s", "0.0.1", runtime.GOOS, runtime.GOARCH),
	Use:     "macup",
	Short:   "Backup and restore your macOS setup with one command.",
	Long: `A Go-powered CLI to back up and restore your macOS setup.
	Define folders, excludes, apps, dev tools, and system tweaks in a single
	YAML config to then recreate a clean, personalized Mac in minutes.`,
}

// Execute adds all child commands to the root command and sets flags.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

}
