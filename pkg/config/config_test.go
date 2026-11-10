package config

import (
	"testing"
	"time"

	"go.yaml.in/yaml/v4"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	if cfg.ModFilePath != "" {
		t.Errorf("DefaultConfig().ModFilePath = %q, want empty", cfg.ModFilePath)
	}
	if cfg.MaxProbe != DefaultMaxProbe {
		t.Errorf("DefaultConfig().MaxProbe = %d, want %d", cfg.MaxProbe, DefaultMaxProbe)
	}
	if cfg.CheckAll != false {
		t.Errorf("DefaultConfig().CheckAll = %v, want false", cfg.CheckAll)
	}
	if cfg.JSONOutput != false {
		t.Errorf("DefaultConfig().JSONOutput = %v, want false", cfg.JSONOutput)
	}
	if cfg.NoColor != false {
		t.Errorf("DefaultConfig().NoColor = %v, want false", cfg.NoColor)
	}
	if cfg.Minor != true {
		t.Errorf("DefaultConfig().Minor = %v, want true", cfg.Minor)
	}
	if cfg.Major != true {
		t.Errorf("DefaultConfig().Major = %v, want true", cfg.Major)
	}
	if cfg.Verbose != false {
		t.Errorf("DefaultConfig().Verbose = %v, want false", cfg.Verbose)
	}
	if cfg.ConfigPath != "" {
		t.Errorf("DefaultConfig().ConfigPath = %q, want empty", cfg.ConfigPath)
	}
	if cfg.OutputPath != "" {
		t.Errorf("DefaultConfig().OutputPath = %q, want empty", cfg.OutputPath)
	}
	if cfg.GitHubRepos != nil {
		t.Errorf("DefaultConfig().GitHubRepos = %v, want nil", cfg.GitHubRepos)
	}
}

func TestConstants(t *testing.T) {
	t.Parallel()
	if MB != 1024*1024 {
		t.Errorf("MB = %d, want %d", MB, 1024*1024)
	}
	if ProxyResponseMaxBytes != 10*MB {
		t.Errorf("ProxyResponseMaxBytes = %d, want %d", ProxyResponseMaxBytes, 10*MB)
	}
	if GitHubModMaxBytes != 5*MB {
		t.Errorf("GitHubModMaxBytes = %d, want %d", GitHubModMaxBytes, 5*MB)
	}
	if DefaultMaxProbe != 5 {
		t.Errorf("DefaultMaxProbe = %d, want 5", DefaultMaxProbe)
	}
	if MaxAllowedMaxProbe != 50 {
		t.Errorf("MaxAllowedMaxProbe = %d, want 50", MaxAllowedMaxProbe)
	}
	if EngineConcurrencyLimit != 100 {
		t.Errorf("EngineConcurrencyLimit = %d, want 100", EngineConcurrencyLimit)
	}
	if CheckerConcurrencyLimit != 100 {
		t.Errorf("CheckerConcurrencyLimit = %d, want 100", CheckerConcurrencyLimit)
	}
	if HTTPTimeout != 10*time.Second {
		t.Errorf("HTTPTimeout = %v, want 10s", HTTPTimeout)
	}
	if HTTPMaxRetries != 3 {
		t.Errorf("HTTPMaxRetries = %d, want 3", HTTPMaxRetries)
	}
}

func TestYAMLConfig_Unmarshal(t *testing.T) {
	t.Parallel()
	rawYAML := `
local:
  - "./go.mod"
  - "../other/go.mod"
github:
  - "owner/repo"
output: "out.json"
minor: false
major: true
all: true
max_probe: 10
json: true
no_color: true
verbose: true
`
	var cfg YAMLConfig
	err := yaml.Unmarshal([]byte(rawYAML), &cfg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if len(cfg.Local) != 2 || cfg.Local[0] != "./go.mod" || cfg.Local[1] != "../other/go.mod" {
		t.Errorf("Local = %v, want ['./go.mod', '../other/go.mod']", cfg.Local)
	}
	if len(cfg.Github) != 1 || cfg.Github[0] != "owner/repo" {
		t.Errorf("Github = %v, want ['owner/repo']", cfg.Github)
	}
	if cfg.Output != "out.json" {
		t.Errorf("Output = %q, want 'out.json'", cfg.Output)
	}
	if cfg.Minor == nil || *cfg.Minor != false {
		t.Errorf("Minor = %v, want false", cfg.Minor)
	}
	if cfg.Major == nil || *cfg.Major != true {
		t.Errorf("Major = %v, want true", cfg.Major)
	}
	if cfg.All == nil || *cfg.All != true {
		t.Errorf("All = %v, want true", cfg.All)
	}
	if cfg.MaxProbe == nil || *cfg.MaxProbe != 10 {
		t.Errorf("MaxProbe = %v, want 10", cfg.MaxProbe)
	}
	if cfg.JSON == nil || *cfg.JSON != true {
		t.Errorf("JSON = %v, want true", cfg.JSON)
	}
	if cfg.NoColor == nil || *cfg.NoColor != true {
		t.Errorf("NoColor = %v, want true", cfg.NoColor)
	}
	if cfg.Verbose == nil || *cfg.Verbose != true {
		t.Errorf("Verbose = %v, want true", cfg.Verbose)
	}
}
