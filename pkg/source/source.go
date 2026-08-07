package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/mod/modfile"
)

// Type identifies where a go.mod was loaded from.
type Type string

const (
	// Local indicates a go.mod loaded from the local filesystem.
	Local Type = "local"
	// GitHub indicates a go.mod fetched from a remote GitHub repository.
	GitHub Type = "github"
)

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

// NewLocalProvider returns a new LocalProvider for the given file path.
func NewLocalProvider(path string) LocalProvider {
	return LocalProvider{Path: path}
}

func (p LocalProvider) Name() string { return p.Path }
func (p LocalProvider) Type() Type   { return Local }
func (p LocalProvider) Parse(_ context.Context, _ *http.Client) (ParsedSource, error) {
	return ParseLocalMod(p.Path)
}

// GitHubProvider implements Provider for GitHub repositories.
type GitHubProvider struct {
	PathOrURL   string
	URLResolver func(string) []string
}

// NewGitHubProvider returns a new GitHubProvider for the given path or URL.
func NewGitHubProvider(pathOrURL string) GitHubProvider {
	return GitHubProvider{PathOrURL: pathOrURL}
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

// SanitizeURL strips credentials (usernames, passwords, or tokens) and
// sensitive query parameters from a raw URL.
func SanitizeURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
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
	case "key", "api_key", "apikey", "pat", "pass", "pwd", "bearer":
		return true
	}
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "auth") ||
		strings.Contains(lower, "credential")
}
