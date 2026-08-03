package utils

import (
	"errors"
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

func TestGetGoEnv(t *testing.T) {
	defer ClearGoEnvCache()

	t.Run("ProcessEnvPriority", func(t *testing.T) {
		ClearGoEnvCache()
		t.Setenv("TEST_GOPROXY_VAR", "https://custom.proxy.org")
		got := GetGoEnv("TEST_GOPROXY_VAR")
		if got != "https://custom.proxy.org" {
			t.Errorf("GetGoEnv() = %q, want %q", got, "https://custom.proxy.org")
		}
	})

	t.Run("FallbackAndCaching", func(t *testing.T) {
		ClearGoEnvCache()
		origExec := execGoEnv
		defer func() { execGoEnv = origExec }()

		calls := 0
		execGoEnv = func(key string) (string, error) {
			calls++
			if key == "TEST_UNSET_VAR" {
				return "from-go-env", nil
			}
			return "", nil
		}

		// First lookup should call execGoEnv
		val1 := GetGoEnv("TEST_UNSET_VAR")
		if val1 != "from-go-env" {
			t.Errorf("First GetGoEnv() = %q, want %q", val1, "from-go-env")
		}
		if calls != 1 {
			t.Errorf("execGoEnv call count = %d, want 1", calls)
		}

		// Second lookup should use cache
		val2 := GetGoEnv("TEST_UNSET_VAR")
		if val2 != "from-go-env" {
			t.Errorf("Second GetGoEnv() = %q, want %q", val2, "from-go-env")
		}
		if calls != 1 {
			t.Errorf("execGoEnv call count after cache hit = %d, want 1", calls)
		}
	})

	t.Run("ErrorFallback", func(t *testing.T) {
		ClearGoEnvCache()
		origExec := execGoEnv
		defer func() { execGoEnv = origExec }()

		execGoEnv = func(key string) (string, error) {
			return "", errors.New("mock error")
		}

		val := GetGoEnv("TEST_ERR_VAR")
		if val != "" {
			t.Errorf("GetGoEnv() on error = %q, want %q", val, "")
		}
	})
}
