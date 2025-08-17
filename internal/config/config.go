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

	AgentUsername        string
	PollingInterval      string
	MaxConcurrentTickets int

	WorkingDir   string
	BaseBranch   string
	BranchPrefix string

	ContextMaxFiles int
	ContextMaxBytes int

	PlanMaxFiles     int
	AllowedWriteDirs []string // TODO: add to config
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

		AgentUsername:        viper.GetString("AGENT_USERNAME"),
		PollingInterval:      viper.GetString("POLLING_INTERVAL"),
		MaxConcurrentTickets: viper.GetInt("MAX_CONCURRENT_TICKETS"),

		WorkingDir:   viper.GetString("WORKING_DIR"),
		BaseBranch:   viper.GetString("BASE_BRANCH"),
		BranchPrefix: viper.GetString("BRANCH_PREFIX"),

		ContextMaxFiles: viper.GetInt("CONTEXT_MAX_FILES"),
		ContextMaxBytes: viper.GetInt("CONTEXT_MAX_BYTES"),

		PlanMaxFiles: viper.GetInt("PLAN_MAX_FILES"),
	}

	// Defaults
	if cfg.ContextMaxFiles <= 0 {
		cfg.ContextMaxFiles = 40
	}
	if cfg.ContextMaxBytes <= 0 {
		cfg.ContextMaxBytes = 32 * 1024
	}
	if cfg.PlanMaxFiles <= 0 {
		cfg.PlanMaxFiles = 20
	}
	allowed := viper.GetString("ALLOWED_WRITE_DIRS")
	if strings.TrimSpace(allowed) == "" {
		cfg.AllowedWriteDirs = []string{"internal", "cmd", "pkg", "docs"}
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
	if c.AnthropicAPIKey == "" {
		return fmt.Errorf("missing Anthropic API key")
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
