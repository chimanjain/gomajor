package cmd

import (
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
