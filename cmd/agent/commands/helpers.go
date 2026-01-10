package commands

import (
	"context"
	"os"

	"intern/internal/config"
	"intern/internal/errors"
	"intern/internal/orchestrator"
	"intern/internal/provider"
	"intern/internal/repository"
	"intern/internal/repository/github"
	"intern/internal/ticketing"
	jiraraw "intern/internal/ticketing/jira-raw"

	logger "github.com/jenish-jain/logger"
)

// Dependencies holds all initialized dependencies for the application
type Dependencies struct {
	Config       *config.Config
	JiraClient   interface{} // JIRA ticketing client
	TicketingSvc *ticketing.Service
	GitHubClient repository.RepositoryClient
	RepoSvc      *repository.RepositoryService
	State        *orchestrator.State
	Agent        interface{} // AI agent (provider-agnostic)
	Coordinator  *orchestrator.Coordinator
	RepoPaths    *repository.RepositoryPath
}

// State represents the agent state (wrapper for orchestrator.State)
type State struct {
	*orchestrator.State
}

// InitDependencies initializes all dependencies for the agent
func InitDependencies(ctx context.Context) (*Dependencies, error) {
	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Initialize JIRA client
	jiraClient, err := jiraraw.NewRawClient(cfg.JiraURL, cfg.JiraEmail, cfg.JiraAPIToken)
	if err != nil {
		logger.Error("Failed to init JIRA client: %v", err)
		return nil, err
	}

	// Health check for JIRA
	if err := jiraClient.HealthCheck(context.Background()); err != nil {
		logger.Error("JIRA health check failed: %v", err)
		return nil, err
	}

	// Create ticketing service
	ticketingSvc := ticketing.NewService(jiraClient)

	// Create repository path manager
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "./workspace"
	}

	repoPaths, err := repository.NewRepositoryPath(workingDir, cfg.GitHubRepo)
	if err != nil {
		logger.Error("Failed to create repository path manager", "error", err)
		return nil, err
	}

	// Initialize GitHub client and repository service
	githubClient := github.NewClient(cfg.GitHubToken, cfg.GitHubOwner, cfg.GitHubRepo, repoPaths)
	repoSvc := repository.NewRepositoryService(githubClient)

	// Load state
	stateFile := "agent_state.jsonc"
	state := orchestrator.NewState(stateFile)

	// Load existing state if available
	// Only ignore "file not found" error (expected on first run)
	// Fail on other errors (permission denied, corrupted file, etc.)
	if err := state.Load(); err != nil && !os.IsNotExist(err) {
		stateErr := errors.NewStateLoadError(err, stateFile)
		logger.Error("Failed to load state file", stateErr.LogFields())
		logger.Info("State file may be corrupted. Delete %s to reset state.", stateFile)
		return nil, err
	}

	// Initialize AI agent based on configured provider
	agent, err := provider.NewAgent(cfg)
	if err != nil {
		logger.Error("Failed to initialize AI agent: %v", err)
		return nil, err
	}
	logger.Info("Initialized AI provider", "provider", cfg.AIProvider)

	// Create coordinator
	coordinator := orchestrator.NewCoordinator(ticketingSvc, repoSvc, agent, cfg, state, repoPaths)

	return &Dependencies{
		Config:       cfg,
		JiraClient:   jiraClient,
		TicketingSvc: ticketingSvc,
		GitHubClient: githubClient,
		RepoSvc:      repoSvc,
		State:        state,
		Agent:        agent,
		Coordinator:  coordinator,
		RepoPaths:    repoPaths,
	}, nil
}

// InitPartialDependencies initializes only config and repository paths for lightweight commands
func InitPartialDependencies() (*config.Config, *repository.RepositoryPath, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, err
	}

	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "./workspace"
	}

	repoPaths, err := repository.NewRepositoryPath(workingDir, cfg.GitHubRepo)
	if err != nil {
		logger.Error("Failed to create repository path manager", "error", err)
		return nil, nil, err
	}

	return cfg, repoPaths, nil
}

// NewState creates a new state instance
func NewState(filePath string) *State {
	return &State{
		State: orchestrator.NewState(filePath),
	}
}
