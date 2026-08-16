package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cenkalti/backoff/v7"
	"github.com/chimanjain/gomajor/internal/goenv"
	"github.com/chimanjain/gomajor/pkg/constants"
)

// FetchGithubMod retrieves a go.mod from a GitHub repository or URL using default URL resolution.
func FetchGithubMod(ctx context.Context, client *http.Client, pathOrURL string) ([]byte, string, error) {
	return FetchGithubModWithResolver(ctx, client, pathOrURL, getGithubRawURLs)
}

// FetchGithubModWithResolver retrieves a go.mod using a specified URL resolver.
func FetchGithubModWithResolver(ctx context.Context, client *http.Client, pathOrURL string, resolver func(string) []string) ([]byte, string, error) {
	if resolver == nil {
		resolver = getGithubRawURLs
	}
	urls := resolver(pathOrURL)
	if len(urls) == 0 {
		return nil, "", fmt.Errorf("invalid github repository format: %s", pathOrURL)
	}

	var parsedURL *url.URL
	if strings.Contains(pathOrURL, "://") {
		parsedURL, _ = url.Parse(pathOrURL)
	}

	token := resolveGithubToken(parsedURL)
	// tokenSourceHost is non-empty only when the token was embedded in the URL
	// user-info field. In that case we restrict the token to requests targeting
	// the same host, preventing accidental credential leakage to other servers.
	// Env-var tokens (tokenSourceHost == "") are restricted strictly to trusted
	// GitHub domains to prevent leaking tokens to untrusted third-party hosts.
	tokenSourceHost := resolveTokenHost(parsedURL)

	if len(urls) == 1 {
		u := urls[0]
		sendToken := token != "" && shouldSendToken(u, tokenSourceHost)
		content, err := fetchSingleURL(ctx, client, u, sendToken, token)
		if err == nil {
			return content, u, nil
		}
		return nil, "", fmt.Errorf("failed to fetch go.mod from candidates [%s]: %s", SanitizeURL(u), SanitizeURL(err.Error()))
	}

	type fetchResult struct {
		content []byte
		url     string
		err     error
	}

	cCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan := make(chan fetchResult, len(urls))
	for _, u := range urls {
		go func(targetURL string) {
			sendToken := token != "" && shouldSendToken(targetURL, tokenSourceHost)
			content, err := fetchSingleURL(cCtx, client, targetURL, sendToken, token)
			resChan <- fetchResult{content: content, url: targetURL, err: err}
		}(u)
	}

	var lastErr error
	for range len(urls) {
		select {
		case res := <-resChan:
			if res.err == nil {
				cancel()
				return res.content, res.url, nil
			}
			lastErr = res.err
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}

	sanitizedUrls := make([]string, len(urls))
	for i, u := range urls {
		sanitizedUrls[i] = SanitizeURL(u)
	}
	// lastErr is always non-nil here: len(urls) > 0 and every attempt failed.
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %s", sanitizedUrls, SanitizeURL(lastErr.Error()))
}

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

// parseGithubModWithResolver parses GitHub mod content using a custom URL resolver function.
func parseGithubModWithResolver(ctx context.Context, httpClient *http.Client, pathOrURL string, resolver func(string) []string) (ParsedSource, error) {
	content, resolvedURL, err := FetchGithubModWithResolver(ctx, httpClient, pathOrURL, resolver)
	if err != nil {
		return ParsedSource{}, err
	}
	return parseModContent(resolvedURL, GitHub, content)
}

// resolveGithubToken extracts a token from the URL user-info field, falling
// back to the GITHUB_TOKEN and GITHUB_PAT environment variables.
func resolveGithubToken(u *url.URL) string {
	if u != nil && u.User != nil {
		if passwd, ok := u.User.Password(); ok {
			return passwd
		}
		return u.User.Username()
	}
	if t := goenv.Get("GITHUB_TOKEN"); t != "" {
		return t
	}
	return goenv.Get("GITHUB_PAT")
}

// resolveTokenHost returns the hostname that a URL-embedded token was scoped to.
// If the token came from an environment variable (no user-info in the URL) an
// empty string is returned.
func resolveTokenHost(u *url.URL) string {
	if u != nil && u.User != nil && u.Host != "" {
		return u.Hostname()
	}
	return ""
}

// isTrustedGitHubHost reports whether targetURL is an official GitHub host.
func isTrustedGitHubHost(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || strings.HasSuffix(host, ".github.com") ||
		host == "githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// shouldSendToken determines whether the authentication token should be sent to targetURL.
// Scoped tokens (from URL user-info) are sent only to the host in targetURL matching tokenSourceHost.
// Global environment tokens (tokenSourceHost == "") are restricted strictly to trusted GitHub domains.
func shouldSendToken(targetURL, tokenSourceHost string) bool {
	if tokenSourceHost != "" {
		return urlHostMatches(targetURL, tokenSourceHost)
	}
	return isTrustedGitHubHost(targetURL)
}

// urlHostMatches reports whether targetURL's hostname equals wantHost.
func urlHostMatches(targetURL, wantHost string) bool {
	u, err := url.Parse(targetURL)
	return err == nil && strings.EqualFold(u.Hostname(), wantHost)
}

func fetchSingleURL(ctx context.Context, client *http.Client, u string, sendToken bool, token string) ([]byte, error) {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = constants.HTTPRetryInitialDelay

	maxTries := max(uint(constants.HTTPMaxRetries), 1)

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
		defer func() {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}()

		if resp.StatusCode == http.StatusOK {
			return io.ReadAll(io.LimitReader(resp.Body, constants.GitHubModMaxBytes))
		}

		errStatus := fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return nil, backoff.Permanent(errStatus)
		}
		return nil, errStatus
	}

	return backoff.Retry(ctx, operation, backoff.WithBackOff(b), backoff.WithMaxTries(maxTries))
}
