package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getGithubRawURLs normalizes a GitHub path or URL into candidate raw URL(s).
func getGithubRawURLs(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	if strings.Contains(input, "raw.githubusercontent.com") {
		return []string{input}
	}

	// Shorthand formats (e.g. "owner/repo" or "github.com/owner/repo"):
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		trimmed := strings.TrimPrefix(input, "github.com/")
		trimmed = strings.TrimSuffix(trimmed, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			owner := parts[0]
			repo := strings.TrimSuffix(parts[1], ".git")
			return []string{
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/go.mod", owner, repo),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/go.mod", owner, repo),
			}
		}
		return []string{input}
	}

	// Full URLs that are not GitHub links (e.g. custom mock server URLs):
	if !strings.Contains(input, "github.com/") {
		return []string{input}
	}

	// It is a GitHub URL. Strip prefix to get the path segments.
	trimmed := input
	for _, prefix := range []string{"https://", "http://", "github.com/"} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	trimmed = strings.TrimSuffix(trimmed, "/")

	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 {
		owner := parts[0]
		repo := strings.TrimSuffix(parts[1], ".git")
		if len(parts) >= 5 && parts[2] == "blob" {
			branch := parts[3]
			path := strings.Join(parts[4:], "/")
			return []string{fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, path)}
		}
		return []string{
			fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/go.mod", owner, repo),
			fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/go.mod", owner, repo),
		}
	}
	return []string{input}
}

// fetchGithubMod retrieves a go.mod from a GitHub repository or URL.
func fetchGithubMod(ctx context.Context, client *http.Client, pathOrUrl string) ([]byte, string, error) {
	urls := getGithubRawURLs(pathOrUrl)
	if len(urls) == 0 {
		return nil, "", fmt.Errorf("invalid github repository format: %s", pathOrUrl)
	}

	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
			_ = resp.Body.Close()
			continue
		}

		content, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}
		return content, u, nil
	}
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %w", urls, lastErr)
}
