package utils

import (
	"testing"
)

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		name      string
		modPath   string
		wantBase  string
		wantMajor int
	}{
		{"Standard v2", "github.com/user/gomodule/v2", "github.com/user/gomodule", 2},
		{"Standard v3", "github.com/user/gomodule/v3", "github.com/user/gomodule", 3},
		{"Gopkg.in v2", "gopkg.in/yaml.v2", "gopkg.in/yaml", 2},
		{"Gopkg.in v3", "gopkg.in/yaml.v3", "gopkg.in/yaml", 3},
		{"Unversioned", "github.com/google/uuid", "github.com/google/uuid", 1},
		{"Invalid v", "github.com/foo/bar/v", "github.com/foo/bar/v", 1},
		{"Invalid v1", "github.com/foo/bar/v1", "github.com/foo/bar/v1", 1},
		{"Double digit v", "github.com/foo/bar/v10", "github.com/foo/bar", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotMajor := ParseModulePath(tt.modPath)
			if gotBase != tt.wantBase {
				t.Errorf("ParseModulePath() gotBase = %v, want %v", gotBase, tt.wantBase)
			}
			if gotMajor != tt.wantMajor {
				t.Errorf("ParseModulePath() gotMajor = %v, want %v", gotMajor, tt.wantMajor)
			}
		})
	}
}

func TestNextMajorPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		major    int
		want     string
	}{
		{"Standard", "github.com/user/gomodule", 3, "github.com/user/gomodule/v3"},
		{"Gopkg.in", "gopkg.in/yaml", 3, "gopkg.in/yaml.v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextMajorPath(tt.basePath, tt.major); got != tt.want {
				t.Errorf("NextMajorPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapePath(t *testing.T) {
	tests := []struct {
		name    string
		modPath string
		want    string
		wantErr bool
	}{
		{"No uppercase", "github.com/google/uuid", "github.com/google/uuid", false},
		{"With uppercase", "github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml", false},
		{"Empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EscapePath(tt.modPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("EscapePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EscapePath() = %v, want %v", got, tt.want)
			}
		})
	}
}
