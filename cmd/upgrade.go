package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hinkolas/macup/internal/update"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

func init() {
	// Upgrade-Command Flags
	upgradeCmd.Flags().Bool("check", false, "Only check whether a newer version is available")
	upgradeCmd.Flags().Bool("force", false, "Install the latest release even if it is not newer")

	rootCmd.AddCommand(upgradeCmd)
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade macup to the latest release",
	Long: `Download the latest macup release from GitHub, verify its checksum and
replace the currently installed binary with it.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		checkOnly, _ := cmd.Flags().GetBool("check")
		force, _ := cmd.Flags().GetBool("force")

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		latest, err := update.Latest(ctx)
		exitOnError(err)

		// Release tags carry a "v" prefix while the injected version does not
		current := version
		if version != "dev" {
			current = "v" + version
		}
		upToDate := version != "dev" && semver.Compare(current, latest) >= 0

		if checkOnly {
			if upToDate {
				fmt.Printf("macup is up to date (%s)\n", current)
			} else {
				fmt.Printf("A new version is available: %s -> %s\n", current, latest)
				fmt.Println("Run 'macup upgrade' to install it.")
			}
			return
		}

		if !force {
			if version == "dev" {
				fmt.Println("This is a development build; use --force to replace it with the latest release.")
				os.Exit(1)
			}
			if upToDate {
				fmt.Printf("macup is up to date (%s)\n", current)
				return
			}
		}

		fmt.Printf("Upgrading macup %s -> %s...\n", current, latest)
		exitOnError(update.Apply(ctx, latest))
		fmt.Printf("✓ macup upgraded to %s\n", latest)
	},
}
