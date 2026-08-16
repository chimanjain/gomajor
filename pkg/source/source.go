package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	// Local indicates a go.mod loaded from the local filesystem.
	Local Type = "local"
	// GitHub indicates a go.mod fetched from a remote GitHub repository.
	GitHub Type = "github"
)

var (
	urlRegex                = regexp.MustCompile(`https?://[^\s"'<>\],]+`)
	schemelessUserPassRegex = regexp.MustCompile(`(^|[\s"'\(\[])([a-zA-Z0-9_.-]+:[^@\s/]+)@([a-zA-Z0-9_.-]+)`)
)

// Type identifies where a go.mod was loaded from.
type Type string

// ParsedSource represents a parsed go.mod file.
type ParsedSource struct {
	Source     string
	SourceType Type
	Reqs       []*modfile.Require
}

// Provider defines the interface for fetching and parsing a go.mod source.
type Provider interface {
	Name() string
	Type() Type
	Parse(ctx context.Context, httpClient *http.Client) (ParsedSource, error)
}

// LocalProvider implements Provider for local go.mod files.
type LocalProvider struct {
	Path string
}

// GitHubProvider implements Provider for GitHub repositories.
type GitHubProvider struct {
	PathOrURL   string
	URLResolver func(string) []string
}

// NewLocalProvider returns a new LocalProvider for the given file path.
func NewLocalProvider(path string) LocalProvider {
	return LocalProvider{Path: path}
}

// NewGitHubProvider returns a new GitHubProvider for the given path or URL.
func NewGitHubProvider(pathOrURL string) GitHubProvider {
	return GitHubProvider{PathOrURL: pathOrURL}
}

func (p LocalProvider) Name() string { return p.Path }
func (p LocalProvider) Type() Type   { return Local }
func (p LocalProvider) Parse(_ context.Context, _ *http.Client) (ParsedSource, error) {
	return ParseLocalMod(p.Path)
}

func (p GitHubProvider) Name() string { return SanitizeURL(p.PathOrURL) }
func (p GitHubProvider) Type() Type   { return GitHub }
func (p GitHubProvider) Parse(ctx context.Context, httpClient *http.Client) (ParsedSource, error) {
	if p.URLResolver != nil {
		return parseGithubModWithResolver(ctx, httpClient, p.PathOrURL, p.URLResolver)
	}
	return ParseGithubMod(ctx, httpClient, p.PathOrURL)
}

// ParseLocalMod reads and parses a local go.mod file.
func ParseLocalMod(path string) (ParsedSource, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	return parseModContent(path, Local, content)
}

// ParseGithubMod fetches and parses a remote go.mod file from GitHub.
func ParseGithubMod(ctx context.Context, httpClient *http.Client, pathOrURL string) (ParsedSource, error) {
	content, resolvedURL, err := FetchGithubMod(ctx, httpClient, pathOrURL)
	if err != nil {
		return ParsedSource{}, err
	}
	return parseModContent(resolvedURL, GitHub, content)
}

// SanitizeURL strips credentials (usernames, passwords, or tokens) and
// sensitive query parameters from a URL or text containing URLs.
func SanitizeURL(raw string) string {
	if raw == "" {
		return raw
	}

	// Fast path: clean single URL with scheme and no spaces or surrounding punctuation
	if (strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")) &&
		!strings.ContainsAny(raw, " \t\n\r\"'[]<>,") {
		return sanitizeSingleURL(raw)
	}

	// For general text (error messages, compound strings), find and sanitize all embedded URLs
	sanitized := urlRegex.ReplaceAllStringFunc(raw, sanitizeSingleURL)

	// Also redact any schemeless credentials: user:pass@host
	sanitized = schemelessUserPassRegex.ReplaceAllString(sanitized, "${1}redacted@${3}")

	return sanitized
}

// parseModContent parses the module content and extracts required dependencies.
func parseModContent(sourceName string, sourceType Type, content []byte) (ParsedSource, error) {
	modFile, err := modfile.ParseLax(sourceName, content, nil)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("parsing go.mod: %w", err)
	}
	return ParsedSource{
		Source:     SanitizeURL(sourceName),
		SourceType: sourceType,
		Reqs:       modFile.Require,
	}, nil
}

// sanitizeSingleURL sanitizes a single parsed HTTP/HTTPS URL.
func sanitizeSingleURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	modified := false

	if u.User != nil {
		u.User = url.User("redacted")
		modified = true
	}

	if u.RawQuery != "" {
		q := u.Query()
		for key := range q {
			if isSensitiveParam(key) {
				q.Set(key, "REDACTED")
				modified = true
			}
		}
		if modified {
			u.RawQuery = q.Encode()
		}
	}

	if !modified {
		return raw
	}
	return u.String()
}

// isSensitiveParam returns true if the query parameter name looks like it
// could contain a secret value.
func isSensitiveParam(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "key", "api_key", "apikey", "pat", "pass", "pwd", "bearer", "sig", "signature", "jwt", "session", "sessionid", "hash":
		return true
	}
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "auth") ||
		strings.Contains(lower, "credential") || strings.Contains(lower, "private")
}
