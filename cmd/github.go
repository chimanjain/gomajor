package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// getGithubRawURLs normalizes a GitHub path or URL into candidate raw URL(s).
func getGithubRawURLs(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// If it already looks like a direct raw URL, use it first.
	if strings.HasPrefix(input, "https://raw.githubusercontent.com/") || strings.HasPrefix(input, "http://raw.githubusercontent.com/") {
		return []string{input}
	}

	// Normalize http(s)://github.com/owner/repo/blob/branch/go.mod or similar
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		input = strings.TrimSuffix(input, "/")

		// If it's a link to a go.mod file
		// e.g., https://github.com/owner/repo/blob/branch/go.mod
		reBlob := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)/blob/([^/]+)/(.+)`)
		if reBlob.MatchString(input) {
			raw := reBlob.ReplaceAllString(input, "https://raw.githubusercontent.com/$1/$2/$3/$4")
			return []string{raw}
		}

		// If it's a link to a repo root: https://github.com/owner/repo
		reRepo := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)`)
		if reRepo.MatchString(input) {
			m := reRepo.FindStringSubmatch(input)
			owner := m[1]
			repo := strings.TrimSuffix(m[2], ".git")
			return []string{
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/go.mod", owner, repo),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/go.mod", owner, repo),
			}
		}

		return []string{input}
	}

	// It's a path like "github.com/owner/repo" or "owner/repo"
	parts := strings.Split(input, "/")
	if len(parts) >= 2 {
		var owner, repo string
		if parts[0] == "github.com" && len(parts) >= 3 {
			owner = parts[1]
			repo = parts[2]
		} else {
			owner = parts[0]
			repo = parts[1]
		}
		repo = strings.TrimSuffix(repo, ".git")
		return []string{
			fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/go.mod", owner, repo),
			fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/go.mod", owner, repo),
		}
	}

	return nil
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
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
			continue
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}
		return content, u, nil
	}
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %w", urls, lastErr)
}
