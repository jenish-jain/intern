package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	JiraURL         string
	JiraEmail       string
	JiraAPIToken    string
	JiraProject     string
	JiraTransitions map[string]string

	GitHubToken string
	GitHubOwner string
	GitHubRepo  string

	AnthropicAPIKey string

	// AI Provider configuration
	AIProvider     string // "anthropic" or "ollama"
	OllamaBaseURL  string // Ollama server URL (default: http://localhost:11434)
	OllamaModel    string // Ollama model name (e.g., qwen2.5-coder:7b)

	AgentUsername        string
	PollingInterval      string
	MaxConcurrentTickets int

	WorkingDir   string // Base working directory, will be joined with GitHubRepo to create ./workspace/{repoName}
	BaseBranch   string
	BranchPrefix string

	ContextMaxFiles      int
	ContextMaxBytes      int
	ContextCacheEnabled  bool   // Enable context caching
	ContextCacheTTL      string // Cache time-to-live (e.g., "1h", "30m")

	PlanMaxFiles     int
	AllowedWriteDirs []string

	RunTestsBeforePR bool
	RunVetBeforePR   bool

	// Self-healing configuration
	SelfHealEnabled      bool // Enable self-healing for failed quality gates
	SelfHealMaxAttempts  int  // Maximum healing attempts (default: 3)
	SelfHealOnTests      bool // Retry on test failures
	SelfHealOnVet        bool // Retry on vet failures
	SelfHealOnBuild      bool // Retry on build failures

	DryRun bool // If true, process tickets but don't create PRs (preview mode)

	// Metrics server configuration
	MetricsEnabled bool // Enable HTTP metrics server
	MetricsPort    int  // Port for metrics server (default: 9090)
}

func LoadConfig() (*Config, error) {
	_ = godotenv.Load()
	viper.AutomaticEnv()

	cfg := &Config{
		JiraURL:      viper.GetString("JIRA_URL"),
		JiraEmail:    viper.GetString("JIRA_EMAIL"),
		JiraAPIToken: viper.GetString("JIRA_API_TOKEN"),
		JiraProject:  viper.GetString("JIRA_PROJECT_KEY"),
		JiraTransitions: map[string]string{
			"To Do":       viper.GetString("JIRA_TRANSITION_TO_DO"),
			"In Progress": viper.GetString("JIRA_TRANSITION_IN_PROGRESS"),
			"Done":        viper.GetString("JIRA_TRANSITION_DONE"),
		},

		GitHubToken: viper.GetString("GITHUB_TOKEN"),
		GitHubOwner: viper.GetString("GITHUB_OWNER"),
		GitHubRepo:  viper.GetString("GITHUB_REPO"),

		AnthropicAPIKey: viper.GetString("ANTHROPIC_API_KEY"),

		AIProvider:    viper.GetString("AI_PROVIDER"),
		OllamaBaseURL: viper.GetString("OLLAMA_BASE_URL"),
		OllamaModel:   viper.GetString("OLLAMA_MODEL"),

		AgentUsername:        viper.GetString("AGENT_USERNAME"),
		PollingInterval:      viper.GetString("POLLING_INTERVAL"),
		MaxConcurrentTickets: viper.GetInt("MAX_CONCURRENT_TICKETS"),

		WorkingDir:   viper.GetString("WORKING_DIR"),
		BaseBranch:   viper.GetString("BASE_BRANCH"),
		BranchPrefix: viper.GetString("BRANCH_PREFIX"),

		ContextMaxFiles:     viper.GetInt("CONTEXT_MAX_FILES"),
		ContextMaxBytes:     viper.GetInt("CONTEXT_MAX_BYTES"),
		ContextCacheEnabled: viper.GetBool("CONTEXT_CACHE_ENABLED"),
		ContextCacheTTL:     viper.GetString("CONTEXT_CACHE_TTL"),

		PlanMaxFiles: viper.GetInt("PLAN_MAX_FILES"),

		RunTestsBeforePR: viper.GetBool("RUN_TESTS_BEFORE_PR"),
		RunVetBeforePR:   viper.GetBool("RUN_VET_BEFORE_PR"),

		SelfHealEnabled:     viper.GetBool("SELF_HEAL_ENABLED"),
		SelfHealMaxAttempts: viper.GetInt("SELF_HEAL_MAX_ATTEMPTS"),
		SelfHealOnTests:     viper.GetBool("SELF_HEAL_ON_TESTS"),
		SelfHealOnVet:       viper.GetBool("SELF_HEAL_ON_VET"),
		SelfHealOnBuild:     viper.GetBool("SELF_HEAL_ON_BUILD"),

		DryRun: viper.GetBool("DRY_RUN"),

		MetricsEnabled: viper.GetBool("METRICS_ENABLED"),
		MetricsPort:    viper.GetInt("METRICS_PORT"),
	}

	// Defaults
	if cfg.AIProvider == "" {
		cfg.AIProvider = "anthropic" // Default to Anthropic for backwards compatibility
	}
	if cfg.OllamaBaseURL == "" {
		cfg.OllamaBaseURL = "http://localhost:11434"
	}
	if cfg.ContextMaxFiles <= 0 {
		cfg.ContextMaxFiles = 20 // Reduced from 40 to prevent timeouts with large contexts
	}
	if cfg.ContextMaxBytes <= 0 {
		cfg.ContextMaxBytes = 32 * 1024
	}
	if cfg.ContextCacheTTL == "" {
		cfg.ContextCacheTTL = "1h" // Default: cache for 1 hour
	}
	// ContextCacheEnabled defaults to false (opt-in)
	if cfg.PlanMaxFiles <= 0 {
		cfg.PlanMaxFiles = 20
	}
	// Self-healing defaults
	if cfg.SelfHealMaxAttempts <= 0 {
		cfg.SelfHealMaxAttempts = 3 // Default: 3 healing attempts
	}
	// SelfHealEnabled defaults to false (opt-in)
	// SelfHealOnTests, SelfHealOnVet, SelfHealOnBuild default to false

	// Metrics defaults
	if cfg.MetricsPort <= 0 {
		cfg.MetricsPort = 9090 // Default Prometheus port
	}
	// MetricsEnabled defaults to false (opt-in)
	allowed := viper.GetString("ALLOWED_WRITE_DIRS")
	if strings.TrimSpace(allowed) == "" {
		cfg.AllowedWriteDirs = []string{"internal", "cmd", "pkg", "docs", "config", "."}
	} else {
		parts := strings.Split(allowed, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.AllowedWriteDirs = parts
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.JiraURL == "" || c.JiraEmail == "" || c.JiraAPIToken == "" || c.JiraProject == "" {
		return fmt.Errorf("missing JIRA configuration")
	}
	if c.GitHubToken == "" || c.GitHubOwner == "" || c.GitHubRepo == "" {
		return fmt.Errorf("missing GitHub configuration")
	}

	// Validate AI provider configuration
	switch c.AIProvider {
	case "anthropic":
		if c.AnthropicAPIKey == "" {
			return fmt.Errorf("missing Anthropic API key (required when AI_PROVIDER=anthropic)")
		}
	case "ollama":
		if c.OllamaModel == "" {
			return fmt.Errorf("missing Ollama model (required when AI_PROVIDER=ollama)")
		}
		if c.OllamaBaseURL == "" {
			return fmt.Errorf("missing Ollama base URL")
		}
	default:
		return fmt.Errorf("unsupported AI provider: %s (supported: anthropic, ollama)", c.AIProvider)
	}

	if c.AgentUsername == "" {
		return fmt.Errorf("missing agent username")
	}
	if c.PollingInterval == "" {
		return fmt.Errorf("missing polling interval")
	}
	if c.MaxConcurrentTickets <= 0 {
		return fmt.Errorf("max concurrent tickets must be > 0")
	}
	return nil
}
