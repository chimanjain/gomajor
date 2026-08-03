package config

import (
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/constants"
)

// Config holds the configuration for the checker command.
type Config struct {
	ModFilePath string
	MaxProbe    int
	CheckAll    bool
	JSONOutput  bool
	NoColor     bool
	Minor       bool
	Major       bool
	Verbose     bool
	// Client is the module-version checker. Accepts any implementation of
	// checker.ModChecker, making it easy to inject a test double.
	Client checker.ModChecker
	// GitHubHTTPClient is the HTTP client used to fetch remote go.mod files
	// from GitHub. Kept separate from Client so the checker interface stays
	// focused on version-querying concerns.
	GitHubHTTPClient *http.Client
	ConfigPath       string
	OutputPath       string
	GitHubRepos      []string
	Out              io.Writer
	Err              io.Writer
	Logger           *slog.Logger
	OnProgress       func(completed, total int)
}

// YAMLConfig defines the structure for the configuration YAML file.
type YAMLConfig struct {
	Local  []string `yaml:"local" mapstructure:"local"`
	Github []string `yaml:"github" mapstructure:"github"`
	Output string   `yaml:"output" mapstructure:"output"`
	Minor  *bool    `yaml:"minor,omitempty" mapstructure:"minor"`
	Major  *bool    `yaml:"major,omitempty" mapstructure:"major"`
}

// DefaultConfig returns a config with standard settings.
// Client and GitHubHTTPClient are left nil; the caller is responsible for
// constructing them with the desired options (e.g. minor/major flags) to
// avoid creating a client that is immediately discarded and replaced.
func DefaultConfig() *Config {
	return &Config{
		ModFilePath: "",
		MaxProbe:    constants.DefaultMaxProbe,
		CheckAll:    false,
		JSONOutput:  false,
		NoColor:     false,
		Minor:       true,
		Major:       true,
		Verbose:     false,
		ConfigPath:  "",
		OutputPath:  "",
		GitHubRepos: nil,
		Out:         os.Stdout,
		Err:         os.Stderr,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}
