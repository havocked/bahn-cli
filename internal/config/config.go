package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const DefaultConfigFile = "config.toml"

type Config struct {
	API    APIConfig    `toml:"api"`
	Output OutputConfig `toml:"output"`
	Watch  WatchConfig  `toml:"watch"`
}

type APIConfig struct {
	RISKey         string `toml:"ris_key"`
	DefaultStation string `toml:"default_station"`
}

type OutputConfig struct {
	Format string `toml:"format"`
}

type WatchConfig struct {
	ThresholdMinutes int `toml:"threshold_minutes"`
	CheckBeforeHours int `toml:"check_before_hours"`
}

// DefaultPath returns ~/.config/bahn-cli/config.toml
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bahn-cli", DefaultConfigFile), nil
}

// ConfigDir returns ~/.config/bahn-cli/
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bahn-cli"), nil
}

// Load reads config from path, or defaults if not found.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, err
	}
	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes config to path.
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{
		API: APIConfig{
			DefaultStation: "Leipzig Hbf",
		},
		Output: OutputConfig{
			Format: "json",
		},
		Watch: WatchConfig{
			ThresholdMinutes: 5,
			CheckBeforeHours: 4,
		},
	}
}
