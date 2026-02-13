package cli

import (
	"github.com/alecthomas/kong"
	"github.com/havocked/bahn-cli/internal/app"
	"github.com/havocked/bahn-cli/internal/output"
)

const Version = "0.1.0"

func New() *CLI {
	return &CLI{}
}

type CLI struct {
	Globals Globals `kong:"embed"`

	Auth AuthCmd `kong:"cmd,help='Authentication and token management.'"`
}

type Globals struct {
	Config  string           `help:"Config file path." env:"BAHN_CONFIG"`
	Human   bool             `help:"Human-readable output." env:"BAHN_HUMAN"`
	Quiet   bool             `short:"q" help:"Suppress stderr diagnostics." env:"BAHN_QUIET"`
	Verbose bool             `short:"v" help:"Extra detail in stderr." env:"BAHN_VERBOSE"`
	APIKey  string           `help:"RIS API key." env:"BAHN_API_KEY"`
	Version kong.VersionFlag `help:"Print version."`
}

func (g Globals) Settings() app.Settings {
	format := output.FormatJSON
	if g.Human {
		format = output.FormatHuman
	}
	return app.Settings{
		ConfigPath: g.Config,
		Format:     format,
		Quiet:      g.Quiet,
		Verbose:    g.Verbose,
		APIKey:     g.APIKey,
	}
}

func VersionVars() map[string]string {
	return map[string]string{
		"version": Version,
	}
}
