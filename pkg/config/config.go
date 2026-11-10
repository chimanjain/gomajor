package config

import "time"

const (
	// EngineConcurrencyLimit is the maximum ceiling for concurrent module checking
	// and source parsing tasks. Worker pool concurrency is balanced in the engine
	// based on CheckerConcurrencyLimit and MaxProbe to avoid goroutine over-subscription.
	EngineConcurrencyLimit = 100

	// CheckerConcurrencyLimit is the maximum number of concurrent HTTP requests to the Go proxy.
	CheckerConcurrencyLimit = 100

	// HTTPMaxIdleConns is the maximum number of idle connections across all hosts.
	HTTPMaxIdleConns = 100

	// HTTPMaxIdleConnsPerHost is the maximum number of idle connections per host.
	HTTPMaxIdleConnsPerHost = 100

	// HTTPTimeout is the default timeout for HTTP requests.
	HTTPTimeout = 10 * time.Second

	// HTTPResponseHeaderTimeout is the maximum time to wait for a server's response
	// headers after fully writing the request, defending against slowloris attacks.
	HTTPResponseHeaderTimeout = 5 * time.Second

	// HTTPTLSHandshakeTimeout specifies the maximum time to wait for a TLS handshake.
	HTTPTLSHandshakeTimeout = 5 * time.Second

	// HTTPIdleConnTimeout is the maximum time an idle connection remains open.
	HTTPIdleConnTimeout = 90 * time.Second

	// HTTPExpectContinueTimeout is the maximum time to wait for 100-continue response.
	HTTPExpectContinueTimeout = 5 * time.Second

	// HTTPRetryInitialDelay is the initial delay between retry attempts.
	HTTPRetryInitialDelay = 100 * time.Millisecond

	// HTTPMaxRetries is the maximum number of retry attempts.
	HTTPMaxRetries = 3

	// DefaultMaxProbe is the default maximum number of major versions to probe.
	DefaultMaxProbe = 5

	// MaxAllowedMaxProbe is the maximum allowed probe limit to prevent resource exhaustion.
	MaxAllowedMaxProbe = 50

	// MB is one mebibyte (1024 × 1024 bytes).
	MB = 1024 * 1024

	// ProxyResponseMaxBytes is the maximum number of bytes read from a Go module
	// proxy response body, guarding against memory exhaustion.
	ProxyResponseMaxBytes = 10 * MB

	// GitHubModMaxBytes is the maximum number of bytes read when fetching a
	// remote go.mod file from GitHub, guarding against memory exhaustion.
	GitHubModMaxBytes = 5 * MB

	// LocalModMaxBytes is the maximum number of bytes read when loading a
	// local go.mod file, guarding against memory exhaustion and infinite stream hangs.
	LocalModMaxBytes = 5 * MB
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
	LocalMods   []string
	GitHubRepos []string
}

// IsSingleMode reports whether the configuration targets a single local go.mod file.
func (c *Config) IsSingleMode() bool {
	if c == nil {
		return true
	}
	return c.ConfigPath == "" && len(c.GitHubRepos) == 0 && len(c.LocalMods) == 0
}

// YAMLConfig defines the structure for the configuration YAML file.
type YAMLConfig struct {
	Local    []string `yaml:"local"`
	Github   []string `yaml:"github"`
	Output   string   `yaml:"output"`
	Minor    *bool    `yaml:"minor,omitempty"`
	Major    *bool    `yaml:"major,omitempty"`
	All      *bool    `yaml:"all,omitempty"`
	MaxProbe *int     `yaml:"max_probe,omitempty"`
	JSON     *bool    `yaml:"json,omitempty"`
	NoColor  *bool    `yaml:"no_color,omitempty"`
	Verbose  *bool    `yaml:"verbose,omitempty"`
}

// DefaultConfig returns a config with standard default settings.
func DefaultConfig() *Config {
	return &Config{
		ModFilePath: "",
		MaxProbe:    DefaultMaxProbe,
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
