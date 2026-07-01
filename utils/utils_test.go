package utils

import (
	"testing"
)

const (
	testUserGoModule   = "github.com/user/gomodule"
	testUserGoModuleV3 = "github.com/user/gomodule/v3"
	testYamlGopkg      = "gopkg.in/yaml"
	testGoogleUUID     = "github.com/google/uuid"
)

func TestUtils(t *testing.T) {
	t.Run("ParseModulePath", func(t *testing.T) {
		tests := []struct {
			name      string
			modPath   string
			wantBase  string
			wantMajor int
			wantSep   string
		}{
			{"Standard v2", "github.com/user/gomodule/v2", testUserGoModule, 2, "/"},
			{"Standard v3", testUserGoModuleV3, testUserGoModule, 3, "/"},
			{"Gopkg.in v2", "gopkg.in/yaml.v2", testYamlGopkg, 2, "."},
			{"Gopkg.in v3", "gopkg.in/yaml.v3", testYamlGopkg, 3, "."},
			{"Gopkg.in v1", "gopkg.in/yaml.v1", testYamlGopkg, 1, "."},
			{"Gopkg.in v0", "gopkg.in/yaml.v0", testYamlGopkg, 0, "."},
			{"Unversioned", testGoogleUUID, testGoogleUUID, 1, "/"},
			{"Invalid v", "github.com/foo/bar/v", "github.com/foo/bar/v", 1, "/"},
			{"Invalid v1", "github.com/foo/bar/v1", "github.com/foo/bar/v1", 1, "/"},
			{"Double digit v", "github.com/foo/bar/v10", "github.com/foo/bar", 10, "/"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotBase, gotMajor, gotSep := ParseModulePath(tt.modPath)
				if gotBase != tt.wantBase {
					t.Errorf("ParseModulePath() gotBase = %v, want %v", gotBase, tt.wantBase)
				}
				if gotMajor != tt.wantMajor {
					t.Errorf("ParseModulePath() gotMajor = %v, want %v", gotMajor, tt.wantMajor)
				}
				if gotSep != tt.wantSep {
					t.Errorf("ParseModulePath() gotSep = %v, want %v", gotSep, tt.wantSep)
				}
			})
		}
	})

	t.Run("NextMajorPath", func(t *testing.T) {
		tests := []struct {
			name     string
			basePath string
			major    int
			sep      string
			want     string
		}{
			{"Standard", testUserGoModule, 3, "/", testUserGoModuleV3},
			{"Gopkg.in", testYamlGopkg, 3, ".", "gopkg.in/yaml.v3"},
			{"DefaultSep", testUserGoModule, 3, "", testUserGoModuleV3},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := NextMajorPath(tt.basePath, tt.major, tt.sep); got != tt.want {
					t.Errorf("NextMajorPath() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("EscapePath", func(t *testing.T) {
		tests := []struct {
			name    string
			modPath string
			want    string
			wantErr bool
		}{
			{"No uppercase", testGoogleUUID, testGoogleUUID, false},
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
	})
}
