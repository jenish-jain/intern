package orchestrator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewMetricsServer(t *testing.T) {
	metrics := NewMetrics()

	tests := []struct {
		name     string
		port     int
		wantPort int
	}{
		{
			name:     "default port",
			port:     0,
			wantPort: 9090,
		},
		{
			name:     "custom port",
			port:     8080,
			wantPort: 8080,
		},
		{
			name:     "negative port defaults to 9090",
			port:     -1,
			wantPort: 9090,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMetricsServer(metrics, tt.port)
			if server.port != tt.wantPort {
				t.Errorf("NewMetricsServer() port = %v, want %v", server.port, tt.wantPort)
			}
			if server.metrics != metrics {
				t.Error("NewMetricsServer() did not set metrics correctly")
			}
		})
	}
}

func TestHandleMetrics(t *testing.T) {
	metrics := NewMetrics()

	// Add some test data
	metrics.IncTicketsProcessed()
	metrics.IncTicketsProcessed()
	metrics.IncPRsCreated()
	metrics.AddTokenUsage(1000, 500, 0.50)
	metrics.IncSmartContextUsed()
	metrics.AddHealAttempts(2)
	metrics.IncHealSuccesses()

	server := NewMetricsServer(metrics, 9090)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	server.handleMetrics(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check response headers
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected Content-Type text/plain, got %s", contentType)
	}

	// Check Prometheus format
	expectedMetrics := []string{
		"# HELP ai_intern_tickets_processed_total",
		"# TYPE ai_intern_tickets_processed_total counter",
		"ai_intern_tickets_processed_total 2",
		"# HELP ai_intern_prs_created_total",
		"ai_intern_prs_created_total 1",
		"# HELP ai_intern_cost_total_dollars",
		"ai_intern_cost_total_dollars 0.500000",
		"# HELP ai_intern_input_tokens_total",
		"ai_intern_input_tokens_total 1000",
		"# HELP ai_intern_output_tokens_total",
		"ai_intern_output_tokens_total 500",
		"# HELP ai_intern_smart_context_used_total",
		"ai_intern_smart_context_used_total 1",
		"# HELP ai_intern_heal_attempts_total",
		"ai_intern_heal_attempts_total 2",
		"# HELP ai_intern_heal_successes_total",
		"ai_intern_heal_successes_total 1",
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(bodyStr, expected) {
			t.Errorf("Expected metrics output to contain %q, but it didn't", expected)
		}
	}
}

func TestHandleHealth(t *testing.T) {
	metrics := NewMetrics()
	server := NewMetricsServer(metrics, 9090)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Check response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check JSON contains expected fields
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"status":"healthy"`) {
		t.Error("Expected health response to contain status:healthy")
	}
	if !strings.Contains(bodyStr, `"uptime_seconds"`) {
		t.Error("Expected health response to contain uptime_seconds")
	}
}

func TestHandleDashboard(t *testing.T) {
	metrics := NewMetrics()

	// Add some test data
	metrics.IncTicketsProcessed()
	metrics.IncPRsCreated()
	metrics.AddTokenUsage(100, 50, 1.23)
	metrics.IncSmartContextUsed()

	server := NewMetricsServer(metrics, 9090)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleDashboard(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected Content-Type text/html, got %s", contentType)
	}

	// Check HTML contains expected elements
	expectedElements := []string{
		"<!DOCTYPE html>",
		"AI Intern Agent Dashboard",
		"Tickets Processed",
		"PRs Created",
		"Total Cost",
		"Self-Healing",
		"Context Strategy",
		"Performance",
		"/metrics",
		"/health",
		"refresh",
	}

	for _, expected := range expectedElements {
		if !strings.Contains(bodyStr, expected) {
			t.Errorf("Expected dashboard HTML to contain %q", expected)
		}
	}

	// Check that metrics values are displayed
	if !strings.Contains(bodyStr, "1") { // tickets processed
		t.Error("Expected dashboard to show tickets processed count")
	}
	if !strings.Contains(bodyStr, "$1.23") {
		t.Error("Expected dashboard to show cost")
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{"small number", 500, "500"},
		{"thousand", 1000, "1.0K"},
		{"thousands", 5432, "5.4K"},
		{"million", 1500000, "1.50M"},
		{"large million", 2345678, "2.35M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNumber(tt.input)
			if got != tt.want {
				t.Errorf("formatNumber(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCalculateSuccessRate(t *testing.T) {
	tests := []struct {
		name      string
		successes int64
		total     int64
		want      string
	}{
		{"zero total", 0, 0, "N/A"},
		{"perfect success", 10, 10, "100.0%"},
		{"half success", 5, 10, "50.0%"},
		{"partial success", 7, 10, "70.0%"},
		{"no success", 0, 10, "0.0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSuccessRate(tt.successes, tt.total)
			if got != tt.want {
				t.Errorf("calculateSuccessRate(%d, %d) = %v, want %v", tt.successes, tt.total, got, tt.want)
			}
		})
	}
}

func TestCalculateSmartRate(t *testing.T) {
	tests := []struct {
		name   string
		smart  int64
		simple int64
		want   string
	}{
		{"no usage", 0, 0, "N/A"},
		{"all smart", 10, 0, "100.0%"},
		{"all simple", 0, 10, "0.0%"},
		{"half and half", 5, 5, "50.0%"},
		{"mostly smart", 8, 2, "80.0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSmartRate(tt.smart, tt.simple)
			if got != tt.want {
				t.Errorf("calculateSmartRate(%d, %d) = %v, want %v", tt.smart, tt.simple, got, tt.want)
			}
		})
	}
}

func TestFormatDurationHTML(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"seconds", 45 * time.Second, "45.0s"},
		{"minute", 90 * time.Second, "1.5m"},
		{"minutes", 5 * time.Minute, "5.0m"},
		{"hour", 65 * time.Minute, "1h 5m"},
		{"hours", 150 * time.Minute, "2h 30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDurationHTML(tt.duration)
			if got != tt.want {
				t.Errorf("formatDurationHTML(%v) = %v, want %v", tt.duration, got, tt.want)
			}
		})
	}
}

func TestMetricsServerStart(t *testing.T) {
	metrics := NewMetrics()
	server := NewMetricsServer(metrics, 0) // Use port 0 for automatic assignment

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop server
	cancel()

	// Wait for server to stop
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Server returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop in time")
	}
}

func TestMetricsServerStop(t *testing.T) {
	metrics := NewMetrics()
	server := NewMetricsServer(metrics, 0)

	// Start server briefly
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Stop server
	err := server.Stop()
	if err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}

	// Calling stop again should be safe
	err = server.Stop()
	if err != nil && err != http.ErrServerClosed {
		t.Errorf("Second Stop() returned unexpected error: %v", err)
	}
}

func TestMetricsServerEndpoints(t *testing.T) {
	metrics := NewMetrics()
	metrics.IncTicketsProcessed()

	server := NewMetricsServer(metrics, 0)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedType   string
	}{
		{
			name:           "metrics endpoint",
			path:           "/metrics",
			expectedStatus: http.StatusOK,
			expectedType:   "text/plain",
		},
		{
			name:           "health endpoint",
			path:           "/health",
			expectedStatus: http.StatusOK,
			expectedType:   "application/json",
		},
		{
			name:           "dashboard endpoint",
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedType:   "text/html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			// Route to correct handler
			switch tt.path {
			case "/metrics":
				server.handleMetrics(w, req)
			case "/health":
				server.handleHealth(w, req)
			case "/":
				server.handleDashboard(w, req)
			}

			resp := w.Result()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, tt.expectedType) {
				t.Errorf("Expected Content-Type to contain %s, got %s", tt.expectedType, contentType)
			}
		})
	}
}
