// Package retry provides generic retry mechanisms using exponential backoff.
package retry

import (
	"context"
	"errors"
	"time"
)

type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

// Permanent wraps err in a permanentError indicating it should not be retried.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// Do executes op and retries on retryable errors using exponential backoff up to maxTries total attempts.
func Do[T any](ctx context.Context, maxTries uint, initialInterval time.Duration, op func() (T, error)) (T, error) {
	var zero T
	if maxTries == 0 {
		maxTries = 1
	}

	interval := initialInterval
	for attempt := uint(1); attempt <= maxTries; attempt++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}

		val, err := op()
		if err == nil {
			return val, nil
		}

		if perm, ok := err.(permanentError); ok {
			return zero, perm.err
		}
		var perm permanentError
		if errors.As(err, &perm) {
			return zero, perm.err
		}

		if attempt == maxTries {
			return zero, err
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}

		interval *= 2
	}

	return zero, ctx.Err()
}
