package commands

import (
	"os"

	logger "github.com/jenish-jain/logger"
	"github.com/spf13/cobra"
)

// InitCmd represents the init command
var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize sample configuration files",
	Long:  `Create sample config.yaml, .env.example, and agent_state.jsonc files.`,
	RunE:  initConfig,
}

func initConfig(cmd *cobra.Command, args []string) error {
	logger.Info("Creating sample configuration files...")

	envContent := `JIRA_URL="https://company.atlassian.net"
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
`

	if err := os.WriteFile(".env.example", []byte(envContent), 0644); err != nil {
		logger.Error("Failed to write .env.example: %v", err)
		return err
	}

	stateContent := `{"processed":{}}`
	if err := os.WriteFile("agent_state.jsonc", []byte(stateContent), 0644); err != nil {
		logger.Error("Failed to write agent_state.jsonc: %v", err)
		return err
	}

	logger.Info("Sample config.yaml, .env.example, and agent_state.jsonc created.")
	return nil
}
