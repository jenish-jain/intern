package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jenish-jain/logger"
)

// MetricsServer serves metrics over HTTP in Prometheus format
type MetricsServer struct {
	metrics *Metrics
	server  *http.Server
	port    int
}

// NewMetricsServer creates a new metrics HTTP server
func NewMetricsServer(metrics *Metrics, port int) *MetricsServer {
	if port <= 0 {
		port = 9090 // Default Prometheus port
	}

	return &MetricsServer{
		metrics: metrics,
		port:    port,
	}
}

// Start starts the HTTP metrics server
func (ms *MetricsServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", ms.handleMetrics)
	mux.HandleFunc("/health", ms.handleHealth)
	mux.HandleFunc("/", ms.handleDashboard)

	ms.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ms.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ms.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Metrics server shutdown error", "error", err)
		}
	}()

	logger.Info("Starting metrics server", "port", ms.port, "endpoints", []string{"/metrics", "/health", "/"})

	if err := ms.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server error: %w", err)
	}

	return nil
}

// Stop stops the HTTP metrics server
func (ms *MetricsServer) Stop() error {
	if ms.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ms.server.Shutdown(ctx)
	}
	return nil
}

// handleMetrics serves metrics in Prometheus format
func (ms *MetricsServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot := ms.metrics.Snapshot()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Write Prometheus metrics format
	fmt.Fprintf(w, "# HELP ai_intern_tickets_processed_total Total number of tickets processed\n")
	fmt.Fprintf(w, "# TYPE ai_intern_tickets_processed_total counter\n")
	fmt.Fprintf(w, "ai_intern_tickets_processed_total %d\n\n", snapshot.TicketsProcessed)

	fmt.Fprintf(w, "# HELP ai_intern_prs_created_total Total number of PRs created\n")
	fmt.Fprintf(w, "# TYPE ai_intern_prs_created_total counter\n")
	fmt.Fprintf(w, "ai_intern_prs_created_total %d\n\n", snapshot.PRsCreated)

	fmt.Fprintf(w, "# HELP ai_intern_tickets_failed_total Total number of tickets that failed\n")
	fmt.Fprintf(w, "# TYPE ai_intern_tickets_failed_total counter\n")
	fmt.Fprintf(w, "ai_intern_tickets_failed_total %d\n\n", snapshot.TicketsFailed)

	fmt.Fprintf(w, "# HELP ai_intern_retries_total Total number of retry attempts\n")
	fmt.Fprintf(w, "# TYPE ai_intern_retries_total counter\n")
	fmt.Fprintf(w, "ai_intern_retries_total %d\n\n", snapshot.Retries)

	fmt.Fprintf(w, "# HELP ai_intern_ai_plan_failures_total Total number of AI planning failures\n")
	fmt.Fprintf(w, "# TYPE ai_intern_ai_plan_failures_total counter\n")
	fmt.Fprintf(w, "ai_intern_ai_plan_failures_total %d\n\n", snapshot.AIPlanFailures)

	fmt.Fprintf(w, "# HELP ai_intern_cost_total_dollars Total cost in US dollars\n")
	fmt.Fprintf(w, "# TYPE ai_intern_cost_total_dollars gauge\n")
	fmt.Fprintf(w, "ai_intern_cost_total_dollars %.6f\n\n", snapshot.TotalCost)

	fmt.Fprintf(w, "# HELP ai_intern_input_tokens_total Total input tokens used\n")
	fmt.Fprintf(w, "# TYPE ai_intern_input_tokens_total counter\n")
	fmt.Fprintf(w, "ai_intern_input_tokens_total %d\n\n", snapshot.TotalInputTokens)

	fmt.Fprintf(w, "# HELP ai_intern_output_tokens_total Total output tokens generated\n")
	fmt.Fprintf(w, "# TYPE ai_intern_output_tokens_total counter\n")
	fmt.Fprintf(w, "ai_intern_output_tokens_total %d\n\n", snapshot.TotalOutputTokens)

	fmt.Fprintf(w, "# HELP ai_intern_smart_context_used_total Number of times smart context was used\n")
	fmt.Fprintf(w, "# TYPE ai_intern_smart_context_used_total counter\n")
	fmt.Fprintf(w, "ai_intern_smart_context_used_total %d\n\n", snapshot.SmartContextUsed)

	fmt.Fprintf(w, "# HELP ai_intern_simple_context_used_total Number of times simple context was used\n")
	fmt.Fprintf(w, "# TYPE ai_intern_simple_context_used_total counter\n")
	fmt.Fprintf(w, "ai_intern_simple_context_used_total %d\n\n", snapshot.SimpleContextUsed)

	fmt.Fprintf(w, "# HELP ai_intern_heal_attempts_total Total number of self-healing attempts\n")
	fmt.Fprintf(w, "# TYPE ai_intern_heal_attempts_total counter\n")
	fmt.Fprintf(w, "ai_intern_heal_attempts_total %d\n\n", snapshot.HealAttempts)

	fmt.Fprintf(w, "# HELP ai_intern_heal_successes_total Number of successful self-healing attempts\n")
	fmt.Fprintf(w, "# TYPE ai_intern_heal_successes_total counter\n")
	fmt.Fprintf(w, "ai_intern_heal_successes_total %d\n\n", snapshot.HealSuccesses)

	fmt.Fprintf(w, "# HELP ai_intern_heal_failures_total Number of failed self-healing attempts\n")
	fmt.Fprintf(w, "# TYPE ai_intern_heal_failures_total counter\n")
	fmt.Fprintf(w, "ai_intern_heal_failures_total %d\n\n", snapshot.HealFailures)

	fmt.Fprintf(w, "# HELP ai_intern_files_changed_total Total number of files changed\n")
	fmt.Fprintf(w, "# TYPE ai_intern_files_changed_total counter\n")
	fmt.Fprintf(w, "ai_intern_files_changed_total %d\n\n", snapshot.TotalFilesChanged)

	fmt.Fprintf(w, "# HELP ai_intern_execution_time_seconds Total execution time in seconds\n")
	fmt.Fprintf(w, "# TYPE ai_intern_execution_time_seconds gauge\n")
	fmt.Fprintf(w, "ai_intern_execution_time_seconds %.2f\n\n", snapshot.TotalExecutionTime.Seconds())

	fmt.Fprintf(w, "# HELP ai_intern_avg_execution_time_seconds Average execution time per ticket in seconds\n")
	fmt.Fprintf(w, "# TYPE ai_intern_avg_execution_time_seconds gauge\n")
	fmt.Fprintf(w, "ai_intern_avg_execution_time_seconds %.2f\n\n", snapshot.AvgExecutionTime.Seconds())

	fmt.Fprintf(w, "# HELP ai_intern_avg_cost_per_ticket Average cost per ticket in dollars\n")
	fmt.Fprintf(w, "# TYPE ai_intern_avg_cost_per_ticket gauge\n")
	fmt.Fprintf(w, "ai_intern_avg_cost_per_ticket %.6f\n\n", snapshot.AvgCostPerTicket)

	fmt.Fprintf(w, "# HELP ai_intern_runtime_seconds Total runtime since start in seconds\n")
	fmt.Fprintf(w, "# TYPE ai_intern_runtime_seconds gauge\n")
	fmt.Fprintf(w, "ai_intern_runtime_seconds %.2f\n", snapshot.TotalRuntime.Seconds())
}

// handleHealth serves a health check endpoint
func (ms *MetricsServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy","uptime_seconds":%.2f}`, ms.metrics.Snapshot().TotalRuntime.Seconds())
}

// handleDashboard serves a simple HTML dashboard
func (ms *MetricsServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	snapshot := ms.metrics.Snapshot()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>AI Intern Agent - Metrics Dashboard</title>
    <meta http-equiv="refresh" content="5">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            margin: 0;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        h1 {
            color: #333;
            margin-bottom: 10px;
        }
        .subtitle {
            color: #666;
            margin-bottom: 30px;
        }
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
        }
        .metric-card {
            background: white;
            border-radius: 8px;
            padding: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .metric-card h2 {
            margin: 0 0 15px 0;
            color: #555;
            font-size: 14px;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }
        .metric-value {
            font-size: 36px;
            font-weight: bold;
            color: #2196F3;
            margin-bottom: 5px;
        }
        .metric-label {
            font-size: 14px;
            color: #999;
        }
        .metric-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
            padding-bottom: 10px;
            border-bottom: 1px solid #eee;
        }
        .metric-row:last-child {
            margin-bottom: 0;
            padding-bottom: 0;
            border-bottom: none;
        }
        .metric-name {
            font-size: 14px;
            color: #666;
        }
        .metric-number {
            font-size: 18px;
            font-weight: 600;
            color: #333;
        }
        .success { color: #4CAF50; }
        .warning { color: #FF9800; }
        .error { color: #F44336; }
        .info { color: #2196F3; }
        footer {
            text-align: center;
            margin-top: 40px;
            color: #999;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>AI Intern Agent Dashboard</h1>
        <p class="subtitle">Live metrics - Auto-refreshes every 5 seconds</p>

        <div class="metrics-grid">
            <div class="metric-card">
                <h2>Processing Overview</h2>
                <div class="metric-row">
                    <span class="metric-name">Tickets Processed</span>
                    <span class="metric-number success">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">PRs Created</span>
                    <span class="metric-number success">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Tickets Failed</span>
                    <span class="metric-number error">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">AI Plan Failures</span>
                    <span class="metric-number warning">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Retries</span>
                    <span class="metric-number">%d</span>
                </div>
            </div>

            <div class="metric-card">
                <h2>Cost Metrics</h2>
                <div class="metric-value info">$%.2f</div>
                <div class="metric-label">Total Cost</div>
                <div class="metric-row" style="margin-top: 15px;">
                    <span class="metric-name">Avg Cost/Ticket</span>
                    <span class="metric-number">$%.3f</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Input Tokens</span>
                    <span class="metric-number">%s</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Output Tokens</span>
                    <span class="metric-number">%s</span>
                </div>
            </div>

            <div class="metric-card">
                <h2>Self-Healing</h2>
                <div class="metric-row">
                    <span class="metric-name">Heal Attempts</span>
                    <span class="metric-number">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Successes</span>
                    <span class="metric-number success">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Failures</span>
                    <span class="metric-number error">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Success Rate</span>
                    <span class="metric-number">%s</span>
                </div>
            </div>

            <div class="metric-card">
                <h2>Context Strategy</h2>
                <div class="metric-row">
                    <span class="metric-name">Smart Context</span>
                    <span class="metric-number success">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Simple Context</span>
                    <span class="metric-number">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Smart Usage Rate</span>
                    <span class="metric-number">%s</span>
                </div>
            </div>

            <div class="metric-card">
                <h2>Performance</h2>
                <div class="metric-row">
                    <span class="metric-name">Total Runtime</span>
                    <span class="metric-number">%s</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Avg Execution Time</span>
                    <span class="metric-number">%s</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Files Changed</span>
                    <span class="metric-number">%d</span>
                </div>
                <div class="metric-row">
                    <span class="metric-name">Avg Files/Ticket</span>
                    <span class="metric-number">%.1f</span>
                </div>
            </div>
        </div>

        <footer>
            <p>Metrics endpoint: <a href="/metrics">/metrics</a> | Health check: <a href="/health">/health</a></p>
        </footer>
    </div>
</body>
</html>`,
		snapshot.TicketsProcessed,
		snapshot.PRsCreated,
		snapshot.TicketsFailed,
		snapshot.AIPlanFailures,
		snapshot.Retries,
		snapshot.TotalCost,
		snapshot.AvgCostPerTicket,
		formatNumber(snapshot.TotalInputTokens),
		formatNumber(snapshot.TotalOutputTokens),
		snapshot.HealAttempts,
		snapshot.HealSuccesses,
		snapshot.HealFailures,
		calculateSuccessRate(snapshot.HealSuccesses, snapshot.HealAttempts),
		snapshot.SmartContextUsed,
		snapshot.SimpleContextUsed,
		calculateSmartRate(snapshot.SmartContextUsed, snapshot.SimpleContextUsed),
		formatDurationHTML(snapshot.TotalRuntime),
		formatDurationHTML(snapshot.AvgExecutionTime),
		snapshot.TotalFilesChanged,
		snapshot.AvgFilesPerTicket,
	)

	fmt.Fprint(w, html)
}

// formatNumber formats large numbers with commas
func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1000000)
}

// calculateSuccessRate calculates success rate as percentage
func calculateSuccessRate(successes, total int64) string {
	if total == 0 {
		return "N/A"
	}
	rate := float64(successes) / float64(total) * 100
	return fmt.Sprintf("%.1f%%", rate)
}

// calculateSmartRate calculates smart context usage rate
func calculateSmartRate(smart, simple int64) string {
	total := smart + simple
	if total == 0 {
		return "N/A"
	}
	rate := float64(smart) / float64(total) * 100
	return fmt.Sprintf("%.1f%%", rate)
}

// formatDurationHTML formats duration for HTML display
func formatDurationHTML(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
