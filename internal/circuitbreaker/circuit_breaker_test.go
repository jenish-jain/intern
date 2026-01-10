package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNew_DefaultConfig(t *testing.T) {
	cb := New(Config{})
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED, got %v", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("Expected 0 failures, got %d", cb.Failures())
	}
}

func TestNew_CustomConfig(t *testing.T) {
	cfg := Config{
		MaxFailures: 3,
		Timeout:     10 * time.Second,
	}
	cb := New(cfg)

	if cb.config.MaxFailures != 3 {
		t.Errorf("Expected MaxFailures=3, got %d", cb.config.MaxFailures)
	}
}

func TestCircuitBreaker_SuccessfulRequest(t *testing.T) {
	cb := New(DefaultConfig())

	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED, got %v", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("Expected 0 failures, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_FailedRequest(t *testing.T) {
	cb := New(DefaultConfig())
	testErr := errors.New("test error")

	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return testErr
	})

	if err != testErr {
		t.Errorf("Expected test error, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED (not enough failures), got %v", cb.State())
	}
	if cb.Failures() != 1 {
		t.Errorf("Expected 1 failure, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_OpenAfterMaxFailures(t *testing.T) {
	cfg := Config{
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Execute 3 failing requests
	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state OPEN after 3 failures, got %v", cb.State())
	}
	if cb.Failures() != 3 {
		t.Errorf("Expected 3 failures, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_OpenCircuitFailsFast(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:     10 * time.Second, // Long timeout to keep it open
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Next request should fail fast
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})

	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_TransitionToHalfOpen(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond, // Short timeout
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Next request should transition to half-open
	executed := false
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		executed = true
		return nil
	})

	if err != nil {
		t.Errorf("Expected nil error in half-open state, got %v", err)
	}
	if !executed {
		t.Error("Function should have been executed in half-open state")
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED after successful half-open request, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	cfg := Config{
		MaxFailures:           2,
		Timeout:               50 * time.Millisecond,
		MaxConcurrentHalfOpen: 1,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Successful request in half-open should close the circuit
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED, got %v", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("Expected failures reset to 0, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cfg := Config{
		MaxFailures:           2,
		Timeout:               50 * time.Millisecond,
		MaxConcurrentHalfOpen: 1,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Failed request in half-open should reopen the circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return testErr
	})

	if cb.State() != StateOpen {
		t.Errorf("Expected state OPEN after half-open failure, got %v", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cfg := Config{
		MaxFailures: 5,
		Timeout:     1 * time.Second,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.Failures() != 3 {
		t.Fatalf("Expected 3 failures, got %d", cb.Failures())
	}

	// 1 success
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if cb.Failures() != 0 {
		t.Errorf("Expected failures reset to 0 after success, got %d", cb.Failures())
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED, got %v", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := Config{
		MaxFailures: 2,
		Timeout:     10 * time.Second,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Reset
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("Expected state CLOSED after reset, got %v", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", cb.Failures())
	}

	// Should allow requests after reset
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected nil error after reset, got %v", err)
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	transitions := []struct{ from, to State }{}

	cfg := Config{
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
		OnStateChange: func(from, to State) {
			transitions = append(transitions, struct{ from, to State }{from, to})
		},
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Wait for callback
	time.Sleep(10 * time.Millisecond)

	if len(transitions) != 1 {
		t.Fatalf("Expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].from != StateClosed || transitions[0].to != StateOpen {
		t.Errorf("Expected CLOSED->OPEN, got %v->%v", transitions[0].from, transitions[0].to)
	}

	// Wait for timeout and make successful request
	time.Sleep(100 * time.Millisecond)
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})
	time.Sleep(10 * time.Millisecond)

	if len(transitions) < 2 {
		t.Fatalf("Expected at least 2 transitions, got %d", len(transitions))
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := New(DefaultConfig())
	testErr := errors.New("test error")

	// Execute failing request
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return testErr
	})

	stats := cb.Stats()
	if stats.State != StateClosed {
		t.Errorf("Expected state CLOSED, got %v", stats.State)
	}
	if stats.Failures != 1 {
		t.Errorf("Expected 1 failure, got %d", stats.Failures)
	}
	if stats.LastFailureTime.IsZero() {
		t.Error("Expected LastFailureTime to be set")
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "CLOSED"},
		{StateOpen, "OPEN"},
		{StateHalfOpen, "HALF_OPEN"},
		{State(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("Expected %s, got %s", tt.expected, tt.state.String())
		}
	}
}

func TestCircuitBreaker_TooManyHalfOpenRequests(t *testing.T) {
	cfg := Config{
		MaxFailures:           2,
		Timeout:               50 * time.Millisecond,
		MaxConcurrentHalfOpen: 1,
	}
	cb := New(cfg)
	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Wait for timeout to enter half-open
	time.Sleep(100 * time.Millisecond)

	// Manually set state to half-open and increment counter
	cb.mu.Lock()
	cb.state = StateHalfOpen
	cb.halfOpenRequests = 1 // Simulate one request in flight
	cb.mu.Unlock()

	// Second request should fail with too many requests
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		t.Error("Should not execute when half-open limit reached")
		return nil
	})

	if err != ErrTooManyRequests {
		t.Errorf("Expected ErrTooManyRequests, got %v", err)
	}
}

// Benchmark circuit breaker overhead
func BenchmarkCircuitBreaker_Success(b *testing.B) {
	cb := New(DefaultConfig())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
	}
}

func BenchmarkCircuitBreaker_Failure(b *testing.B) {
	cb := New(DefaultConfig())
	ctx := context.Background()
	testErr := errors.New("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Execute(ctx, func(ctx context.Context) error {
			return testErr
		})
	}
}
