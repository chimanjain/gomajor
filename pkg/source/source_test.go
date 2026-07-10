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
