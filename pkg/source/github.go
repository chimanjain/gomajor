package source

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/chimanjain/gomajor/internal/retry"
	"github.com/chimanjain/gomajor/pkg/config"
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
		index   int
		content []byte
		url     string
		err     error
	}

	cCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan := make(chan fetchResult, len(urls))
	for i, u := range urls {
		go func(idx int, targetURL string) {
			sendToken := token != "" && shouldSendToken(targetURL, tokenSourceHost)
			content, err := fetchSingleURL(cCtx, client, targetURL, sendToken, token)
			resChan <- fetchResult{index: idx, content: content, url: targetURL, err: err}
		}(i, u)
	}

	finished := make([]bool, len(urls))
	var bestResult fetchResult
	bestSuccessIdx := -1
	var lastErr error

	for range len(urls) {
		select {
		case res := <-resChan:
			finished[res.index] = true
			if res.err == nil {
				if bestSuccessIdx == -1 || res.index < bestSuccessIdx {
					bestSuccessIdx = res.index
					bestResult = res
				}
			} else {
				lastErr = res.err
			}

			// If we have a successful candidate, check whether all higher-priority
			// (lower-indexed) candidates have finished. If so, no higher-priority candidate
			// can arrive, so we can commit to this result immediately.
			if bestSuccessIdx != -1 {
				higherPriorityPending := false
				for j := 0; j < bestSuccessIdx; j++ {
					if !finished[j] {
						higherPriorityPending = true
						break
					}
				}
				if !higherPriorityPending {
					cancel()
					return bestResult.content, bestResult.url, nil
				}
			}
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}

	if bestSuccessIdx != -1 {
		cancel()
		return bestResult.content, bestResult.url, nil
	}

	sanitizedUrls := make([]string, len(urls))
	for i, u := range urls {
		sanitizedUrls[i] = SanitizeURL(u)
	}
	// lastErr is always non-nil here: len(urls) > 0 and every attempt failed.
	return nil, "", fmt.Errorf("failed to fetch go.mod from candidates %v: %s", sanitizedUrls, SanitizeURL(lastErr.Error()))
}

// getGithubRawURLs normalizes a GitHub path or URL into candidate raw URL(s).
// It strictly validates that inputs are valid GitHub repositories or URLs,
// returning nil for non-GitHub or invalid inputs to prevent SSRF.
func getGithubRawURLs(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil || !isTrustedGitHubHost(input) {
			return nil
		}

		if strings.Contains(u.Host, "raw.githubusercontent.com") {
			return []string{input}
		}

		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] != "" {
			owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
			if len(parts) >= 5 && parts[2] == "blob" {
				return []string{fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, parts[3], strings.Join(parts[4:], "/"))}
			}
			return rawModCandidates(owner, repo)
		}
		return nil
	}

	trimmed := strings.TrimPrefix(strings.Trim(input, "/"), "github.com/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
		if !strings.ContainsAny(owner, " \t\n\r/\\:@?#") && !strings.ContainsAny(repo, " \t\n\r/\\:@?#") {
			return rawModCandidates(owner, repo)
		}
	}

	return nil
}

func rawModCandidates(owner, repo string) []string {
	return []string{
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/go.mod", owner, repo),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/go.mod", owner, repo),
	}
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
// back to the GITHUB_TOKEN, GH_TOKEN, and GITHUB_PAT environment variables.
func resolveGithubToken(u *url.URL) string {
	if u != nil && u.User != nil {
		if passwd, ok := u.User.Password(); ok {
			return passwd
		}
		return u.User.Username()
	}
	return cmp.Or(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_PAT"))
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
// Tokens are strictly transmitted over HTTPS.
// Scoped tokens (from URL user-info) are sent only to the host in targetURL matching tokenSourceHost,
// or between trusted GitHub hosts (e.g. github.com and raw.githubusercontent.com).
// Global environment tokens (tokenSourceHost == "") are restricted strictly to trusted GitHub domains.
func shouldSendToken(targetURL, tokenSourceHost string) bool {
	u, err := url.Parse(targetURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if tokenSourceHost != "" {
		if isTrustedGitHubHost("https://"+tokenSourceHost) && isTrustedGitHubHost(targetURL) {
			return true
		}
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
	if client == nil {
		client = http.DefaultClient
	}

	doFetch := func(withAuth bool) ([]byte, int, error) {
		baseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, 0, err
		}

		if withAuth && sendToken && token != "" {
			if strings.Contains(token, " ") {
				baseReq.Header.Set("Authorization", token)
			} else {
				baseReq.Header.Set("Authorization", "token "+token)
			}
		}

		var lastStatus int
		operation := func() ([]byte, error) {
			req := baseReq.Clone(ctx)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer func() {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
			}()

			lastStatus = resp.StatusCode
			if resp.StatusCode == http.StatusOK {
				return io.ReadAll(io.LimitReader(resp.Body, config.GitHubModMaxBytes))
			}

			errStatus := fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return nil, retry.Permanent(errStatus)
			}
			return nil, errStatus
		}

		body, err := retry.Do(ctx, uint(config.HTTPMaxRetries), config.HTTPRetryInitialDelay, operation)
		return body, lastStatus, err
	}

	// Attempt unauthenticated request first to avoid transmitting credentials on
	// public repositories. If the unauthenticated request returns 401 Unauthorized
	// or 404 Not Found (private repo), retry with authentication.
	if sendToken && token != "" {
		body, status, err := doFetch(false)
		if err == nil {
			return body, nil
		}
		if status == http.StatusUnauthorized || status == http.StatusNotFound {
			bodyAuth, _, errAuth := doFetch(true)
			return bodyAuth, errAuth
		}
		return nil, err
	}

	body, _, err := doFetch(false)
	return body, err
}
