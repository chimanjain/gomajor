package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

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

func TestFetchGithubMod_Limit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1024*1024) // 1 MB chunk
		for i := 0; i < 11; i++ {
			_, _ = rw.Write(chunk)
		}
	}))
	defer server.Close()

	content, _, err := fetchGithubMod(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchGithubMod failed: %v", err)
	}

	expectedLimit := 10 * 1024 * 1024
	if len(content) != expectedLimit {
		t.Errorf("expected fetched content size to be capped at %d bytes, got %d", expectedLimit, len(content))
	}
}

func TestFetchGithubMod_Auth(t *testing.T) {
	t.Run("TokenFromEnv", func(t *testing.T) {
		token := "my-secret-token-from-env"
		t.Setenv("GITHUB_TOKEN", token)

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			auth := req.Header.Get("Authorization")
			if auth != "token "+token {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("module my-module\n"))
		}))
		defer server.Close()

		content, _, err := fetchGithubMod(context.Background(), server.Client(), server.URL)
		if err != nil {
			t.Fatalf("fetchGithubMod failed: %v", err)
		}
		if !strings.Contains(string(content), "my-module") {
			t.Errorf("unexpected content: %s", string(content))
		}
	})

	t.Run("TokenFromURL", func(t *testing.T) {
		token := "token-from-url"
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			auth := req.Header.Get("Authorization")
			if auth != "token "+token {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("module url-module\n"))
		}))
		defer server.Close()

		rawURL := server.URL
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("url parse failed: %v", err)
		}
		parsed.User = url.UserPassword("", token)
		authURL := parsed.String()

		content, _, err := fetchGithubMod(context.Background(), server.Client(), authURL)
		if err != nil {
			t.Fatalf("fetchGithubMod failed: %v", err)
		}
		if !strings.Contains(string(content), "url-module") {
			t.Errorf("unexpected content: %s", string(content))
		}
	})
}

func TestFetchGithubMod_Retries(t *testing.T) {
	t.Run("TransientRetriesThenSuccess", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			attempts++
			if attempts < 2 {
				rw.WriteHeader(http.StatusBadGateway) // transient 502
				return
			}
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("module retried\n"))
		}))
		defer server.Close()

		content, _, err := fetchGithubMod(context.Background(), server.Client(), server.URL)
		if err != nil {
			t.Fatalf("fetchGithubMod failed: %v", err)
		}
		if attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
		if !strings.Contains(string(content), "retried") {
			t.Errorf("unexpected content: %s", string(content))
		}
	})

	t.Run("TerminalErrorImmediateFail", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			attempts++
			rw.WriteHeader(http.StatusNotFound) // terminal 404
		}))
		defer server.Close()

		_, _, err := fetchGithubMod(context.Background(), server.Client(), server.URL)
		if err == nil {
			t.Fatal("expected fetchGithubMod to fail")
		}
		if attempts != 1 {
			t.Errorf("expected exactly 1 attempt for 404 terminal error, got %d", attempts)
		}
	})
}
