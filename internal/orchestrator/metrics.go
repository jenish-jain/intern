package orchestrator

import (
	"math"
	"sync/atomic"
	"time"
)

// Metrics tracks aggregate statistics for all tickets processed during the agent's run.
// All fields use atomic operations for thread-safe concurrent access.
type Metrics struct {
	// Ticket processing metrics
	ticketsProcessed int64
	prsCreated       int64
	retries          int64
	aiPlanFailures   int64
	ticketsFailed    int64

	// Cost tracking (using micro-dollars to avoid float64 atomics)
	totalInputTokens     int64
	totalOutputTokens    int64
	totalCostMicroDollar int64 // Cost in micro-dollars (multiply by 0.000001 for dollars)

	// Context strategy tracking
	smartContextUsed  int64
	simpleContextUsed int64

	// Performance tracking (using milliseconds for atomic operations)
	totalExecutionTimeMs int64
	totalFilesChanged    int64

	// Timing
	startTime time.Time
}

// NewMetrics creates a new metrics tracker with the start time initialized.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// Basic counters
func (m *Metrics) IncTicketsProcessed() { atomic.AddInt64(&m.ticketsProcessed, 1) }
func (m *Metrics) IncPRsCreated()       { atomic.AddInt64(&m.prsCreated, 1) }
func (m *Metrics) IncTicketsFailed()    { atomic.AddInt64(&m.ticketsFailed, 1) }
func (m *Metrics) AddRetries(n int) {
	if n > 0 {
		atomic.AddInt64(&m.retries, int64(n))
	}
}
func (m *Metrics) IncAIPlanFailures() { atomic.AddInt64(&m.aiPlanFailures, 1) }

// Cost tracking
func (m *Metrics) AddTokenUsage(inputTokens, outputTokens int, cost float64) {
	atomic.AddInt64(&m.totalInputTokens, int64(inputTokens))
	atomic.AddInt64(&m.totalOutputTokens, int64(outputTokens))
	// Store cost in micro-dollars to use int64 atomic operations with sufficient precision
	// Use math.Round to avoid truncation errors
	atomic.AddInt64(&m.totalCostMicroDollar, int64(math.Round(cost*1000000)))
}

// Context strategy tracking
func (m *Metrics) IncSmartContextUsed()  { atomic.AddInt64(&m.smartContextUsed, 1) }
func (m *Metrics) IncSimpleContextUsed() { atomic.AddInt64(&m.simpleContextUsed, 1) }

// Performance tracking
func (m *Metrics) AddExecutionTime(d time.Duration) {
	atomic.AddInt64(&m.totalExecutionTimeMs, d.Milliseconds())
}
func (m *Metrics) AddFilesChanged(n int) {
	atomic.AddInt64(&m.totalFilesChanged, int64(n))
}

// MetricsSnapshot is a point-in-time snapshot of all metrics.
// Safe to access without locks.
type MetricsSnapshot struct {
	// Ticket processing
	TicketsProcessed int64
	PRsCreated       int64
	TicketsFailed    int64
	Retries          int64
	AIPlanFailures   int64

	// Cost tracking
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCost         float64 // In dollars

	// Context strategy
	SmartContextUsed  int64
	SimpleContextUsed int64

	// Performance
	TotalExecutionTime time.Duration
	AvgExecutionTime   time.Duration
	TotalFilesChanged  int64
	AvgFilesPerTicket  float64

	// Cost averages
	AvgCostPerTicket         float64
	AvgInputTokensPerTicket  float64
	AvgOutputTokensPerTicket float64

	// Runtime
	TotalRuntime time.Duration
}

// Snapshot creates a consistent point-in-time snapshot of all metrics.
// Calculates derived values like averages.
func (m *Metrics) Snapshot() MetricsSnapshot {
	ticketsProcessed := atomic.LoadInt64(&m.ticketsProcessed)
	totalCostMicroDollar := atomic.LoadInt64(&m.totalCostMicroDollar)
	totalInputTokens := atomic.LoadInt64(&m.totalInputTokens)
	totalOutputTokens := atomic.LoadInt64(&m.totalOutputTokens)
	totalExecutionTimeMs := atomic.LoadInt64(&m.totalExecutionTimeMs)
	totalFilesChanged := atomic.LoadInt64(&m.totalFilesChanged)

	totalCost := float64(totalCostMicroDollar) / 1000000.0
	totalExecutionTime := time.Duration(totalExecutionTimeMs) * time.Millisecond

	// Calculate averages (avoid division by zero)
	var avgCost, avgInput, avgOutput, avgFiles float64
	var avgExecTime time.Duration
	if ticketsProcessed > 0 {
		avgCost = totalCost / float64(ticketsProcessed)
		avgInput = float64(totalInputTokens) / float64(ticketsProcessed)
		avgOutput = float64(totalOutputTokens) / float64(ticketsProcessed)
		avgFiles = float64(totalFilesChanged) / float64(ticketsProcessed)
		avgExecTime = totalExecutionTime / time.Duration(ticketsProcessed)
	}

	return MetricsSnapshot{
		TicketsProcessed:  ticketsProcessed,
		PRsCreated:        atomic.LoadInt64(&m.prsCreated),
		TicketsFailed:     atomic.LoadInt64(&m.ticketsFailed),
		Retries:           atomic.LoadInt64(&m.retries),
		AIPlanFailures:    atomic.LoadInt64(&m.aiPlanFailures),
		TotalInputTokens:  totalInputTokens,
		TotalOutputTokens: totalOutputTokens,
		TotalCost:         totalCost,
		SmartContextUsed:  atomic.LoadInt64(&m.smartContextUsed),
		SimpleContextUsed: atomic.LoadInt64(&m.simpleContextUsed),
		TotalExecutionTime: totalExecutionTime,
		AvgExecutionTime:   avgExecTime,
		TotalFilesChanged:  totalFilesChanged,
		AvgFilesPerTicket:  avgFiles,
		AvgCostPerTicket:   avgCost,
		AvgInputTokensPerTicket:  avgInput,
		AvgOutputTokensPerTicket: avgOutput,
		TotalRuntime: time.Since(m.startTime),
	}
}
