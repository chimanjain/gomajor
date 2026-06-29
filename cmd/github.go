package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

	// Full URLs (e.g. https://github.com/owner/repo):
	u, err := url.Parse(input)
	if err == nil && u.Scheme != "" && u.Host != "" {
		if strings.Contains(u.Host, "github.com") {
			trimmedPath := strings.TrimPrefix(strings.TrimSuffix(u.Path, "/"), "/")
			parts := strings.Split(trimmedPath, "/")
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

	var token string
	if u, err := url.Parse(pathOrUrl); err == nil && u.User != nil {
		if passwd, ok := u.User.Password(); ok {
			token = passwd
		} else {
			token = u.User.Username()
		}
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GITHUB_PAT")
	}

	var lastErr error
	for _, u := range urls {
		var resp *http.Response
		delay := 100 * time.Millisecond

		for attempt := 0; attempt < 3; attempt++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				lastErr = err
				break
			}

			if token != "" {
				if strings.Contains(token, " ") {
					req.Header.Set("Authorization", token)
				} else {
					req.Header.Set("Authorization", "token "+token)
				}
			}

			resp, lastErr = client.Do(req)
			if lastErr == nil {
				if resp.StatusCode == http.StatusOK {
					break
				}
				// Terminal errors (e.g. 404, 401, 403) - do not retry this URL
				if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
					_ = resp.Body.Close()
					lastErr = fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
					break
				}
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
			}

			if attempt < 2 {
				select {
				case <-ctx.Done():
					return nil, "", ctx.Err()
				case <-time.After(delay):
					delay *= 2
				}
			}
		}

		if lastErr != nil || resp == nil || resp.StatusCode != http.StatusOK {
			continue
		}

		limitReader := io.LimitReader(resp.Body, 10*1024*1024)
		content, err := io.ReadAll(limitReader)
		_ = resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}
		return content, u, nil
	}
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %w", urls, lastErr)
}
