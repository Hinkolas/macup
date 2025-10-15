package cmd

import (
	"fmt"
	"os"

	"github.com/hinkolas/macup/internal/backup"
	"github.com/spf13/cobra"
)

func init() {

	// Create-Command Flags
	createCmd.Flags().BoolP("debug", "d", false, "Enable debug mode")
	createCmd.Flags().StringP("config", "c", "~/.config/macup/config.yaml", "Specify the path to the config file")
	createCmd.Flags().StringP("output", "o", "./backup", "Output path of the backup")

	rootCmd.AddCommand(createCmd)

}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new backup with the specified configuration",
	Run: func(cmd *cobra.Command, args []string) {

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

		if cmd.Flag("output").Changed {
			config.Output = cmd.Flag("output").Value.String()
		}

		// Create a new backup with the specified configuration
		configPath := cmd.Flag("config").Value.String()
		err = backup.Create(config, configPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	},
}
