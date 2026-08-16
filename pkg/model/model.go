// Package model defines core domain models and DTOs for gomajor.
package model

import (
	"github.com/chimanjain/gomajor/pkg/source"
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
	BasePath           string `yaml:"-" json:"-"`
}

// SourceResult holds checking results grouped by source.
type SourceResult struct {
	Source       string           `yaml:"source" json:"source"`
	SourceType   source.Type      `yaml:"source_type" json:"source_type"`
	Dependencies []DependencyInfo `yaml:"dependencies" json:"dependencies"`
}
