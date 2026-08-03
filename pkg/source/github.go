package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cenkalti/backoff/v7"
	"github.com/chimanjain/gomajor/pkg/constants"
	"github.com/chimanjain/gomajor/utils"
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
	// tokenSourceHost is non-empty only when the token was embedded in the URL
	// user-info field. In that case we restrict the token to requests targeting
	// the same host, preventing accidental credential leakage to other servers.
	// Env-var tokens (tokenSourceHost == "") are forwarded to all candidates
	// because the user deliberately set them as global credentials.
	tokenSourceHost := resolveTokenHost(pathOrURL)

	var lastErr error
	for _, u := range urls {
		// Send the token if it was from env (always global) or from a URL whose
		// host matches the candidate URL's host.
		sendToken := token != "" && (tokenSourceHost == "" || urlHostMatches(u, tokenSourceHost))
		content, err := fetchSingleURL(ctx, client, u, sendToken, token)
		if err == nil {
			return content, u, nil
		}
		lastErr = err
	}

	sanitizedUrls := make([]string, len(urls))
	for i, u := range urls {
		sanitizedUrls[i] = SanitizeURL(u)
	}

	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %w", sanitizedUrls, lastErr)
}

// resolveGithubToken extracts a token from the URL user-info field, falling
// back to the GITHUB_TOKEN and GITHUB_PAT environment variables.
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
		token = utils.GetGoEnv("GITHUB_TOKEN")
	}
	if token == "" {
		token = utils.GetGoEnv("GITHUB_PAT")
	}
	return token
}

// resolveTokenHost returns the hostname that a URL-embedded token was scoped to.
// If the token came from an environment variable (no user-info in the URL) an
// empty string is returned, which hostMatches treats as "match any github host".
func resolveTokenHost(pathOrURL string) string {
	if u, err := url.Parse(pathOrURL); err == nil && u.User != nil && u.Host != "" {
		return u.Hostname()
	}
	return ""
}

// urlHostMatches reports whether targetURL's hostname equals wantHost.
func urlHostMatches(targetURL, wantHost string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return u.Hostname() == wantHost
}

func fetchSingleURL(ctx context.Context, client *http.Client, u string, sendToken bool, token string) ([]byte, error) {
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

		if sendToken && token != "" {
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
			limitReader := io.LimitReader(resp.Body, constants.GitHubModMaxBytes)
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
