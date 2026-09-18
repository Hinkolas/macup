package backup

import (
	"errors"
	"fmt"

	"github.com/spf13/viper"
)

// Config is decoded by viper, which uses mapstructure tags (not yaml tags).
type Config struct {
	Output string `mapstructure:"output"`
	Data   Data   `mapstructure:"data"`
}

func LoadConfig(path string) (*Config, error) {

	// Expand "~" ourselves: the flag default never passes through a shell
	path, err := normalizePath(path)
	if err != nil {
		return nil, err
	}

	v := viper.NewWithOptions(viper.KeyDelimiter("|"))
	v.SetConfigType("yaml")
	v.SetConfigFile(path)

	v.SetDefault("output", "./backup")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	// Unmarshal the config into backup config
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// Fail loudly on a misspelled key instead of silently backing up nothing
	if len(cfg.Data.Locations) == 0 {
		return nil, errors.New("no locations configured under data.locations")
	}

	return &cfg, nil

}
