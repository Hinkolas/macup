package backup

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Output string `yaml:"output"`
	Data   Data   `yaml:"data"`
}

func LoadConfig(path string) (*Config, error) {

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

	return &cfg, nil

}
