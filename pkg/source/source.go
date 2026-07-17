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

// SourceType identifies where a go.mod was loaded from.
type SourceType string

const (
	// SourceTypeLocal indicates a go.mod loaded from the local filesystem.
	SourceTypeLocal SourceType = "local"
	// SourceTypeGitHub indicates a go.mod fetched from a remote GitHub repository.
	SourceTypeGitHub SourceType = "github"
)

// ParsedSource represents a parsed go.mod file.
type ParsedSource struct {
	Source     string
	SourceType SourceType
	Reqs       []*modfile.Require
}

// ParseLocalMod reads and parses a local go.mod file.
func ParseLocalMod(path string) (ParsedSource, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	return parseModContent(path, SourceTypeLocal, content)
}

// ParseGithubMod fetches and parses a remote go.mod file from GitHub.
func ParseGithubMod(ctx context.Context, httpClient *http.Client, pathOrURL string) (ParsedSource, error) {
	content, resolvedURL, err := FetchGithubMod(ctx, httpClient, pathOrURL)
	if err != nil {
		return ParsedSource{}, err
	}
	return parseModContent(resolvedURL, SourceTypeGitHub, content)
}

// parseModContent parses the module content and extracts required dependencies.
func parseModContent(sourceName string, sourceType SourceType, content []byte) (ParsedSource, error) {
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
	case "token", "access_token", "secret", "password", "auth",
		"key", "api_key", "apikey", "pat", "private_token":
		return true
	}
	return false
}
