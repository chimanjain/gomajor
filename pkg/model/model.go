// Package model defines core domain models and DTOs for gomajor.
package model

// SourceType identifies where a go.mod was loaded from.
type SourceType string

const (
	// Local indicates a go.mod loaded from the local filesystem.
	Local SourceType = "local"
	// GitHub indicates a go.mod fetched from a remote GitHub repository.
	GitHub SourceType = "github"
)

// CalculationMetadata holds internal computation state used during version discovery
// that is excluded from external reports and serialization.
type CalculationMetadata struct {
	Current      string `yaml:"-" json:"-"`
	BasePath     string `yaml:"-" json:"-"`
	Separator    string `yaml:"-" json:"-"`
	CurrentMajor int    `yaml:"-" json:"-"`
	LatestMajor  int    `yaml:"-" json:"-"`
}

// DependencyInfo holds information about a specific checked dependency.
type DependencyInfo struct {
	Module              string `yaml:"module" json:"module"`
	CurrentVersion      string `yaml:"current_version" json:"current_version"`
	LatestMajorVersion  string `yaml:"latest_major_version" json:"latest_major_version"`
	LatestMajorPath     string `yaml:"latest_major_path" json:"latest_major_path"`
	HasUpdate           bool   `yaml:"has_update" json:"has_update"`
	LatestMinorVersion  string `yaml:"latest_minor_version,omitempty" json:"latest_minor_version,omitempty"`
	HasMinorUpdate      bool   `yaml:"has_minor_update,omitempty" json:"has_minor_update,omitempty"`
	CalculationMetadata `yaml:"-" json:"-"`
}

// SourceResult holds checking results grouped by source.
type SourceResult struct {
	Source       string           `yaml:"source" json:"source"`
	SourceType   SourceType       `yaml:"source_type" json:"source_type"`
	Dependencies []DependencyInfo `yaml:"dependencies" json:"dependencies"`
}
