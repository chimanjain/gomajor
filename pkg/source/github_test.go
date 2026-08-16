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

	"github.com/chimanjain/gomajor/pkg/constants"
)

const githubOwnerRepoURL = "https://github.com/owner/repo"

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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
		name          string
		setup         func(t *testing.T) (inputURL string, candidates []string, client *http.Client, cleanup func())
		wantContent   string
		wantLen       int
		wantErr       bool
		wantURLSuffix string
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
				return server.URL, nil, server.Client(), server.Close
			},
			wantLen: 5 * constants.MB,
		},
		{
			name: "Auth_TokenFromEnv_TrustedHost",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "env-token"
				t.Setenv("GITHUB_TOKEN", token)
				customClient := &http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
				return server.URL, nil, server.Client(), server.Close
			},
			wantContent: "untrusted-ok",
		},
		{
			name: "Auth_TokenFromURL",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				token := "url-token"
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.Header.Get("Authorization") != "token "+token {
						rw.WriteHeader(http.StatusUnauthorized)
						return
					}
					_, _ = rw.Write([]byte("url-ok"))
				}))
				u, _ := url.Parse(server.URL)
				u.User = url.UserPassword("", token)
				return u.String(), nil, server.Client(), server.Close
			},
			wantContent: "url-ok",
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
				return server.URL, nil, server.Client(), server.Close
			},
			wantContent: "retry-ok",
		},
		{
			name: "Retries_TerminalImmediateFail",
			setup: func(t *testing.T) (string, []string, *http.Client, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.WriteHeader(http.StatusNotFound)
				}))
				return server.URL, nil, server.Client(), server.Close
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
				if chosenURL != candidates[0] {
					t.Errorf("chosenURL = %q, want %q", chosenURL, candidates[0])
				}
			}
		})
	}
}
