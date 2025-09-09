package orchestrator

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_BasicOperations(t *testing.T) {
	m := NewMetrics()

	// Initial state
	snapshot := m.Snapshot()
	assert.Equal(t, int64(0), snapshot.TicketsProcessed)
	assert.Equal(t, int64(0), snapshot.PRsCreated)
	assert.Equal(t, int64(0), snapshot.Retries)
	assert.Equal(t, int64(0), snapshot.AIPlanFailures)

	// Increment operations
	m.IncTicketsProcessed()
	m.IncPRsCreated()
	m.AddRetries(3)
	m.IncAIPlanFailures()

	snapshot = m.Snapshot()
	assert.Equal(t, int64(1), snapshot.TicketsProcessed)
	assert.Equal(t, int64(1), snapshot.PRsCreated)
	assert.Equal(t, int64(3), snapshot.Retries)
	assert.Equal(t, int64(1), snapshot.AIPlanFailures)

	// More increments
	m.IncTicketsProcessed()
	m.IncTicketsProcessed()
	m.IncPRsCreated()
	m.AddRetries(2)
	m.AddRetries(1)
	m.IncAIPlanFailures()

	snapshot = m.Snapshot()
	assert.Equal(t, int64(3), snapshot.TicketsProcessed)
	assert.Equal(t, int64(2), snapshot.PRsCreated)
	assert.Equal(t, int64(6), snapshot.Retries)
	assert.Equal(t, int64(2), snapshot.AIPlanFailures)
}

func TestMetrics_AddRetriesZeroOrNegative(t *testing.T) {
	m := NewMetrics()

	m.AddRetries(0)
	m.AddRetries(-1)

	snapshot := m.Snapshot()
	assert.Equal(t, int64(0), snapshot.Retries)

	m.AddRetries(5)
	snapshot = m.Snapshot()
	assert.Equal(t, int64(5), snapshot.Retries)
}

func TestMetrics_ThreadSafety(t *testing.T) {
	m := NewMetrics()
	const goroutines = 10
	const increments = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Launch multiple goroutines that increment counters
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				m.IncTicketsProcessed()
				m.IncPRsCreated()
				m.AddRetries(1)
				m.IncAIPlanFailures()
			}
		}()
	}

	wg.Wait()

	snapshot := m.Snapshot()
	expected := int64(goroutines * increments)
	assert.Equal(t, expected, snapshot.TicketsProcessed)
	assert.Equal(t, expected, snapshot.PRsCreated)
	assert.Equal(t, expected, snapshot.Retries)
	assert.Equal(t, expected, snapshot.AIPlanFailures)
}

func TestMetrics_SnapshotConsistency(t *testing.T) {
	m := NewMetrics()

	// Increment some counters
	m.IncTicketsProcessed()
	m.IncPRsCreated()
	m.AddRetries(5)

	// Take multiple snapshots
	snapshot1 := m.Snapshot()
	snapshot2 := m.Snapshot()

	// Snapshots should be identical
	assert.Equal(t, snapshot1.TicketsProcessed, snapshot2.TicketsProcessed)
	assert.Equal(t, snapshot1.PRsCreated, snapshot2.PRsCreated)
	assert.Equal(t, snapshot1.Retries, snapshot2.Retries)
	assert.Equal(t, snapshot1.AIPlanFailures, snapshot2.AIPlanFailures)

	// Modify metrics
	m.IncTicketsProcessed()

	// New snapshot should reflect changes
	snapshot3 := m.Snapshot()
	assert.Equal(t, snapshot1.TicketsProcessed+1, snapshot3.TicketsProcessed)
	assert.Equal(t, snapshot1.PRsCreated, snapshot3.PRsCreated)
	assert.Equal(t, snapshot1.Retries, snapshot3.Retries)
	assert.Equal(t, snapshot1.AIPlanFailures, snapshot3.AIPlanFailures)
}
