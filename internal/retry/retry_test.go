package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_SuccessFirstTry(t *testing.T) {
	t.Parallel()
	calls := 0
	val, err := Do(context.Background(), 3, 10*time.Millisecond, func() (string, error) {
		calls++
		return "success", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "success" {
		t.Errorf("got %q, want %q", val, "success")
	}
	if calls != 1 {
		t.Errorf("call count = %d, want 1", calls)
	}
}

func TestDo_RetryThenSuccess(t *testing.T) {
	t.Parallel()
	calls := 0
	val, err := Do(context.Background(), 3, 10*time.Millisecond, func() (int, error) {
		calls++
		if calls < 2 {
			return 0, errors.New("transient error")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("got %d, want 42", val)
	}
	if calls != 2 {
		t.Errorf("call count = %d, want 2", calls)
	}
}

func TestDo_RetryBackoffDelay(t *testing.T) {
	t.Parallel()
	start := time.Now()
	calls := 0
	interval := 20 * time.Millisecond
	_, _ = Do(context.Background(), 2, interval, func() (int, error) {
		calls++
		if calls < 2 {
			return 0, errors.New("fail once")
		}
		return 1, nil
	})
	elapsed := time.Since(start)
	if elapsed < interval/2 {
		t.Errorf("expected backoff delay around %v, elapsed = %v", interval, elapsed)
	}
}

func TestDo_PermanentError(t *testing.T) {
	t.Parallel()
	calls := 0
	permErr := errors.New("fatal")
	_, err := Do(context.Background(), 5, 10*time.Millisecond, func() (string, error) {
		calls++
		return "", Permanent(permErr)
	})
	if !errors.Is(err, permErr) {
		t.Fatalf("expected permanent error %v, got %v", permErr, err)
	}
	if calls != 1 {
		t.Errorf("call count = %d, want 1 (should not retry permanent errors)", calls)
	}
}

func TestDo_MaxTriesExceeded(t *testing.T) {
	t.Parallel()
	calls := 0
	_, err := Do(context.Background(), 3, 5*time.Millisecond, func() (string, error) {
		calls++
		return "", errors.New("transient")
	})
	if err == nil {
		t.Fatal("expected error after max tries exceeded, got nil")
	}
	if calls != 3 {
		t.Errorf("call count = %d, want 3", calls)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := Do(ctx, 5, 50*time.Millisecond, func() (string, error) {
		calls++
		cancel()
		return "", errors.New("retryable error")
	})
	if err == nil {
		t.Fatal("expected error due to cancelled context, got nil")
	}
}

func BenchmarkRetryDo_PermanentError(b *testing.B) {
	ctx := context.Background()
	permErr := Permanent(errors.New("not found"))
	op := func() (string, error) {
		return "", permErr
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = Do(ctx, 3, time.Millisecond, op)
	}
}
