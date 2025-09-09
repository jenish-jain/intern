package orchestrator

import "sync/atomic"

type Metrics struct {
	ticketsProcessed int64
	prsCreated       int64
	retries          int64
	aiPlanFailures   int64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) IncTicketsProcessed() { atomic.AddInt64(&m.ticketsProcessed, 1) }
func (m *Metrics) IncPRsCreated()       { atomic.AddInt64(&m.prsCreated, 1) }
func (m *Metrics) AddRetries(n int) {
	if n > 0 {
		atomic.AddInt64(&m.retries, int64(n))
	}
}
func (m *Metrics) IncAIPlanFailures() { atomic.AddInt64(&m.aiPlanFailures, 1) }

type MetricsSnapshot struct {
	TicketsProcessed int64
	PRsCreated       int64
	Retries          int64
	AIPlanFailures   int64
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		TicketsProcessed: atomic.LoadInt64(&m.ticketsProcessed),
		PRsCreated:       atomic.LoadInt64(&m.prsCreated),
		Retries:          atomic.LoadInt64(&m.retries),
		AIPlanFailures:   atomic.LoadInt64(&m.aiPlanFailures),
	}
}
