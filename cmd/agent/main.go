package main

import (
	"context"
	"flag"
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

PLAN_MAX_FILES=10
ALLOWED_WRITE_DIRS="internal,cmd,pkg,docs,config,."
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

	// Build index
	idx := indexer.New(repoRoot)
	fileIndex, err := idx.BuildIndex()
	if err != nil {
		logger.Error("Failed to build index", "error", err)
		os.Exit(1)
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
