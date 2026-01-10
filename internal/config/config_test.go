package config

import (
	"os"
	"strings"
	"testing"
)

// Helper to create a valid base config for testing
func validConfig() *Config {
	return &Config{
		JiraURL:      "https://test.atlassian.net",
		JiraEmail:    "test@example.com",
		JiraAPIToken: "test-token",
		JiraProject:  "TEST",
		JiraTransitions: map[string]string{
			"To Do":       "11",
			"In Progress": "21",
			"Done":        "31",
		},
		GitHubToken:          "ghp_test123",
		GitHubOwner:          "testorg",
		GitHubRepo:           "testrepo",
		AIProvider:           "anthropic",
		AnthropicAPIKey:      "sk-test123",
		AgentUsername:        "ai-intern",
		PollingInterval:      "30s",
		MaxConcurrentTickets: 5,
		WorkingDir:           "./workspace",
		BaseBranch:           "main",
		BranchPrefix:         "feature/",
		ContextMaxFiles:      40,
		ContextMaxBytes:      32768,
		ContextCacheTTL:      "1h",
		PlanMaxFiles:         20,
		AllowedWriteDirs:     []string{"internal", "cmd", "pkg"},
		SelfHealMaxAttempts:  3,
		MetricsPort:          9090,
	}
}

func TestConfig_Validate_Success(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not fail validation: %v", err)
	}
}

func TestConfig_Validate_MissingJiraURL(t *testing.T) {
	cfg := validConfig()
	cfg.JiraURL = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with missing JIRA_URL")
	}
	if !strings.Contains(err.Error(), "JIRA_URL") {
		t.Errorf("Error should mention JIRA_URL, got: %v", err)
	}
}

func TestConfig_Validate_MissingGitHubToken(t *testing.T) {
	cfg := validConfig()
	cfg.GitHubToken = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with missing GITHUB_TOKEN")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("Error should mention GITHUB_TOKEN, got: %v", err)
	}
}

func TestConfig_Validate_InvalidAIProvider(t *testing.T) {
	cfg := validConfig()
	cfg.AIProvider = "openai" // Not supported yet
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with unsupported AI provider")
	}
	if !strings.Contains(err.Error(), "AI_PROVIDER") {
		t.Errorf("Error should mention AI_PROVIDER, got: %v", err)
	}
}

func TestConfig_Validate_OllamaProvider(t *testing.T) {
	cfg := validConfig()
	cfg.AIProvider = "ollama"
	cfg.AnthropicAPIKey = "" // Not needed for Ollama
	cfg.OllamaModel = "qwen2.5-coder:7b"
	cfg.OllamaBaseURL = "http://localhost:11434"

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid Ollama config should not fail: %v", err)
	}
}

func TestConfig_Validate_OllaMissingModel(t *testing.T) {
	cfg := validConfig()
	cfg.AIProvider = "ollama"
	cfg.AnthropicAPIKey = "" // Not needed
	cfg.OllamaModel = ""      // Missing!
	cfg.OllamaBaseURL = "http://localhost:11434"

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with missing Ollama model")
	}
	if !strings.Contains(err.Error(), "OLLAMA_MODEL") {
		t.Errorf("Error should mention OLLAMA_MODEL, got: %v", err)
	}
}

func TestConfig_Validate_InvalidPollingInterval(t *testing.T) {
	cfg := validConfig()
	cfg.PollingInterval = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with invalid polling interval format")
	}
	if !strings.Contains(err.Error(), "POLLING_INTERVAL") {
		t.Errorf("Error should mention POLLING_INTERVAL, got: %v", err)
	}
}

func TestConfig_Validate_ValidPollingIntervals(t *testing.T) {
	validIntervals := []string{"1s", "30s", "5m", "1h", "2h30m"}
	cfg := validConfig()

	for _, interval := range validIntervals {
		cfg.PollingInterval = interval
		if err := cfg.Validate(); err != nil {
			t.Errorf("Valid interval %s should not fail: %v", interval, err)
		}
	}
}

func TestConfig_Validate_MaxConcurrentTicketsZero(t *testing.T) {
	cfg := validConfig()
	cfg.MaxConcurrentTickets = 0
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with MaxConcurrentTickets = 0")
	}
}

func TestConfig_Validate_MaxConcurrentTicketsTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.MaxConcurrentTickets = 101
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with MaxConcurrentTickets > 100")
	}
	if !strings.Contains(err.Error(), "100") {
		t.Errorf("Error should mention limit, got: %v", err)
	}
}

func TestConfig_Validate_WorkingDirIsFile(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test_file_*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := validConfig()
	cfg.WorkingDir = tmpFile.Name()
	err = cfg.Validate()
	if err == nil {
		t.Error("Should fail when WorkingDir is a file, not a directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Error should mention it's not a directory, got: %v", err)
	}
}

func TestConfig_Validate_InvalidCacheTTL(t *testing.T) {
	cfg := validConfig()
	cfg.ContextCacheTTL = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with invalid cache TTL")
	}
	if !strings.Contains(err.Error(), "CONTEXT_CACHE_TTL") {
		t.Errorf("Error should mention CONTEXT_CACHE_TTL, got: %v", err)
	}
}

func TestConfig_Validate_ContextMaxFilesTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.ContextMaxFiles = 1001
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with ContextMaxFiles > 1000")
	}
	if !strings.Contains(err.Error(), "1000") {
		t.Errorf("Error should mention limit, got: %v", err)
	}
}

func TestConfig_Validate_PlanMaxFilesTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.PlanMaxFiles = 101
	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with PlanMaxFiles > 100")
	}
}

func TestConfig_Validate_SelfHealConflict(t *testing.T) {
	cfg := validConfig()
	cfg.SelfHealEnabled = true
	// All gates disabled - should fail
	cfg.SelfHealOnTests = false
	cfg.SelfHealOnVet = false
	cfg.SelfHealOnBuild = false

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail when self-healing enabled but no gates enabled")
	}
	if !strings.Contains(err.Error(), "SELF_HEAL") {
		t.Errorf("Error should mention SELF_HEAL, got: %v", err)
	}
}

func TestConfig_Validate_SelfHealWithOneGate(t *testing.T) {
	cfg := validConfig()
	cfg.SelfHealEnabled = true
	cfg.SelfHealOnTests = true
	cfg.SelfHealOnVet = false
	cfg.SelfHealOnBuild = false

	if err := cfg.Validate(); err != nil {
		t.Errorf("Should succeed with one gate enabled: %v", err)
	}
}

func TestConfig_Validate_SelfHealMaxAttemptsTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.SelfHealEnabled = true
	cfg.SelfHealOnTests = true
	cfg.SelfHealMaxAttempts = 11

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with SelfHealMaxAttempts > 10")
	}
	if !strings.Contains(err.Error(), "10") {
		t.Errorf("Error should mention limit, got: %v", err)
	}
}

func TestConfig_Validate_MetricsPortTooLow(t *testing.T) {
	cfg := validConfig()
	cfg.MetricsEnabled = true
	cfg.MetricsPort = 80 // Privileged port

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with privileged port")
	}
	if !strings.Contains(err.Error(), "1024") {
		t.Errorf("Error should mention port limit, got: %v", err)
	}
}

func TestConfig_Validate_MetricsPortTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.MetricsEnabled = true
	cfg.MetricsPort = 70000

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with port > 65535")
	}
}

func TestConfig_Validate_EmptyAllowedWriteDirs(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedWriteDirs = []string{}

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with empty AllowedWriteDirs")
	}
}

func TestConfig_Validate_AllowedWriteDirsRootDir(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedWriteDirs = []string{"/"}

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail when allowing writes to root directory")
	}
	if !strings.Contains(err.Error(), "root directory") {
		t.Errorf("Error should mention root directory, got: %v", err)
	}
}

func TestConfig_Validate_AllowedWriteDirsPathTraversal(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedWriteDirs = []string{"internal", "../etc"}

	err := cfg.Validate()
	if err == nil {
		t.Error("Should fail with path traversal in AllowedWriteDirs")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("Error should mention path traversal, got: %v", err)
	}
}

func TestConfig_Validate_WorkingDirNormalization(t *testing.T) {
	cfg := validConfig()
	cfg.WorkingDir = "workspace" // No ./ prefix

	if err := cfg.Validate(); err != nil {
		t.Errorf("Should succeed and normalize path: %v", err)
	}

	if !strings.HasPrefix(cfg.WorkingDir, "./") {
		t.Errorf("WorkingDir should be normalized to ./workspace, got: %s", cfg.WorkingDir)
	}
}

func TestConfig_Validate_AllRequiredFieldsValidated(t *testing.T) {
	// Test that all critical config fields are validated
	requiredFields := []struct {
		name     string
		mutate   func(*Config)
		errorKey string
	}{
		{"JIRA_URL", func(c *Config) { c.JiraURL = "" }, "JIRA_URL"},
		{"JIRA_EMAIL", func(c *Config) { c.JiraEmail = "" }, "JIRA_EMAIL"},
		{"JIRA_API_TOKEN", func(c *Config) { c.JiraAPIToken = "" }, "JIRA_API_TOKEN"},
		{"JIRA_PROJECT_KEY", func(c *Config) { c.JiraProject = "" }, "JIRA_PROJECT_KEY"},
		{"GITHUB_TOKEN", func(c *Config) { c.GitHubToken = "" }, "GITHUB_TOKEN"},
		{"GITHUB_OWNER", func(c *Config) { c.GitHubOwner = "" }, "GITHUB_OWNER"},
		{"GITHUB_REPO", func(c *Config) { c.GitHubRepo = "" }, "GITHUB_REPO"},
		{"AGENT_USERNAME", func(c *Config) { c.AgentUsername = "" }, "AGENT_USERNAME"},
		{"POLLING_INTERVAL", func(c *Config) { c.PollingInterval = "" }, "POLLING_INTERVAL"},
		{"ANTHROPIC_API_KEY", func(c *Config) { c.AnthropicAPIKey = "" }, "ANTHROPIC_API_KEY"},
	}

	for _, tc := range requiredFields {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Errorf("Validation should fail for missing %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errorKey) {
				t.Errorf("Error should mention %s, got: %v", tc.errorKey, err)
			}
		})
	}
}

func TestConfig_Validate_EdgeCases(t *testing.T) {
	t.Run("exactly_100_concurrent_tickets", func(t *testing.T) {
		cfg := validConfig()
		cfg.MaxConcurrentTickets = 100
		if err := cfg.Validate(); err != nil {
			t.Errorf("Should allow exactly 100 concurrent tickets: %v", err)
		}
	})

	t.Run("exactly_1000_context_files", func(t *testing.T) {
		cfg := validConfig()
		cfg.ContextMaxFiles = 1000
		if err := cfg.Validate(); err != nil {
			t.Errorf("Should allow exactly 1000 context files: %v", err)
		}
	})

	t.Run("exactly_100_plan_files", func(t *testing.T) {
		cfg := validConfig()
		cfg.PlanMaxFiles = 100
		if err := cfg.Validate(); err != nil {
			t.Errorf("Should allow exactly 100 plan files: %v", err)
		}
	})

	t.Run("port_1024", func(t *testing.T) {
		cfg := validConfig()
		cfg.MetricsEnabled = true
		cfg.MetricsPort = 1024
		if err := cfg.Validate(); err != nil {
			t.Errorf("Should allow port 1024: %v", err)
		}
	})

	t.Run("port_65535", func(t *testing.T) {
		cfg := validConfig()
		cfg.MetricsEnabled = true
		cfg.MetricsPort = 65535
		if err := cfg.Validate(); err != nil {
			t.Errorf("Should allow port 65535: %v", err)
		}
	})
}
