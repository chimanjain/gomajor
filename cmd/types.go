package cmd

import (
	"github.com/chimanjain/gomajor/checker"
)

// Config holds the configuration for the checker command.
type Config struct {
	ModFilePath string
	MaxProbe    int
	CheckAll    bool
	JsonOutput  bool
	NoColor     bool
	Minor       bool
	Major       bool
	Client      *checker.Client
	ConfigPath  string
	OutputPath  string
	GithubRepos []string
}

// YAMLConfig defines the structure for the configuration YAML file.
type YAMLConfig struct {
	Local  []string `yaml:"local"`
	Github []string `yaml:"github"`
	Output string   `yaml:"output"`
	Minor  *bool    `yaml:"minor,omitempty"`
	Major  *bool    `yaml:"major,omitempty"`
}

// DependencyInfo holds information about a specific checked dependency.
type DependencyInfo struct {
	Module             string `yaml:"module"`
	CurrentVersion     string `yaml:"current_version"`
	LatestMajorVersion string `yaml:"latest_major_version"`
	LatestMajorPath    string `yaml:"latest_major_path"`
	HasUpdate          bool   `yaml:"has_update"`
	LatestMinorVersion string `yaml:"latest_minor_version,omitempty"`
	HasMinorUpdate     bool   `yaml:"has_minor_update,omitempty"`
}

// SourceResult holds checking results grouped by source.
type SourceResult struct {
	Source       string           `yaml:"source"`
	SourceType   string           `yaml:"source_type"`
	Dependencies []DependencyInfo `yaml:"dependencies"`
}

// YAMLOutput defines the structured schema for the saved YAML output.
type YAMLOutput struct {
	Results []SourceResult `yaml:"results"`
}
