package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/jenish-jain/logger"
)

type contextKey string

const ticketContextKey contextKey = "ticket_context"

// TicketContext contains contextual information for ticket processing
// This is attached to the context.Context and used for correlation in logs
type TicketContext struct {
	TicketKey     string        // JIRA ticket key (e.g., "PROJ-123")
	CorrelationID string        // Unique ID for this ticket processing session
	StartTime     time.Time     // When ticket processing started
	Provider      string        // AI provider being used (anthropic, ollama)
	Model         string        // AI model being used
	Branch        string        // Git branch created for this ticket
}

// WithTicketContext creates a new context with ticket information attached
func WithTicketContext(ctx context.Context, ticketKey string) context.Context {
	tctx := &TicketContext{
		TicketKey:     ticketKey,
		CorrelationID: generateCorrelationID(),
		StartTime:     time.Now(),
	}
	return context.WithValue(ctx, ticketContextKey, tctx)
}

// GetTicketContext retrieves ticket context from the context
func GetTicketContext(ctx context.Context) *TicketContext {
	if ctx == nil {
		return nil
	}
	tctx, ok := ctx.Value(ticketContextKey).(*TicketContext)
	if !ok {
		return nil
	}
	return tctx
}

// UpdateTicketContext updates fields in the ticket context
func UpdateTicketContext(ctx context.Context, updater func(*TicketContext)) {
	tctx := GetTicketContext(ctx)
	if tctx != nil {
		updater(tctx)
	}
}

// generateCorrelationID generates a unique correlation ID for tracking
func generateCorrelationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if random generation fails
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405")))
	}
	return hex.EncodeToString(b)
}

// buildFields creates a field slice with ticket context information
func buildFields(ctx context.Context, fields []interface{}) []interface{} {
	tctx := GetTicketContext(ctx)
	if tctx == nil {
		return fields
	}

	// Calculate elapsed time
	elapsed := time.Since(tctx.StartTime)

	// Prepend ticket context fields
	contextFields := []interface{}{
		"ticket", tctx.TicketKey,
		"correlation_id", tctx.CorrelationID,
		"elapsed", elapsed.Round(time.Millisecond).String(),
	}

	// Add optional fields if set
	if tctx.Provider != "" {
		contextFields = append(contextFields, "provider", tctx.Provider)
	}
	if tctx.Model != "" {
		contextFields = append(contextFields, "model", tctx.Model)
	}
	if tctx.Branch != "" {
		contextFields = append(contextFields, "branch", tctx.Branch)
	}

	// Combine with provided fields
	return append(contextFields, fields...)
}

// Debug logs a debug message with ticket context
func Debug(ctx context.Context, msg string, fields ...interface{}) {
	logger.Debug(msg, buildFields(ctx, fields)...)
}

// Info logs an info message with ticket context
func Info(ctx context.Context, msg string, fields ...interface{}) {
	logger.Info(msg, buildFields(ctx, fields)...)
}

// Warn logs a warning message with ticket context
func Warn(ctx context.Context, msg string, fields ...interface{}) {
	logger.Warn(msg, buildFields(ctx, fields)...)
}

// Error logs an error message with ticket context
func Error(ctx context.Context, msg string, fields ...interface{}) {
	logger.Error(msg, buildFields(ctx, fields)...)
}
