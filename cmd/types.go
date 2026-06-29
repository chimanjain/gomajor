package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/chimanjain/gomajor/checker"
	"go.yaml.in/yaml/v3"
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
	Module             string `yaml:"module" json:"module"`
	CurrentVersion     string `yaml:"current_version" json:"current_version"`
	LatestMajorVersion string `yaml:"latest_major_version" json:"latest_major_version"`
	LatestMajorPath    string `yaml:"latest_major_path" json:"latest_major_path"`
	HasUpdate          bool   `yaml:"has_update" json:"has_update"`
	LatestMinorVersion string `yaml:"latest_minor_version,omitempty" json:"latest_minor_version,omitempty"`
	HasMinorUpdate     bool   `yaml:"has_minor_update,omitempty" json:"has_minor_update,omitempty"`
}

// SourceResult holds checking results grouped by source.
type SourceResult struct {
	Source       string           `yaml:"source" json:"source"`
	SourceType   string           `yaml:"source_type" json:"source_type"`
	Dependencies []DependencyInfo `yaml:"dependencies" json:"dependencies"`
}

// YAMLOutput defines the structured schema for the saved YAML output.
type YAMLOutput struct {
	Results []SourceResult `yaml:"results" json:"results"`
}

func toDependencyInfos(results []checker.ModuleInfo) []DependencyInfo {
	depInfos := make([]DependencyInfo, len(results))
	for i, info := range results {
		depInfos[i] = DependencyInfo{
			Module:             info.Current,
			CurrentVersion:     info.CurrentVersion,
			LatestMajorVersion: info.LatestMajorVersion,
			LatestMajorPath:    info.LatestMajorPath,
			HasUpdate:          info.HasUpdate,
			LatestMinorVersion: info.LatestMinorVersion,
			HasMinorUpdate:     info.HasMinorUpdate,
		}
	}
	return depInfos
}

func writeReport(outputPath string, isJSON bool, results []SourceResult) error {
	outputData := YAMLOutput{Results: results}
	var data []byte
	var err error
	formatName := "YAML"

	if isJSON {
		formatName = "JSON"
		data, err = json.MarshalIndent(outputData, "", "  ")
	} else {
		data, err = yaml.Marshal(outputData)
	}

	if err == nil {
		err = os.WriteFile(outputPath, data, 0o644)
	}
	if err != nil {
		return fmt.Errorf("failed to write %s output: %w", formatName, err)
	}
	fmt.Printf("Results written to %s file: %s\n", formatName, outputPath)
	return nil
}
