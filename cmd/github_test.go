package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			input: "https://github.com/owner/repo",
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
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
