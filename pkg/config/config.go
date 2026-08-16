package config

import (
	"github.com/chimanjain/gomajor/pkg/constants"
)

// Config holds the configuration options for the checker command.
type Config struct {
	ModFilePath string
	MaxProbe    int
	CheckAll    bool
	JSONOutput  bool
	NoColor     bool
	Minor       bool
	Major       bool
	Verbose     bool
	ConfigPath  string
	OutputPath  string
	GitHubRepos []string
}

// YAMLConfig defines the structure for the configuration YAML file.
type YAMLConfig struct {
	Local  []string `yaml:"local" mapstructure:"local"`
	Github []string `yaml:"github" mapstructure:"github"`
	Output string   `yaml:"output" mapstructure:"output"`
	Minor  *bool    `yaml:"minor,omitempty" mapstructure:"minor"`
	Major  *bool    `yaml:"major,omitempty" mapstructure:"major"`
}

// DefaultConfig returns a config with standard default settings.
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
	}
}
