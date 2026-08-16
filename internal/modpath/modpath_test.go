package modpath

import "testing"

const (
	testUserGoModule   = "github.com/user/gomodule"
	testUserGoModuleV3 = "github.com/user/gomodule/v3"
	testYamlGopkg      = "gopkg.in/yaml"
	testGoogleUUID     = "github.com/google/uuid"
)

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		name      string
		modPath   string
		wantBase  string
		wantMajor int
		wantSep   string
	}{
		{"Standard v3", testUserGoModuleV3, testUserGoModule, 3, "/"},
		{"Gopkg.in v3", "gopkg.in/yaml.v3", testYamlGopkg, 3, "."},
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
}

func TestNextMajorPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		major    int
		sep      string
		want     string
	}{
		{"Standard", testUserGoModule, 3, "/", testUserGoModuleV3},
		{"Standard v1", testUserGoModule, 1, "/", testUserGoModule},
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
}

func BenchmarkParseModulePath(b *testing.B) {
	paths := []string{
		"github.com/user/gomodule/v2",
		"gopkg.in/yaml.v2",
		"github.com/google/uuid",
		"github.com/foo/bar/v10",
	}

	for b.Loop() {
		for _, p := range paths {
			_, _, _ = ParseModulePath(p)
		}
	}
}
