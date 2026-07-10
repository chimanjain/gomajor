package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/cenkalti/backoff/v7"
	"github.com/chimanjain/gomajor/pkg/constants"
)

// getGithubRawURLs normalizes a GitHub path or URL into candidate raw URL(s).
var getGithubRawURLs = func(input string) []string {
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

// FetchGithubMod retrieves a go.mod from a GitHub repository or URL.
func FetchGithubMod(ctx context.Context, client *http.Client, pathOrURL string) ([]byte, string, error) {
	urls := getGithubRawURLs(pathOrURL)
	if len(urls) == 0 {
		return nil, "", fmt.Errorf("invalid github repository format: %s", pathOrURL)
	}

	token := resolveGithubToken(pathOrURL)

	var lastErr error
	for _, u := range urls {
		content, err := fetchSingleURL(ctx, client, u, token)
		if err == nil {
			return content, u, nil
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %w", urls, lastErr)
}

func resolveGithubToken(pathOrURL string) string {
	var token string
	if u, err := url.Parse(pathOrURL); err == nil && u.User != nil {
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
	return token
}

func fetchSingleURL(ctx context.Context, client *http.Client, u, token string) ([]byte, error) {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = constants.HTTPRetryInitialDelay

	maxTries := uint(constants.HTTPMaxRetries)
	if maxTries == 0 {
		maxTries = 1
	}

	operation := func() ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, backoff.Permanent(err)
		}

		if token != "" {
			if strings.Contains(token, " ") {
				req.Header.Set("Authorization", token)
			} else {
				req.Header.Set("Authorization", "token "+token)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			limitReader := io.LimitReader(resp.Body, 10*1024*1024)
			return io.ReadAll(limitReader)
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable {
			return nil, backoff.Permanent(fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status))
		}

		return nil, fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
	}

	return backoff.Retry(ctx, operation, backoff.WithBackOff(b), backoff.WithMaxTries(maxTries))
}
