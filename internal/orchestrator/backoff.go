package orchestrator

import (
	"context"
	"math"
	"math/rand"
	"time"
)

type BackoffConfig struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Jitter     float64 // 0..1
	MaxRetries int
}

func (b BackoffConfig) next(attempt int) time.Duration {
	base := float64(b.Initial) * math.Pow(b.Multiplier, float64(attempt))
	if base > float64(b.Max) {
		base = float64(b.Max)
	}
	j := 1 + (rand.Float64()*2-1)*b.Jitter // 1±Jitter
	return time.Duration(base * j)
}

// Retry runs op with backoff on transient errors until MaxRetries or context cancel.
// Returns the number of retry attempts performed.
func Retry(ctx context.Context, cfg BackoffConfig, op func() error) (err error, attempts int) {
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			attempts++
		}
		err = op()
		if err == nil || IsPermanent(err) {
			return err, attempts
		}
		if !IsTransient(err) {
			return err, attempts
		}
		d := cfg.next(attempt)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err(), attempts
		}
	}
	return err, attempts
}
