package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"intern/internal/config"
	"intern/internal/indexer"
	"intern/internal/orchestrator"
	"intern/internal/provider"
	"intern/internal/repository"
	"intern/internal/repository/github"
	"intern/internal/ticketing"
	jiraraw "intern/internal/ticketing/jira-raw"

	logger "github.com/jenish-jain/logger"
)

func main() {
	initFlag := flag.Bool("init", false, "initialize sample config and state files")
	buildIndexFlag := flag.Bool("build-index", false, "build file index for smart context selection")
	statusFlag := flag.Bool("status", false, "show current agent status and metrics")
	metricsFlag := flag.Bool("metrics", false, "show detailed metrics summary")
	flag.Parse()

	logger.Init("debug")

	if *initFlag {
		writeSampleFiles()
		logger.Info("Sample config.yaml, .env.example, and agent_state.jsonc created.")
		return
	}

	if *buildIndexFlag {
		buildIndex()
		return
	}

	if *statusFlag {
		showStatus()
		return
	}

	if *metricsFlag {
		showMetrics()
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	jiraClient, err := jiraraw.NewRawClient(cfg.JiraURL, cfg.JiraEmail, cfg.JiraAPIToken)
	// jiraClient, err := jira.NewClient(cfg.JiraURL, cfg.JiraEmail, cfg.JiraAPIToken)
	if err != nil {
		logger.Error("Failed to init JIRA client: %v", err)
		os.Exit(1)
	}
	if err := jiraClient.HealthCheck(context.Background()); err != nil {
		logger.Error("JIRA health check failed: %v", err)
		os.Exit(1)
	}

	ticketingSvc := ticketing.NewTicketingService(jiraClient)

	githubClient := github.NewClient(cfg.GitHubToken, cfg.GitHubOwner, cfg.GitHubRepo)
	repoSvc := repository.NewRepositoryService(githubClient)

	stateFile := "agent_state.jsonc"
	state := orchestrator.NewState(stateFile)
	_ = state.Load() // ignore error if file doesn't exist

	// Initialize AI agent based on configured provider (anthropic or ollama)
	agent, err := provider.NewAgent(cfg)
	if err != nil {
		logger.Error("Failed to initialize AI agent: %v", err)
		os.Exit(1)
	}
	logger.Info("Initialized AI provider", "provider", cfg.AIProvider)

	coordinator := orchestrator.NewCoordinator(ticketingSvc, repoSvc, agent, cfg, state)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal, shutting down gracefully...")
		cancel()
	}()

	logger.Info("Starting AI Intern Agent MVP...")
	coordinator.Run(ctx)
}

func writeSampleFiles() {

	os.WriteFile(".env.example", []byte(`JIRA_URL="https://company.atlassian.net"
JIRA_EMAIL="ai-agent@company.com"
JIRA_API_TOKEN="your-jira-api-token"
JIRA_PROJECT_KEY="PROJ"
JIRA_TRANSITION_TO_DO="11"
JIRA_TRANSITION_IN_PROGRESS="21"
JIRA_TRANSITION_DONE="31"

GITHUB_TOKEN="your-github-token"
GITHUB_OWNER="company"
GITHUB_REPO="main-repo"

# AI Provider Configuration
# Options: "anthropic" (cloud API) or "ollama" (local LLM)
AI_PROVIDER="anthropic"

# Anthropic Configuration (required if AI_PROVIDER=anthropic)
ANTHROPIC_API_KEY="your-anthropic-api-key"

# Ollama Configuration (required if AI_PROVIDER=ollama)
# Make sure Ollama is running locally: https://ollama.ai
OLLAMA_BASE_URL="http://localhost:11434"
OLLAMA_MODEL="qwen2.5-coder:7b"  # Options: qwen2.5-coder:7b, deepseek-coder:6.7b, codellama:13b

AGENT_USERNAME="ai-intern"
POLLING_INTERVAL="30s"
MAX_CONCURRENT_TICKETS=1

WORKING_DIR="./workspace"  # Will be ./workspace/{GITHUB_REPO} automatically
BASE_BRANCH="master"
BRANCH_PREFIX="feature/"

CONTEXT_MAX_FILES=40
CONTEXT_MAX_BYTES=32
CONTEXT_CACHE_ENABLED=true  # Enable context caching for better performance
CONTEXT_CACHE_TTL=1h         # Cache time-to-live (e.g., "1h", "30m")

PLAN_MAX_FILES=10
ALLOWED_WRITE_DIRS="internal,cmd,pkg,docs,config,."

# Self-Healing Configuration
SELF_HEAL_ENABLED=false      # Enable AI-powered self-healing for failed quality gates
SELF_HEAL_MAX_ATTEMPTS=3     # Maximum healing attempts (default: 3)
SELF_HEAL_ON_TESTS=true      # Retry on test failures
SELF_HEAL_ON_VET=true        # Retry on vet failures
SELF_HEAL_ON_BUILD=false     # Retry on build failures (usually not needed for Go)

# Operational Mode
DRY_RUN=false  # If true, process tickets but don't create PRs (preview mode)

# Metrics Configuration
METRICS_ENABLED=false  # Enable HTTP metrics server with Prometheus format
METRICS_PORT=9090      # Port for metrics server (default: 9090)
# Access metrics at http://localhost:9090/metrics (Prometheus format)
# Access dashboard at http://localhost:9090/ (web UI)
# Access health check at http://localhost:9090/health
`), 0644)

	os.WriteFile("agent_state.jsonc", []byte(`{"processed":{}}`), 0644)
}

func buildIndex() {
	logger.Info("Building file index for smart context selection...")

	// Load config to get repository path
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Determine repository root
	workingDir := os.Getenv("AGENT_WORKING_DIR")
	if workingDir == "" {
		workingDir = cfg.WorkingDir
	}
	repoRoot := filepath.Join(workingDir, cfg.GitHubRepo)

	// Check if repository exists
	if _, err := os.Stat(repoRoot); os.IsNotExist(err) {
		logger.Error("Repository not found", "path", repoRoot)
		logger.Info("Make sure to clone the repository first or set AGENT_WORKING_DIR correctly")
		os.Exit(1)
	}

	logger.Info("Indexing repository", "path", repoRoot)

	// Build or update index incrementally
	idx := indexer.New(repoRoot)
	fileIndex, wasUpdated, err := idx.RebuildIfStale()
	if err != nil {
		logger.Error("Failed to build index", "error", err)
		os.Exit(1)
	}

	if !wasUpdated {
		logger.Info("Index is already up to date")
		indexPath := filepath.Join(repoRoot, indexer.IndexDirName, indexer.IndexFileName)
		logger.Info("Using existing index", "path", indexPath)
		return
	}

	logger.Info("Index built successfully", "files", len(fileIndex.Files), "modules", len(fileIndex.Modules))

	// Save index
	if err := idx.SaveIndex(fileIndex); err != nil {
		logger.Error("Failed to save index", "error", err)
		os.Exit(1)
	}

	indexPath := filepath.Join(repoRoot, indexer.IndexDirName, indexer.IndexFileName)
	logger.Info("Index saved successfully", "path", indexPath)

	// Show some statistics
	categoryCounts := make(map[string]int)
	for _, meta := range fileIndex.Files {
		categoryCounts[meta.Category]++
	}

	logger.Info("Index statistics:")
	for category, count := range categoryCounts {
		logger.Info("  - "+category, "count", count)
	}
}

func showStatus() {
	logger.Info("Checking agent status...")

	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Load state
	stateFile := "agent_state.jsonc"
	state := orchestrator.NewState(stateFile)
	if err := state.Load(); err != nil {
		logger.Warn("Failed to load state", "error", err)
	}

	// Try to load metrics if available
	workingDir := os.Getenv("AGENT_WORKING_DIR")
	if workingDir == "" {
		workingDir = cfg.WorkingDir
		if workingDir == "" {
			workingDir = "./workspace"
		}
	}
	repoRoot := filepath.Join(workingDir, cfg.GitHubRepo)
	metricsPath := filepath.Join(repoRoot, ".ai-intern", "metrics.json")

	fmt.Println("\n=== AI Intern Agent Status ===")
	fmt.Printf("\nConfiguration:\n")
	fmt.Printf("  AI Provider:     %s\n", cfg.AIProvider)
	if cfg.AIProvider == "ollama" {
		fmt.Printf("  Ollama Model:    %s\n", cfg.OllamaModel)
		fmt.Printf("  Ollama URL:      %s\n", cfg.OllamaBaseURL)
	}
	fmt.Printf("  GitHub Repo:     %s/%s\n", cfg.GitHubOwner, cfg.GitHubRepo)
	fmt.Printf("  JIRA Project:    %s\n", cfg.JiraProject)
	fmt.Printf("  Working Dir:     %s\n", workingDir)
	fmt.Printf("  Max Concurrent:  %d\n", cfg.MaxConcurrentTickets)
	fmt.Printf("  Polling Interval: %s\n", cfg.PollingInterval)

	fmt.Printf("\nFeatures:\n")
	fmt.Printf("  Context Caching:  %v\n", cfg.ContextCacheEnabled)
	fmt.Printf("  Self-Healing:     %v\n", cfg.SelfHealEnabled)
	fmt.Printf("  Metrics Server:   %v", cfg.MetricsEnabled)
	if cfg.MetricsEnabled {
		fmt.Printf(" (port %d)", cfg.MetricsPort)
	}
	fmt.Println()
	fmt.Printf("  Dry Run Mode:     %v\n", cfg.DryRun)

	fmt.Printf("\nProcessed Tickets: %d\n", len(state.Processed))

	// Try to show latest metrics if available
	if _, err := os.Stat(metricsPath); err == nil {
		output, err := orchestrator.LoadMetrics(metricsPath)
		if err == nil {
			fmt.Printf("\nLatest Metrics (from %s):\n", output.RunMetadata.Timestamp)
			fmt.Printf("  Tickets Processed: %d\n", output.Summary.TicketsProcessed)
			fmt.Printf("  PRs Created:       %d\n", output.Summary.PRsCreated)
			fmt.Printf("  Total Cost:        $%.2f\n", output.Summary.TotalCost)
		}
	}

	fmt.Println("\n=== End Status ===\n")
}

func showMetrics() {
	logger.Info("Loading metrics...")

	// Load config to find metrics file
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	workingDir := os.Getenv("AGENT_WORKING_DIR")
	if workingDir == "" {
		workingDir = cfg.WorkingDir
		if workingDir == "" {
			workingDir = "./workspace"
		}
	}
	repoRoot := filepath.Join(workingDir, cfg.GitHubRepo)
	metricsPath := filepath.Join(repoRoot, ".ai-intern", "metrics.json")

	// Check if metrics file exists
	if _, err := os.Stat(metricsPath); os.IsNotExist(err) {
		logger.Error("No metrics file found", "path", metricsPath)
		fmt.Println("\nNo metrics available yet. Run the agent to generate metrics.")
		os.Exit(1)
	}

	// Load metrics
	output, err := orchestrator.LoadMetrics(metricsPath)
	if err != nil {
		logger.Error("Failed to load metrics", "error", err)
		os.Exit(1)
	}

	// Print metrics summary
	fmt.Println("\n=== AI Intern Agent Metrics ===")
	fmt.Printf("\nRun Metadata:\n")
	fmt.Printf("  Timestamp:       %s\n", output.RunMetadata.Timestamp)
	fmt.Printf("  Duration:        %.1f seconds\n", output.RunMetadata.DurationSeconds)
	fmt.Printf("  Version:         %s\n", output.RunMetadata.AgentVersion)

	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Tickets Processed: %d\n", output.Summary.TicketsProcessed)
	fmt.Printf("  PRs Created:       %d\n", output.Summary.PRsCreated)
	fmt.Printf("  Tickets Failed:    %d\n", output.Summary.TicketsFailed)

	fmt.Printf("\nCost Metrics:\n")
	fmt.Printf("  Total Cost:        $%.2f\n", output.Summary.TotalCost)
	fmt.Printf("  Avg Cost/Ticket:   $%.3f\n", output.Summary.AvgCostPerTicket)
	fmt.Printf("  Input Tokens:      %d\n", output.Summary.TotalInputTokens)
	fmt.Printf("  Output Tokens:     %d\n", output.Summary.TotalOutputTokens)

	fmt.Printf("\nContext Strategy:\n")
	fmt.Printf("  Smart Context:     %d\n", output.Summary.SmartContextUsed)
	fmt.Printf("  Simple Context:    %d\n", output.Summary.SimpleContextUsed)

	fmt.Printf("\nPerformance:\n")
	fmt.Printf("  Avg Time/Ticket:   %.1f seconds\n", output.Summary.AvgTimePerTicket)
	fmt.Printf("  Files Changed:     %d\n", output.Summary.TotalFilesChanged)

	fmt.Println("\n=== End Metrics ===\n")
}
