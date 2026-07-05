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

// ParsedSource represents a parsed go.mod file.
type ParsedSource struct {
	Source     string
	SourceType string
	Reqs       []*modfile.Require
}

// ParseLocalMod reads and parses a local go.mod file.
func ParseLocalMod(path string) (ParsedSource, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	return parseModContent(path, "local", content)
}

// ParseGithubMod fetches and parses a remote go.mod file from GitHub.
func ParseGithubMod(ctx context.Context, httpClient *http.Client, pathOrURL string) (ParsedSource, error) {
	content, resolvedURL, err := FetchGithubMod(ctx, httpClient, pathOrURL)
	if err != nil {
		return ParsedSource{}, err
	}
	return parseModContent(resolvedURL, "github", content)
}

// parseModContent parses the module content and extracts required dependencies.
func parseModContent(sourceName string, sourceType string, content []byte) (ParsedSource, error) {
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

// SanitizeURL strips credentials (usernames, passwords, or tokens) from a raw URL.
func SanitizeURL(raw string) string {
	if !strings.Contains(raw, "@") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("redacted")
	return u.String()
}
