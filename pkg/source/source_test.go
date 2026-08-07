package source

import (
	"testing"
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

var (
	_ Provider = LocalProvider{}
	_ Provider = GitHubProvider{}
)

func TestProviders(t *testing.T) {
	t.Run("LocalProvider", func(t *testing.T) {
		lp := NewLocalProvider("go.mod")
		if lp.Name() != "go.mod" {
			t.Errorf("lp.Name() = %q, want %q", lp.Name(), "go.mod")
		}
		if lp.Type() != Local {
			t.Errorf("lp.Type() = %q, want %q", lp.Type(), Local)
		}
	})

	t.Run("GitHubProvider", func(t *testing.T) {
		gp := NewGitHubProvider("owner/repo")
		if gp.Name() != "owner/repo" {
			t.Errorf("gp.Name() = %q, want %q", gp.Name(), "owner/repo")
		}
		if gp.Type() != GitHub {
			t.Errorf("gp.Type() = %q, want %q", gp.Type(), GitHub)
		}
	})
}
