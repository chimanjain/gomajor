package constants

import "time"

const (
	// EngineConcurrencyLimit is the maximum number of goroutines for parsing
	// and checking modules. Kept equal to CheckerConcurrencyLimit so the
	// engine never spawns more goroutines than can be simultaneously served
	// by the HTTP semaphore, avoiding wasted goroutine stacks.
	EngineConcurrencyLimit = 100

	// CheckerConcurrencyLimit is the maximum number of concurrent HTTP requests to the Go proxy.
	CheckerConcurrencyLimit = 100

	// HTTPMaxIdleConns is the maximum number of idle connections across all hosts.
	HTTPMaxIdleConns = 100

	// HTTPMaxIdleConnsPerHost is the maximum number of idle connections per host.
	HTTPMaxIdleConnsPerHost = 100

	// HTTPTimeout is the default timeout for HTTP requests.
	HTTPTimeout = 10 * time.Second

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
)
