package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry_Success(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    10 * time.Millisecond,
		Max:        100 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0.1,
		MaxRetries: 3,
	}

	callCount := 0
	op := func() error {
		callCount++
		return nil // succeed on first try
	}

	err, attempts := Retry(context.Background(), cfg, op)
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
	assert.Equal(t, 0, attempts) // no retries needed
}

func TestRetry_TransientError_EventualSuccess(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    1 * time.Millisecond,
		Max:        10 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0,
		MaxRetries: 3,
	}

	callCount := 0
	op := func() error {
		callCount++
		if callCount < 3 {
			return MakeTransient(errors.New("temporary failure"))
		}
		return nil // succeed on third try
	}

	start := time.Now()
	err, attempts := Retry(context.Background(), cfg, op)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
	assert.Equal(t, 2, attempts) // 2 retries
	// Should have taken some time due to backoff
	assert.Greater(t, duration, 1*time.Millisecond)
}

func TestRetry_PermanentError_NoRetry(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    10 * time.Millisecond,
		Max:        100 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0,
		MaxRetries: 3,
	}

	callCount := 0
	permanentErr := MakePermanent(errors.New("permanent failure"))
	op := func() error {
		callCount++
		return permanentErr
	}

	err, attempts := Retry(context.Background(), cfg, op)
	assert.Error(t, err)
	assert.True(t, IsPermanent(err))
	assert.Equal(t, 1, callCount) // should not retry
	assert.Equal(t, 0, attempts)
}

func TestRetry_NonClassifiedError_NoRetry(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    10 * time.Millisecond,
		Max:        100 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0,
		MaxRetries: 3,
	}

	callCount := 0
	regularErr := errors.New("regular error")
	op := func() error {
		callCount++
		return regularErr
	}

	err, attempts := Retry(context.Background(), cfg, op)
	assert.Error(t, err)
	assert.Equal(t, regularErr, err)
	assert.Equal(t, 1, callCount) // should not retry
	assert.Equal(t, 0, attempts)
}

func TestRetry_MaxRetriesExceeded(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    1 * time.Millisecond,
		Max:        5 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0,
		MaxRetries: 2,
	}

	callCount := 0
	op := func() error {
		callCount++
		return MakeTransient(errors.New("always fails"))
	}

	err, attempts := Retry(context.Background(), cfg, op)
	assert.Error(t, err)
	assert.True(t, IsTransient(err))
	assert.Equal(t, 3, callCount) // initial + 2 retries
	assert.Equal(t, 2, attempts)
}

func TestRetry_ContextCanceled(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    100 * time.Millisecond, // long delay
		Max:        1 * time.Second,
		Multiplier: 2,
		Jitter:     0,
		MaxRetries: 5,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	callCount := 0
	op := func() error {
		callCount++
		return MakeTransient(errors.New("always fails"))
	}

	err, attempts := Retry(ctx, cfg, op)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.Equal(t, 1, callCount) // should fail fast on context cancel
	assert.Equal(t, 0, attempts)  // no retries because context canceled
}

func TestBackoffConfig_Next(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    10 * time.Millisecond,
		Max:        100 * time.Millisecond,
		Multiplier: 2,
		Jitter:     0, // no jitter for predictable testing
		MaxRetries: 3,
	}

	// Test exponential backoff
	assert.Equal(t, 10*time.Millisecond, cfg.next(0))
	assert.Equal(t, 20*time.Millisecond, cfg.next(1))
	assert.Equal(t, 40*time.Millisecond, cfg.next(2))
	assert.Equal(t, 80*time.Millisecond, cfg.next(3))
	assert.Equal(t, 100*time.Millisecond, cfg.next(4))  // capped at Max
	assert.Equal(t, 100*time.Millisecond, cfg.next(10)) // still capped
}

func TestBackoffConfig_NextWithJitter(t *testing.T) {
	cfg := BackoffConfig{
		Initial:    100 * time.Millisecond,
		Max:        1 * time.Second,
		Multiplier: 2,
		Jitter:     0.5, // 50% jitter
		MaxRetries: 3,
	}

	// With jitter, results should vary but be within expected range
	for i := 0; i < 10; i++ {
		duration := cfg.next(0)
		// With 50% jitter, should be between 50ms and 150ms
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
		assert.LessOrEqual(t, duration, 150*time.Millisecond)
	}
}
