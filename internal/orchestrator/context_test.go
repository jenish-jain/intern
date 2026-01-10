package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestCheckContext_NotCancelled(t *testing.T) {
	ctx := context.Background()
	err := checkContext(ctx, "TEST-123", "test checkpoint")
	if err != nil {
		t.Errorf("checkContext() returned error for non-cancelled context: %v", err)
	}
}

func TestCheckContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := checkContext(ctx, "TEST-123", "test checkpoint")
	if err == nil {
		t.Error("checkContext() should return error for cancelled context")
	}

	// Should contain checkpoint name
	if err != nil && err.Error() == "" {
		t.Error("Error message should not be empty")
	}
}

func TestCheckContext_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for timeout
	time.Sleep(10 * time.Millisecond)

	err := checkContext(ctx, "TEST-123", "test checkpoint")
	if err == nil {
		t.Error("checkContext() should return error for timed out context")
	}
}

func TestCheckContext_MultipleCheckpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First checkpoint should pass
	if err := checkContext(ctx, "TEST-123", "checkpoint 1"); err != nil {
		t.Errorf("First checkpoint failed: %v", err)
	}

	// Second checkpoint should pass
	if err := checkContext(ctx, "TEST-123", "checkpoint 2"); err != nil {
		t.Errorf("Second checkpoint failed: %v", err)
	}

	// Cancel context
	cancel()

	// Third checkpoint should fail
	if err := checkContext(ctx, "TEST-123", "checkpoint 3"); err == nil {
		t.Error("Third checkpoint should fail after cancellation")
	}
}

func TestCheckContext_WithDeadline(t *testing.T) {
	deadline := time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	// Should pass before deadline
	if err := checkContext(ctx, "TEST-123", "before deadline"); err != nil {
		t.Errorf("Checkpoint before deadline failed: %v", err)
	}

	// Wait for deadline to pass
	time.Sleep(100 * time.Millisecond)

	// Should fail after deadline
	if err := checkContext(ctx, "TEST-123", "after deadline"); err == nil {
		t.Error("Checkpoint should fail after deadline")
	}
}

// Benchmark context checking overhead
func BenchmarkCheckContext(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_ = checkContext(ctx, "BENCH-123", "benchmark")
	}
}

func BenchmarkCheckContext_Cancelled(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < b.N; i++ {
		_ = checkContext(ctx, "BENCH-123", "benchmark")
	}
}
