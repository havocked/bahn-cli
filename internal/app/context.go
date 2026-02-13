package app

import (
	"github.com/havocked/bahn-cli/internal/config"
	"github.com/havocked/bahn-cli/internal/output"
)

// Settings holds parsed CLI flags.
type Settings struct {
	ConfigPath string
	Format     output.Format
	Quiet      bool
	Verbose    bool
	APIKey     string
}

// Context holds runtime state shared across commands.
type Context struct {
	Settings   Settings
	Config     *config.Config
	ConfigPath string
	Output     *output.Writer
}

// NewContext creates a Context from settings.
func NewContext(settings Settings) (*Context, error) {
	configPath := settings.ConfigPath
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath, _ = config.DefaultPath()
	}

	w := output.New(output.Options{
		Format: settings.Format,
		Quiet:  settings.Quiet,
	})

	return &Context{
		Settings:   settings,
		Config:     cfg,
		ConfigPath: configPath,
		Output:     w,
	}, nil
}
