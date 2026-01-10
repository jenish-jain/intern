package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State represents the current state of the circuit breaker
type State int

const (
	StateClosed   State = iota // Normal operation, requests allowed
	StateOpen                   // Circuit open, requests fail fast
	StateHalfOpen               // Testing if service recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

var (
	// ErrCircuitOpen is returned when the circuit breaker is open
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrTooManyRequests is returned when too many requests are made in half-open state
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// Config holds circuit breaker configuration
type Config struct {
	// MaxFailures is the number of consecutive failures before opening the circuit
	MaxFailures uint32
	// Timeout is how long to wait before transitioning from Open to HalfOpen
	Timeout time.Duration
	// MaxConcurrentHalfOpen is max concurrent requests allowed in half-open state
	MaxConcurrentHalfOpen uint32
	// OnStateChange is called when the circuit breaker state changes
	OnStateChange func(from, to State)
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		MaxFailures:           5,
		Timeout:               30 * time.Second,
		MaxConcurrentHalfOpen: 1,
		OnStateChange:         func(from, to State) {},
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu                    sync.RWMutex
	state                 State
	failures              uint32
	lastFailureTime       time.Time
	lastStateChangeTime   time.Time
	halfOpenRequests      uint32
	config                Config
}

// New creates a new circuit breaker with the given configuration
func New(config Config) *CircuitBreaker {
	if config.MaxFailures == 0 {
		config = DefaultConfig()
	}
	if config.OnStateChange == nil {
		config.OnStateChange = func(from, to State) {}
	}
	return &CircuitBreaker{
		state:               StateClosed,
		config:              config,
		lastStateChangeTime: time.Now(),
	}
}

// Execute runs the given function if the circuit breaker allows it
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	// Check if circuit allows execution
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// Execute the function
	err := fn(ctx)

	// Record result
	cb.afterRequest(err)

	return err
}

// beforeRequest checks if the request should be allowed
func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Normal operation - allow request
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailureTime) > cb.config.Timeout {
			// Transition to half-open
			cb.setState(StateHalfOpen)
			cb.halfOpenRequests = 0
			return nil
		}
		// Still open - fail fast
		return ErrCircuitOpen

	case StateHalfOpen:
		// Allow limited concurrent requests
		if cb.halfOpenRequests >= cb.config.MaxConcurrentHalfOpen {
			return ErrTooManyRequests
		}
		cb.halfOpenRequests++
		return nil

	default:
		return ErrCircuitOpen
	}
}

// afterRequest records the result of the request
func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		// Request failed
		cb.failures++
		cb.lastFailureTime = time.Now()

		if cb.state == StateHalfOpen {
			// Half-open test failed - reopen circuit
			cb.setState(StateOpen)
			cb.halfOpenRequests = 0
		} else if cb.failures >= cb.config.MaxFailures {
			// Too many failures - open circuit
			cb.setState(StateOpen)
		}
	} else {
		// Request succeeded
		if cb.state == StateHalfOpen {
			// Half-open test succeeded - close circuit
			cb.setState(StateClosed)
			cb.failures = 0
			cb.halfOpenRequests = 0
		} else {
			// Reset failure count on success
			cb.failures = 0
		}
	}
}

// setState changes the circuit breaker state and calls the callback
func (cb *CircuitBreaker) setState(newState State) {
	oldState := cb.state
	if oldState == newState {
		return
	}

	cb.state = newState
	cb.lastStateChangeTime = time.Now()

	// Call callback without holding lock to prevent deadlocks
	go cb.config.OnStateChange(oldState, newState)
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Failures returns the current failure count
func (cb *CircuitBreaker) Failures() uint32 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := cb.state
	cb.state = StateClosed
	cb.failures = 0
	cb.halfOpenRequests = 0
	cb.lastStateChangeTime = time.Now()

	if oldState != StateClosed {
		go cb.config.OnStateChange(oldState, StateClosed)
	}
}

// Stats returns current circuit breaker statistics
func (cb *CircuitBreaker) Stats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		State:               cb.state,
		Failures:            cb.failures,
		LastFailureTime:     cb.lastFailureTime,
		LastStateChangeTime: cb.lastStateChangeTime,
		HalfOpenRequests:    cb.halfOpenRequests,
	}
}

// Stats holds circuit breaker statistics
type Stats struct {
	State               State
	Failures            uint32
	LastFailureTime     time.Time
	LastStateChangeTime time.Time
	HalfOpenRequests    uint32
}
