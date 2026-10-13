package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"intern/internal/errors"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	// TicketingMode selects the source of tickets/asks: "jira" (default, poll loop)
	// or "slack" (HTTP webhook, request-driven — used for the Cloud Run deployment).
	TicketingMode string

	JiraURL         string
	JiraEmail       string
	JiraAPIToken    string
	JiraProject     string
	JiraTransitions map[string]string
	// JiraAgentAccountID is the agent's JIRA accountId, used as assignee
	// for imported tickets. Looked up from AGENT_USERNAME when empty.
	JiraAgentAccountID string
	JiraIssueType      string // issue type for imported tickets (default "Task")

	SlackBotToken      string
	SlackSigningSecret string
	// SlackArchitectChannel: every thread in this channel ID is an
	// architect conversation. Messages starting with "plan:" elsewhere
	// start one too.
	SlackArchitectChannel string
	// SlackNotifyChannel receives escalations and merge announcements.
	SlackNotifyChannel string

	// Port is the HTTP listen port for `agent serve` (Slack webhook + healthz).
	// Cloud Run injects this via the PORT env var.
	Port string

	GitHubToken string
	GitHubOwner string
	GitHubRepo  string

	AnthropicAPIKey string
	AnthropicModel  string // Claude model ID; empty uses the client default

	// AI Provider configuration
	AIProvider    string // "anthropic" or "ollama"
	OllamaBaseURL string // Ollama server URL (default: http://localhost:11434)
	OllamaModel   string // Ollama model name (e.g., qwen2.5-coder:7b)

	AgentUsername        string
	PollingInterval      string
	MaxConcurrentTickets int
	MaxTicketAttempts    int // Failed attempts before a ticket is skipped (default: 3)

	WorkingDir   string // Base working directory, will be joined with GitHubRepo to create ./workspace/{repoName}
	BaseBranch   string
	BranchPrefix string

	ContextMaxFiles     int
	ContextMaxBytes     int
	ContextCacheEnabled bool   // Enable context caching
	ContextCacheTTL     string // Cache time-to-live (e.g., "1h", "30m")

	PlanMaxFiles     int
	AllowedWriteDirs []string

	RunTestsBeforePR bool
	RunVetBeforePR   bool

	// Self-healing configuration
	SelfHealEnabled     bool // Enable self-healing for failed quality gates
	SelfHealMaxAttempts int  // Maximum healing attempts (default: 3)
	SelfHealOnTests     bool // Retry on test failures
	SelfHealOnVet       bool // Retry on vet failures
	SelfHealOnBuild     bool // Retry on build failures

	// AIRunner selects the engine that does a ticket's work: "legacy"
	// (single-shot JSON edits via AI_PROVIDER) or "claude-code" (agentic
	// Claude Code CLI).
	AIRunner string
	// Claude Code runner settings.
	ClaudeBinary string // CLI path (default "claude")
	ClaudeModel  string // --model, e.g. "opus" (default: CLI default)
	ClaudeEffort string // --effort (default: CLI default)
	// RunnerTimeout bounds one runner job, e.g. "45m".
	RunnerTimeout string
	// RunnerAllowedCommands are Bash command prefixes the agent may run
	// when the sandbox is local (ignored for docker, which is isolated).
	RunnerAllowedCommands []string
	// Sandbox is where the runner process executes: "local" or "docker".
	Sandbox        string
	SandboxImage   string   // image for SANDBOX=docker
	SandboxNetwork string   // docker --network (default "bridge")
	SandboxPassEnv []string // extra host env vars passed to a local sandbox

	// Review/test/merge flow after a PR opens.
	ReviewEnabled   bool   // run the reviewer persona on each PR
	MaxReviewRounds int    // review rounds before escalating to a human (default 2)
	TestEnabled     bool   // run the tester persona after approval
	AutoMerge       bool   // merge when the flow passes and CI is green
	MergeMethod     string // "squash" (default), "merge" or "rebase"
	// EpicReview runs the architect when an epic completes, to check the
	// merged code against the architecture (default true).
	EpicReview bool

	// VerifyFixAttempts is how many times a failing verify is handed back
	// to the runner to fix. Defaults to SELF_HEAL_MAX_ATTEMPTS when
	// SELF_HEAL_ENABLED is set, otherwise 0.
	VerifyFixAttempts int
	// TicketBudgetUSD caps model spend per ticket for runners that
	// support it (0 = no cap).
	TicketBudgetUSD float64
	// ProjectBudgetUSD caps total model spend across all roles; reaching
	// it pauses new work (0 = no cap).
	ProjectBudgetUSD float64
	// SleepAfterFailures puts intern to sleep after this many model jobs
	// in a row fail with an error only a person can fix (a spend limit, no
	// credit, a rejected key): all work stops and, on Cloud Run, the
	// service scales itself to zero until woken (default 3; 0 never
	// sleeps and keeps retrying).
	SleepAfterFailures int

	DryRun bool // If true, process tickets but don't create PRs (preview mode)

	// Metrics server configuration
	MetricsEnabled bool // Enable HTTP metrics server
	MetricsPort    int  // Port for metrics server (default: 9090)
}

func LoadConfig() (*Config, error) {
	_ = godotenv.Load()
	viper.AutomaticEnv()

	cfg := &Config{
		TicketingMode: viper.GetString("TICKETING_MODE"),

		JiraURL:            viper.GetString("JIRA_URL"),
		JiraEmail:          viper.GetString("JIRA_EMAIL"),
		JiraAPIToken:       viper.GetString("JIRA_API_TOKEN"),
		JiraProject:        viper.GetString("JIRA_PROJECT_KEY"),
		JiraAgentAccountID: viper.GetString("JIRA_AGENT_ACCOUNT_ID"),
		JiraIssueType:      viper.GetString("JIRA_ISSUE_TYPE"),
		JiraTransitions: map[string]string{
			"To Do":       viper.GetString("JIRA_TRANSITION_TO_DO"),
			"In Progress": viper.GetString("JIRA_TRANSITION_IN_PROGRESS"),
			"In Review":   viper.GetString("JIRA_TRANSITION_IN_REVIEW"),
			"Done":        viper.GetString("JIRA_TRANSITION_DONE"),
		},

		SlackBotToken:         viper.GetString("SLACK_BOT_TOKEN"),
		SlackSigningSecret:    viper.GetString("SLACK_SIGNING_SECRET"),
		SlackArchitectChannel: viper.GetString("SLACK_ARCHITECT_CHANNEL"),
		SlackNotifyChannel:    viper.GetString("SLACK_NOTIFY_CHANNEL"),
		Port:                  viper.GetString("PORT"),

		GitHubToken: viper.GetString("GITHUB_TOKEN"),
		GitHubOwner: viper.GetString("GITHUB_OWNER"),
		GitHubRepo:  viper.GetString("GITHUB_REPO"),

		AnthropicAPIKey: viper.GetString("ANTHROPIC_API_KEY"),
		AnthropicModel:  viper.GetString("ANTHROPIC_MODEL"),

		AIProvider:    viper.GetString("AI_PROVIDER"),
		OllamaBaseURL: viper.GetString("OLLAMA_BASE_URL"),
		OllamaModel:   viper.GetString("OLLAMA_MODEL"),

		AgentUsername:        viper.GetString("AGENT_USERNAME"),
		PollingInterval:      viper.GetString("POLLING_INTERVAL"),
		MaxConcurrentTickets: viper.GetInt("MAX_CONCURRENT_TICKETS"),
		MaxTicketAttempts:    viper.GetInt("MAX_TICKET_ATTEMPTS"),

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

		AIRunner:              viper.GetString("AI_RUNNER"),
		ClaudeBinary:          viper.GetString("CLAUDE_CODE_BIN"),
		ClaudeModel:           viper.GetString("CLAUDE_MODEL"),
		ClaudeEffort:          viper.GetString("CLAUDE_EFFORT"),
		RunnerTimeout:         viper.GetString("RUNNER_TIMEOUT"),
		RunnerAllowedCommands: splitList(viper.GetString("RUNNER_ALLOWED_COMMANDS")),
		Sandbox:               viper.GetString("SANDBOX"),
		SandboxImage:          viper.GetString("SANDBOX_IMAGE"),
		SandboxNetwork:        viper.GetString("SANDBOX_NETWORK"),
		SandboxPassEnv:        splitList(viper.GetString("SANDBOX_PASS_ENV")),

		ReviewEnabled:   viper.GetBool("REVIEW_ENABLED"),
		MaxReviewRounds: viper.GetInt("MAX_REVIEW_ROUNDS"),
		TestEnabled:     viper.GetBool("TEST_ENABLED"),
		AutoMerge:       viper.GetBool("AUTO_MERGE"),
		MergeMethod:     viper.GetString("MERGE_METHOD"),
		EpicReview:      !viper.IsSet("EPIC_REVIEW") || viper.GetBool("EPIC_REVIEW"),

		VerifyFixAttempts: viper.GetInt("VERIFY_FIX_ATTEMPTS"),
		TicketBudgetUSD:   viper.GetFloat64("TICKET_BUDGET_USD"),
		ProjectBudgetUSD:  viper.GetFloat64("PROJECT_BUDGET_USD"),

		SleepAfterFailures: viper.GetInt("SLEEP_AFTER_FAILURES"),

		DryRun: viper.GetBool("DRY_RUN"),

		MetricsEnabled: viper.GetBool("METRICS_ENABLED"),
		MetricsPort:    viper.GetInt("METRICS_PORT"),
	}

	// Defaults
	if cfg.TicketingMode == "" {
		cfg.TicketingMode = "jira" // Default to JIRA polling for backwards compatibility
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
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
	if cfg.AIRunner == "" {
		cfg.AIRunner = "legacy"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "local"
	}
	if cfg.RunnerAllowedCommands == nil {
		cfg.RunnerAllowedCommands = defaultRunnerAllowedCommands
	}
	if cfg.MaxReviewRounds <= 0 {
		cfg.MaxReviewRounds = 2
	}
	if cfg.MergeMethod == "" {
		cfg.MergeMethod = "squash"
	}
	if cfg.MaxTicketAttempts <= 0 {
		cfg.MaxTicketAttempts = 3
	}
	if !viper.IsSet("SLEEP_AFTER_FAILURES") {
		cfg.SleepAfterFailures = 3
	}
	// Self-healing defaults
	if cfg.SelfHealMaxAttempts <= 0 {
		cfg.SelfHealMaxAttempts = 3 // Default: 3 healing attempts
	}
	// SelfHealEnabled defaults to false (opt-in)
	if !viper.IsSet("VERIFY_FIX_ATTEMPTS") && cfg.SelfHealEnabled {
		cfg.VerifyFixAttempts = cfg.SelfHealMaxAttempts
	}
	// SelfHealOnTests, SelfHealOnVet, SelfHealOnBuild default to false

	// Metrics defaults
	if cfg.MetricsPort <= 0 {
		cfg.MetricsPort = 9090 // Default Prometheus port
	}
	// MetricsEnabled defaults to false (opt-in)
	// "*" disables the allowlist check entirely — see validatePlannedChanges.
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
	// Validate ticketing mode and its required configuration
	switch c.TicketingMode {
	case "jira", "":
		if c.JiraURL == "" {
			return errors.NewConfigMissingError("JIRA_URL")
		}
		if c.JiraEmail == "" {
			return errors.NewConfigMissingError("JIRA_EMAIL")
		}
		if c.JiraAPIToken == "" {
			return errors.NewConfigMissingError("JIRA_API_TOKEN")
		}
		if c.JiraProject == "" {
			return errors.NewConfigMissingError("JIRA_PROJECT_KEY")
		}
		if c.AgentUsername == "" {
			return errors.NewConfigMissingError("AGENT_USERNAME")
		}
		if c.PollingInterval == "" {
			return errors.NewConfigMissingError("POLLING_INTERVAL")
		}
		if _, err := time.ParseDuration(c.PollingInterval); err != nil {
			return errors.NewConfigInvalidError("POLLING_INTERVAL", c.PollingInterval,
				fmt.Sprintf("invalid duration format: %v (use: 30s, 5m, 1h, etc.)", err))
		}
	case "slack":
		if c.SlackBotToken == "" {
			return errors.NewConfigMissingError("SLACK_BOT_TOKEN")
		}
		if c.SlackSigningSecret == "" {
			return errors.NewConfigMissingError("SLACK_SIGNING_SECRET")
		}
	default:
		return errors.NewConfigInvalidError("TICKETING_MODE", c.TicketingMode,
			"supported values: jira, slack")
	}

	if c.SlackBotToken != "" && c.SlackSigningSecret == "" {
		return errors.NewConfigMissingError("SLACK_SIGNING_SECRET")
	}

	// Validate required GitHub configuration
	if c.GitHubToken == "" {
		return errors.NewConfigMissingError("GITHUB_TOKEN")
	}
	if c.GitHubOwner == "" {
		return errors.NewConfigMissingError("GITHUB_OWNER")
	}
	if c.GitHubRepo == "" {
		return errors.NewConfigMissingError("GITHUB_REPO")
	}

	// Validate AI provider configuration
	switch c.AIProvider {
	case "anthropic":
		if c.AnthropicAPIKey == "" {
			return errors.NewConfigMissingError("ANTHROPIC_API_KEY")
		}
	case "ollama":
		if c.OllamaModel == "" {
			return errors.NewConfigMissingError("OLLAMA_MODEL")
		}
		if c.OllamaBaseURL == "" {
			return errors.NewConfigMissingError("OLLAMA_BASE_URL")
		}
	default:
		return errors.NewConfigInvalidError("AI_PROVIDER", c.AIProvider,
			"supported values: anthropic, ollama")
	}

	switch c.AIRunner {
	case "", "legacy":
	case "claude-code":
		if c.RunnerTimeout != "" {
			if _, err := time.ParseDuration(c.RunnerTimeout); err != nil {
				return errors.NewConfigInvalidError("RUNNER_TIMEOUT", c.RunnerTimeout, "must be a duration like 45m")
			}
		}
		switch c.Sandbox {
		case "", "local":
		case "docker":
			if c.SandboxImage == "" {
				return errors.NewConfigMissingError("SANDBOX_IMAGE")
			}
		default:
			return errors.NewConfigInvalidError("SANDBOX", c.Sandbox, "supported values: local, docker")
		}
	default:
		return errors.NewConfigInvalidError("AI_RUNNER", c.AIRunner, "supported values: legacy, claude-code")
	}

	if (c.ReviewEnabled || c.TestEnabled) && c.AIRunner != "claude-code" {
		return errors.NewConfigInvalidError("REVIEW_ENABLED/TEST_ENABLED", true,
			"the reviewer and tester personas require AI_RUNNER=claude-code")
	}
	if c.SlackArchitectChannel != "" && c.AIRunner != "claude-code" {
		return errors.NewConfigInvalidError("SLACK_ARCHITECT_CHANNEL", c.SlackArchitectChannel,
			"the architect persona requires AI_RUNNER=claude-code")
	}
	switch c.MergeMethod {
	case "", "squash", "merge", "rebase":
	default:
		return errors.NewConfigInvalidError("MERGE_METHOD", c.MergeMethod, "supported values: squash, merge, rebase")
	}

	// Validate concurrent tickets
	if c.MaxConcurrentTickets <= 0 {
		return errors.NewConfigInvalidError("MAX_CONCURRENT_TICKETS", c.MaxConcurrentTickets,
			"must be greater than 0")
	}
	if c.MaxConcurrentTickets > 100 {
		return errors.NewConfigInvalidError("MAX_CONCURRENT_TICKETS", c.MaxConcurrentTickets,
			"exceeds reasonable limit of 100 (risk of resource exhaustion)")
	}

	// Validate working directory (if specified)
	if c.WorkingDir != "" {
		if !strings.HasPrefix(c.WorkingDir, "/") && !strings.HasPrefix(c.WorkingDir, "./") && !strings.HasPrefix(c.WorkingDir, "../") {
			// Relative path without ./ prefix - add it for clarity
			c.WorkingDir = "./" + c.WorkingDir
		}
		// Note: We don't check if directory exists yet - it will be created on startup
		// But we can validate it's not a file
		if info, err := os.Stat(c.WorkingDir); err == nil && !info.IsDir() {
			return errors.NewConfigInvalidError("WORKING_DIR", c.WorkingDir,
				"path exists but is not a directory")
		}
	}

	// Validate context cache TTL format
	if c.ContextCacheTTL != "" {
		if _, err := time.ParseDuration(c.ContextCacheTTL); err != nil {
			return errors.NewConfigInvalidError("CONTEXT_CACHE_TTL", c.ContextCacheTTL,
				fmt.Sprintf("invalid duration format: %v", err))
		}
	}

	// Validate file limits
	if c.ContextMaxFiles <= 0 {
		return errors.NewConfigInvalidError("CONTEXT_MAX_FILES", c.ContextMaxFiles,
			"must be greater than 0")
	}
	if c.ContextMaxFiles > 1000 {
		return errors.NewConfigInvalidError("CONTEXT_MAX_FILES", c.ContextMaxFiles,
			"exceeds reasonable limit of 1000 (risk of token limit/timeout)")
	}

	if c.PlanMaxFiles <= 0 {
		return errors.NewConfigInvalidError("PLAN_MAX_FILES", c.PlanMaxFiles,
			"must be greater than 0")
	}
	if c.PlanMaxFiles > 100 {
		return errors.NewConfigInvalidError("PLAN_MAX_FILES", c.PlanMaxFiles,
			"exceeds reasonable limit of 100 (risk of large changesets)")
	}

	if c.ContextMaxBytes <= 0 {
		return errors.NewConfigInvalidError("CONTEXT_MAX_BYTES", c.ContextMaxBytes,
			"must be greater than 0")
	}

	// Validate self-healing configuration for conflicts
	if c.SelfHealEnabled {
		// At least one healing gate must be enabled
		if !c.SelfHealOnTests && !c.SelfHealOnVet && !c.SelfHealOnBuild {
			return errors.NewConfigConflictError(
				[]string{"SELF_HEAL_ENABLED", "SELF_HEAL_ON_TESTS", "SELF_HEAL_ON_VET", "SELF_HEAL_ON_BUILD"},
				"SELF_HEAL_ENABLED=true but no healing gates are enabled (set at least one SELF_HEAL_ON_* to true)")
		}

		// Validate max attempts
		if c.SelfHealMaxAttempts <= 0 {
			return errors.NewConfigInvalidError("SELF_HEAL_MAX_ATTEMPTS", c.SelfHealMaxAttempts,
				"must be greater than 0 when self-healing is enabled")
		}
		if c.SelfHealMaxAttempts > 10 {
			return errors.NewConfigInvalidError("SELF_HEAL_MAX_ATTEMPTS", c.SelfHealMaxAttempts,
				"exceeds reasonable limit of 10 (risk of excessive AI costs)")
		}
	}

	// Validate quality gates - if self-healing is on a specific gate, the gate itself should be enabled
	if c.SelfHealOnTests && !c.RunTestsBeforePR {
		// This is actually okay - self-healing can run tests even if not required before PR
		// Just log a warning in this case (we'll add logging later)
	}
	if c.SelfHealOnVet && !c.RunVetBeforePR {
		// Same as above - okay to heal vet errors even if not required before PR
	}

	// Validate metrics configuration
	if c.MetricsEnabled {
		if c.MetricsPort < 1024 || c.MetricsPort > 65535 {
			return errors.NewConfigInvalidError("METRICS_PORT", c.MetricsPort,
				"must be between 1024 and 65535 (avoid privileged ports)")
		}
	}

	// Validate allowed write directories
	if len(c.AllowedWriteDirs) == 0 {
		return errors.NewConfigInvalidError("ALLOWED_WRITE_DIRS", "",
			"must specify at least one allowed directory")
	}

	// Check for potential security issues
	for _, dir := range c.AllowedWriteDirs {
		if dir == "/" {
			return errors.NewConfigInvalidError("ALLOWED_WRITE_DIRS", dir,
				"allowing writes to root directory (/) is not permitted for security reasons")
		}
		if strings.Contains(dir, "..") {
			return errors.NewConfigInvalidError("ALLOWED_WRITE_DIRS", dir,
				"path traversal patterns (..) are not allowed")
		}
	}

	return nil
}

// defaultRunnerAllowedCommands are the Bash prefixes a local-sandbox
// agent may run: build and test tooling for common stacks, nothing that
// touches remotes or deletes outside the repo.
var defaultRunnerAllowedCommands = []string{
	"go build", "go test", "go vet", "go mod", "go run", "go generate", "gofmt",
	"npm", "npx", "pnpm", "yarn", "node",
	"python -m pytest", "pytest", "pip install",
	"make", "mkdir", "touch", "mv", "cp",
}

// splitList parses a comma-separated env value; empty input returns nil.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
