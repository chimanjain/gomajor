package source

import (
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://token:x-oauth-basic@github.com/owner/repo/go.mod",
			expected: "https://redacted@github.com/owner/repo/go.mod",
		},
		{
			input:    "https://user:password@git.internal.corp/project/go.mod",
			expected: "https://redacted@git.internal.corp/project/go.mod",
		},
		{
			input:    githubOwnerRepoURL,
			expected: githubOwnerRepoURL,
		},
		{
			input:    "/run/media/chiman/Data/github/gomajor/go.mod",
			expected: "/run/media/chiman/Data/github/gomajor/go.mod",
		},
	}

	for _, tt := range tests {
		got := SanitizeURL(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
