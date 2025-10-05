package main

import (
	"context"
	"flag"
	"os"

	"intern/internal/ai/anthropic"
	"intern/internal/config"
	"intern/internal/orchestrator"
	"intern/internal/repository"
	"intern/internal/repository/github"
	"intern/internal/ticketing"
	jiraraw "intern/internal/ticketing/jira-raw"

	logger "github.com/jenish-jain/logger"
)

func main() {
	initFlag := flag.Bool("init", false, "initialize sample config and state files")
	flag.Parse()

	logger.Init("debug")

	if *initFlag {
		writeSampleFiles()
		logger.Info("Sample config.yaml, .env.example, and agent_state.jsonc created.")
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

	agent := anthropic.NewClient(cfg.AnthropicAPIKey)
	coordinator := orchestrator.NewCoordinator(ticketingSvc, repoSvc, agent, cfg, state)
	logger.Info("Starting AI Intern Agent MVP...")
	coordinator.Run(context.Background())
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

ANTHROPIC_API_KEY="your-anthropic-api-key"

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
