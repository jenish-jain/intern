package logger

import (
	"context"
	"time"

	"github.com/jenish-jain/logger"
)

// TraceOperation executes a function while measuring and logging its duration
// This is useful for tracking performance of individual operations
func TraceOperation(ctx context.Context, operation string, fn func() error) error {
	start := time.Now()
	Info(ctx, "Starting operation", "operation", operation)

	err := fn()
	duration := time.Since(start)

	if err != nil {
		Error(ctx, "Operation failed",
			"operation", operation,
			"duration", duration.Round(time.Millisecond).String(),
			"error", err,
		)
	} else {
		Info(ctx, "Operation complete",
			"operation", operation,
			"duration", duration.Round(time.Millisecond).String(),
		)
	}

	return err
}

// TraceOperationWithResult executes a function that returns a result and error,
// while measuring and logging its duration
func TraceOperationWithResult[T any](ctx context.Context, operation string, fn func() (T, error)) (T, error) {
	start := time.Now()
	Info(ctx, "Starting operation", "operation", operation)

	result, err := fn()
	duration := time.Since(start)

	if err != nil {
		Error(ctx, "Operation failed",
			"operation", operation,
			"duration", duration.Round(time.Millisecond).String(),
			"error", err,
		)
	} else {
		Info(ctx, "Operation complete",
			"operation", operation,
			"duration", duration.Round(time.Millisecond).String(),
		)
	}

	return result, err
}

// Span represents a performance span for measuring operation duration
type Span struct {
	ctx       context.Context
	operation string
	startTime time.Time
	fields    []interface{}
}

// StartSpan begins a new performance span
func StartSpan(ctx context.Context, operation string, fields ...interface{}) *Span {
	span := &Span{
		ctx:       ctx,
		operation: operation,
		startTime: time.Now(),
		fields:    fields,
	}

	Info(ctx, "Span started", append([]interface{}{"operation", operation}, fields...)...)
	return span
}

// End finishes the span and logs the duration
func (s *Span) End(err error) {
	duration := time.Since(s.startTime)

	fields := append([]interface{}{
		"operation", s.operation,
		"duration", duration.Round(time.Millisecond).String(),
	}, s.fields...)

	if err != nil {
		fields = append(fields, "error", err)
		Error(s.ctx, "Span failed", fields...)
	} else {
		Info(s.ctx, "Span complete", fields...)
	}
}

// AddField adds additional context to the span
func (s *Span) AddField(key string, value interface{}) {
	s.fields = append(s.fields, key, value)
}

// LogPhase logs an intermediate phase within the span
func (s *Span) LogPhase(phase string, fields ...interface{}) {
	elapsed := time.Since(s.startTime)
	allFields := append([]interface{}{
		"operation", s.operation,
		"phase", phase,
		"elapsed", elapsed.Round(time.Millisecond).String(),
	}, fields...)
	allFields = append(allFields, s.fields...)

	Info(s.ctx, "Span phase", allFields...)
}

// LogMetric logs a metric value within the span
func LogMetric(ctx context.Context, metric string, value interface{}, fields ...interface{}) {
	allFields := append([]interface{}{"metric", metric, "value", value}, fields...)
	logger.Info("Metric logged", buildFields(ctx, allFields)...)
}
