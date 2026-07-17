package engine

import (
	"github.com/chimanjain/gomajor/pkg/checker"
)

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

func ToDependencyInfos(results []checker.ModuleInfo) []DependencyInfo {
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
