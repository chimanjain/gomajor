package utils

import (
	"testing"
)

func TestNormalizeSplitString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"Empty", "", nil},
		{"Simple comma", "a,b", []string{"a", "b"}},
		{"Simple space", "a b", []string{"a", "b"}},
		{"Mixed separators", "a, b\tc\nd\re", []string{"a", "b", "c", "d", "e"}},
		{"Extra spaces and commas", " , a , , b , ", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSplitString(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("NormalizeSplitString(%q) length = %d, want %d (got=%v, want=%v)", tt.input, len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("NormalizeSplitString(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
