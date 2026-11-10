package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/chimanjain/gomajor/internal/sanitize"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/model"
	"golang.org/x/mod/modfile"
)

// Type identifies where a go.mod was loaded from.
type Type = model.SourceType

const (
	// Local indicates a go.mod loaded from the local filesystem.
	Local = model.Local
	// GitHub indicates a go.mod fetched from a remote GitHub repository.
	GitHub = model.GitHub
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

// ParseLocalMod reads and parses a local go.mod file, verifying that the target
// is a regular file and guarding against memory exhaustion and infinite stream hangs.
func ParseLocalMod(path string) (ParsedSource, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return ParsedSource{}, fmt.Errorf("reading file: target is not a regular file: %s", path)
	}
	if fi.Size() > config.LocalModMaxBytes {
		return ParsedSource{}, fmt.Errorf("reading file: file exceeds maximum allowed size (%d bytes): %s", config.LocalModMaxBytes, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	defer f.Close()

	content, err := io.ReadAll(io.LimitReader(f, config.LocalModMaxBytes+1))
	if err != nil {
		return ParsedSource{}, fmt.Errorf("reading file: %w", err)
	}
	if int64(len(content)) > config.LocalModMaxBytes {
		return ParsedSource{}, fmt.Errorf("reading file: file exceeds maximum allowed size (%d bytes): %s", config.LocalModMaxBytes, path)
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

// SanitizeURL strips credentials (usernames, passwords, or tokens), sensitive query
// parameters, and standalone authentication tokens from a URL or text containing URLs/errors.
func SanitizeURL(raw string) string {
	return sanitize.URL(raw)
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
