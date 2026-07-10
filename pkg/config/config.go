package config

import (
	"io"
	"log/slog"
	"os"

	"github.com/chimanjain/gomajor/checker"
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
	Client      *checker.Client
	ConfigPath  string
	OutputPath  string
	GitHubRepos []string
	Out         io.Writer
	Err         io.Writer
	Logger      *slog.Logger
	OnProgress  func(completed, total int)
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
func DefaultConfig() *Config {
	return &Config{
		ModFilePath: "",
		MaxProbe:    constants.DefaultMaxProbe,
		CheckAll:    false,
		JSONOutput:  false,
		NoColor:     false,
		Minor:       true,
		Major:       true,
		Client:      checker.DefaultClient(),
		ConfigPath:  "",
		OutputPath:  "",
		GitHubRepos: nil,
		Out:         os.Stdout,
		Err:         os.Stderr,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}
