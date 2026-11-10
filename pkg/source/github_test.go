package source

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chimanjain/gomajor/internal/testutil"
	"github.com/chimanjain/gomajor/pkg/config"
)

const githubOwnerRepoURL = "https://github.com/owner/repo"

func TestIsTrustedGitHubHost(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/owner/repo", true},
		{"https://raw.githubusercontent.com/owner/repo/main/go.mod", true},
		{"https://api.github.com/repos/owner/repo", true},
		{"https://enterprise.github.com/api", true},
		{"https://subdomain.githubusercontent.com/file", true},
		{"https://attacker.com/repo", false},
		{"https://evil-github.com/owner/repo", false},
		{"https://notgithubusercontent.com", false},
		{"http://127.0.0.1:8080/go.mod", false},
		{"http://localhost:3000", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isTrustedGitHubHost(tt.url)
			if got != tt.want {
				t.Errorf("isTrustedGitHubHost(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetGithubRawURLs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "already raw url",
			input: "https://raw.githubusercontent.com/owner/repo/main/go.mod",
			want:  []string{"https://raw.githubusercontent.com/owner/repo/main/go.mod"},
		},
		{
			name:  "http raw url",
			input: "http://raw.githubusercontent.com/owner/repo/main/go.mod",
			want:  []string{"http://raw.githubusercontent.com/owner/repo/main/go.mod"},
		},
		{
			name:  "github file link",
			input: "https://github.com/owner/repo/blob/some-branch/go.mod",
			want:  []string{"https://raw.githubusercontent.com/owner/repo/some-branch/go.mod"},
		},
		{
			name:  "github repo link",
			input: githubOwnerRepoURL,
			want: []string{
				"https://raw.githubusercontent.com/owner/repo/main/go.mod",
				"https://raw.githubusercontent.com/owner/repo/master/go.mod",
			},
		},
		{
			name:  "github repo link with dot git",
			input: "https://github.com/owner/repo.git",
			want: []string{
				"https://raw.githubusercontent.com/owner/repo/main/go.mod",
				"https://raw.githubusercontent.com/owner/repo/master/go.mod",
			},
		},
		{
			name:  "shorthand domain owner repo",
			input: "github.com/owner/repo",
			want: []string{
				"https://raw.githubusercontent.com/owner/repo/main/go.mod",
				"https://raw.githubusercontent.com/owner/repo/master/go.mod",
			},
		},
		{
			name:  "shorthand owner repo",
			input: "owner/repo",
			want: []string{
				"https://raw.githubusercontent.com/owner/repo/main/go.mod",
				"https://raw.githubusercontent.com/owner/repo/master/go.mod",
			},
		},
		{
			name:  "github link with credentials",
			input: "https://token:x-oauth-basic@github.com/owner/repo.git",
			want: []string{
				"https://raw.githubusercontent.com/owner/repo/main/go.mod",
				"https://raw.githubusercontent.com/owner/repo/master/go.mod",
			},
		},
		{
			name:  "untrusted host blocked",
			input: "http://127.0.0.1:8080/go.mod",
			want:  nil,
		},
		{
			name:  "attacker host blocked",
			input: "https://attacker.com/repo",
			want:  nil,
		},
		{
			name:  "evil github prefix blocked",
			input: "https://evil-github.com/owner/repo",
			want:  nil,
		},
		{
			name:  "invalid shorthand",
			input: "invalid/shorthand/too/many/parts",
			want:  nil,
		},
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getGithubRawURLs(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getGithubRawURLs(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFetchGithubMod(t *testing.T) {
	tests := []struct {
		name             string
		setup            func(t *testing.T) (inputURL string, candidates []string, client *http.Client, cleanup func())
		wantContent      string
		wantLen          int
		wantErr          bool
		wantURLSuffix    string
		wantCandidateIdx *int
	}{
		{
			name: "Limit",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					chunk := make([]byte, 1024*1024)
					for range 11 {
						_, _ = rw.Write(chunk)
					}
				}))
				return server.URL, []string{server.URL}, server.Client(), server.Close
			},
			wantLen: 5 * config.MB,
		},
		{
			name: "Auth_TokenFromEnv_TrustedHost",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "env-token"
				t.Setenv("GITHUB_TOKEN", token)
				customClient := &http.Client{
					Transport: testutil.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
						if req.Header.Get("Authorization") != "token "+token {
							return &http.Response{
								StatusCode: http.StatusUnauthorized,
								Body:       io.NopCloser(strings.NewReader("unauthorized")),
							}, nil
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader("env-ok")),
						}, nil
					}),
				}
				return "https://raw.githubusercontent.com/owner/repo/main/go.mod", nil, customClient, func() {}
			},
			wantContent: "env-ok",
		},
		{
			name: "Auth_TokenFromEnv_UntrustedHostBlocked",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "env-token"
				t.Setenv("GITHUB_TOKEN", token)
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.Header.Get("Authorization") != "" {
						t.Errorf("expected no Authorization header sent to untrusted host, got %s", req.Header.Get("Authorization"))
						rw.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = rw.Write([]byte("untrusted-ok"))
				}))
				return server.URL, []string{server.URL}, server.Client(), server.Close
			},
			wantContent: "untrusted-ok",
		},
		{
			name: "Auth_TokenFromURL",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "url-token"
				server := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.Header.Get("Authorization") != "token "+token {
						rw.WriteHeader(http.StatusUnauthorized)
						return
					}
					_, _ = rw.Write([]byte("url-ok"))
				}))
				u, _ := url.Parse(server.URL)
				u.User = url.UserPassword("", token)
				return u.String(), []string{server.URL}, server.Client(), server.Close
			},
			wantContent: "url-ok",
		},
		{
			name: "Auth_TokenFromURL_GitHubDomainScoping",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "url-token-gh"
				customClient := &http.Client{
					Transport: testutil.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
						if req.Header.Get("Authorization") != "token "+token {
							return &http.Response{
								StatusCode: http.StatusUnauthorized,
								Body:       io.NopCloser(strings.NewReader("unauthorized")),
							}, nil
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader("scoped-ok")),
						}, nil
					}),
				}
				return "https://url-token-gh@github.com/owner/repo", nil, customClient, func() {}
			},
			wantContent: "scoped-ok",
		},
		{
			name: "Auth_TokenBlockedOverHTTP",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "secret-token"
				t.Setenv("GITHUB_TOKEN", token)
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.Header.Get("Authorization") != "" {
						t.Errorf("Authorization header unexpectedly sent over plaintext HTTP: %s", req.Header.Get("Authorization"))
						rw.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = rw.Write([]byte("plaintext-ok"))
				}))
				return server.URL, []string{server.URL}, server.Client(), server.Close
			},
			wantContent: "plaintext-ok",
		},
		{
			name: "Retries_TransientThenSuccess",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				attempts := 0
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					attempts++
					if attempts < 2 {
						rw.WriteHeader(http.StatusBadGateway)
						return
					}
					_, _ = rw.Write([]byte("retry-ok"))
				}))
				return "mockinput", []string{server.URL}, server.Client(), server.Close
			},
			wantContent: "retry-ok",
		},
		{
			name: "Retries_TerminalImmediateFail",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.WriteHeader(http.StatusNotFound)
				}))
				return "mockinput", []string{server.URL}, server.Client(), server.Close
			},
			wantErr: true,
		},
		{
			name: "Concurrent_RacesAndCancels",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.WriteHeader(http.StatusOK)
					_, _ = rw.Write([]byte("server1-ok"))
				}))
				server2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					select {
					case <-req.Context().Done():
					case <-time.After(150 * time.Millisecond):
						rw.WriteHeader(http.StatusOK)
						_, _ = rw.Write([]byte("server2-ok"))
					}
				}))
				cleanup := func() {
					server1.Close()
					server2.Close()
				}
				return "mockinput", []string{server1.URL, server2.URL}, server1.Client(), cleanup
			},
			wantContent:   "server1-ok",
			wantURLSuffix: "/@latest",
		},
		{
			name: "DeterministicPriority_HigherPriorityPreferred",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					time.Sleep(50 * time.Millisecond)
					rw.WriteHeader(http.StatusOK)
					_, _ = rw.Write([]byte("main-branch-ok"))
				}))
				server2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					// Faster response from lower priority candidate
					rw.WriteHeader(http.StatusOK)
					_, _ = rw.Write([]byte("master-branch-ok"))
				}))
				cleanup := func() {
					server1.Close()
					server2.Close()
				}
				return "mockinput", []string{server1.URL, server2.URL}, server1.Client(), cleanup
			},
			wantContent: "main-branch-ok",
		},
		{
			name: "DeterministicPriority_FallbackWhenHigherPriorityFails",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.WriteHeader(http.StatusNotFound)
				}))
				server2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.WriteHeader(http.StatusOK)
					_, _ = rw.Write([]byte("master-fallback-ok"))
				}))
				cleanup := func() {
					server1.Close()
					server2.Close()
				}
				return "mockinput", []string{server1.URL, server2.URL}, server1.Client(), cleanup
			},
			wantContent:      "master-fallback-ok",
			wantCandidateIdx: func() *int { i := 1; return &i }(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputURL, candidates, client, cleanup := tt.setup(t)
			defer cleanup()

			var resolver func(string) []string
			if len(candidates) > 0 {
				resolver = func(_ string) []string { return candidates }
			}

			content, chosenURL, err := FetchGithubModWithResolver(context.Background(), client, inputURL, resolver)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fetchGithubMod err = %v, wantErr = %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if tt.wantLen > 0 {
				if len(content) != tt.wantLen {
					t.Errorf("len(content) = %d, want %d", len(content), tt.wantLen)
				}
			} else if string(content) != tt.wantContent {
				t.Errorf("content = %q, want %q", string(content), tt.wantContent)
			}

			if len(candidates) > 0 {
				expectedIdx := 0
				if tt.wantCandidateIdx != nil {
					expectedIdx = *tt.wantCandidateIdx
				}
				if chosenURL != candidates[expectedIdx] {
					t.Errorf("chosenURL = %q, want %q", chosenURL, candidates[expectedIdx])
				}
			}
		})
	}
}

func TestResolveGithubToken(t *testing.T) {
	uWithPass, _ := url.Parse("https://user:token-from-url@github.com")
	if tok := resolveGithubToken(uWithPass); tok != "token-from-url" {
		t.Errorf("resolveGithubToken with password = %q, want token-from-url", tok)
	}

	uWithUser, _ := url.Parse("https://user-as-token@github.com")
	if tok := resolveGithubToken(uWithUser); tok != "user-as-token" {
		t.Errorf("resolveGithubToken with username = %q, want user-as-token", tok)
	}

	t.Run("GHTokenEnv", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "gh-env-token")
		t.Setenv("GITHUB_PAT", "")
		if tok := resolveGithubToken(nil); tok != "gh-env-token" {
			t.Errorf("resolveGithubToken with GH_TOKEN = %q, want gh-env-token", tok)
		}
	})

	t.Run("GithubTokenPrecedence", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "primary-token")
		t.Setenv("GH_TOKEN", "secondary-token")
		if tok := resolveGithubToken(nil); tok != "primary-token" {
			t.Errorf("resolveGithubToken precedence = %q, want primary-token", tok)
		}
	})
}
