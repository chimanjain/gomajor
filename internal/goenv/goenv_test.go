package goenv

import (
	"errors"
	"testing"
)

func TestGet(t *testing.T) {
	defer ClearCache()

	t.Run("ProcessEnvPriority", func(t *testing.T) {
		ClearCache()
		t.Setenv("TEST_GOPROXY_VAR", "https://custom.proxy.org")
		got := Get("TEST_GOPROXY_VAR")
		if got != "https://custom.proxy.org" {
			t.Errorf("Get() = %q, want %q", got, "https://custom.proxy.org")
		}
	})

	t.Run("FallbackAndCaching", func(t *testing.T) {
		ClearCache()
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
		val1 := Get("TEST_UNSET_VAR")
		if val1 != "from-go-env" {
			t.Errorf("First Get() = %q, want %q", val1, "from-go-env")
		}
		if calls != 1 {
			t.Errorf("execGoEnv call count = %d, want 1", calls)
		}

		// Second lookup should use cache
		val2 := Get("TEST_UNSET_VAR")
		if val2 != "from-go-env" {
			t.Errorf("Second Get() = %q, want %q", val2, "from-go-env")
		}
		if calls != 1 {
			t.Errorf("execGoEnv call count after cache hit = %d, want 1", calls)
		}
	})

	t.Run("ErrorFallback", func(t *testing.T) {
		ClearCache()
		origExec := execGoEnv
		defer func() { execGoEnv = origExec }()

		execGoEnv = func(key string) (string, error) {
			return "", errors.New("mock error")
		}

		val := Get("TEST_ERR_VAR")
		if val != "" {
			t.Errorf("Get() on error = %q, want %q", val, "")
		}
	})
}

func TestIsValidGoEnvKey(t *testing.T) {
	validKeys := []string{"GOPROXY", "GONOPROXY", "GOPRIVATE", "GITHUB_TOKEN", "FOO_BAR_123"}
	for _, k := range validKeys {
		if !isValidGoEnvKey(k) {
			t.Errorf("isValidGoEnvKey(%q) = false, want true", k)
		}
	}

	invalidKeys := []string{"", "-json", "--help", "FOO-BAR", "FOO;BAR", "FOO|BAR", "FOO BAR"}
	for _, k := range invalidKeys {
		if isValidGoEnvKey(k) {
			t.Errorf("isValidGoEnvKey(%q) = true, want false", k)
		}
	}
}
