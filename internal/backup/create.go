package backup

import (
	"log"
	"os"
)

type Backup struct {
	Path      string     `yaml:"path"`
	Locations []Location `yaml:"data"`
}

func Create(config *Config) error {

	log.Println("Starting backup creation...")

	err := os.MkdirAll(config.Output, 0755)
	if err != nil {
		return err
	}

	err = BackupData(config)
	if err != nil {
		return err
	}

	log.Println("Backup created successfully!")

	return nil
}
