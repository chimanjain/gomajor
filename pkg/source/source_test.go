package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

var (
	_ Provider = LocalProvider{}
	_ Provider = GitHubProvider{}
)

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "UserCredentials",
			input:    "https://token:x-oauth-basic@github.com/owner/repo/go.mod",
			expected: "https://redacted@github.com/owner/repo/go.mod",
		},
		{
			name:     "UserPassword",
			input:    "https://user:password@git.internal.corp/project/go.mod",
			expected: "https://redacted@git.internal.corp/project/go.mod",
		},
		{
			name:     "NoCredentials",
			input:    githubOwnerRepoURL,
			expected: githubOwnerRepoURL,
		},
		{
			name:     "LocalPath",
			input:    "/run/media/chiman/Data/github/gomajor/go.mod",
			expected: "/run/media/chiman/Data/github/gomajor/go.mod",
		},
		{
			name:     "TokenQueryParam",
			input:    "https://github.com/owner/repo?token=ghp_secret123",
			expected: "https://github.com/owner/repo?token=REDACTED",
		},
		{
			name:     "AccessTokenQueryParam",
			input:    "https://github.com/owner/repo?access_token=abc123&other=safe",
			expected: "https://github.com/owner/repo?access_token=REDACTED&other=safe",
		},
		{
			name:     "MixedCredentialsAndQueryParams",
			input:    "https://user:pass@github.com/repo?api_key=secret&pat=xyz",
			expected: "https://redacted@github.com/repo?api_key=REDACTED&pat=REDACTED",
		},
		{
			name:     "SafeQueryParams",
			input:    "https://github.com/owner/repo?branch=main&ref=v1.0.0",
			expected: "https://github.com/owner/repo?branch=main&ref=v1.0.0",
		},
		{
			name:     "CustomSubstringSecrets",
			input:    "https://github.com/owner/repo?my_secret_token=abc&auth_code=123&user_credential=xyz",
			expected: "https://github.com/owner/repo?auth_code=REDACTED&my_secret_token=REDACTED&user_credential=REDACTED",
		},
		{
			name:     "ExtraSensitiveQueryParams",
			input:    "https://github.com/owner/repo?jwt=token123&sig=mysig&session=sess456",
			expected: "https://github.com/owner/repo?jwt=REDACTED&session=REDACTED&sig=REDACTED",
		},
		{
			name:     "ErrorMessageWithEmbeddedCredentials",
			input:    "failed to fetch from candidates [https://token:secret@github.com]: Get \"https://token:secret@github.com\": 404",
			expected: "failed to fetch from candidates [https://redacted@github.com]: Get \"https://redacted@github.com\": 404",
		},
		{
			name:     "SchemelessCredentials",
			input:    "user:password@github.com/owner/repo",
			expected: "redacted@github.com/owner/repo",
		},
		{
			name:     "EmptyString",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeURL(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseLocalMod(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "go.mod")
		content := "module example.com/test\n\ngo 1.21\n\nrequire github.com/foo/bar v1.0.0\n"
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		ps, err := ParseLocalMod(p)
		if err != nil {
			t.Fatalf("ParseLocalMod failed: %v", err)
		}
		if ps.Source != p || ps.SourceType != Local {
			t.Errorf("unexpected parsed source: %+v", ps)
		}
		if len(ps.Reqs) != 1 || ps.Reqs[0].Mod.Path != "github.com/foo/bar" {
			t.Errorf("unexpected requirements: %+v", ps.Reqs)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := ParseLocalMod("/nonexistent/path/go.mod")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})
}

func TestProviderParse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "go.mod")
	content := "module example.com/test\n\ngo 1.21\n\nrequire github.com/foo/bar v1.0.0\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("GitHubProvider_Parse", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, _ = rw.Write([]byte(content))
		}))
		defer server.Close()

		gp := NewGitHubProvider(server.URL + "/go.mod")
		ps, err := gp.Parse(context.Background(), server.Client())
		if err != nil {
			t.Fatalf("gp.Parse failed: %v", err)
		}
		if ps.SourceType != GitHub || len(ps.Reqs) != 1 {
			t.Errorf("unexpected result: %+v", ps)
		}
	})

	t.Run("GitHubProvider_ParseWithResolver", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, _ = rw.Write([]byte(content))
		}))
		defer server.Close()

		gp := GitHubProvider{
			PathOrURL:   "owner/repo",
			URLResolver: func(string) []string { return []string{server.URL + "/go.mod"} },
		}
		ps, err := gp.Parse(context.Background(), server.Client())
		if err != nil {
			t.Fatalf("gp.Parse with resolver failed: %v", err)
		}
		if ps.SourceType != GitHub || len(ps.Reqs) != 1 {
			t.Errorf("unexpected result: %+v", ps)
		}
	})
}
