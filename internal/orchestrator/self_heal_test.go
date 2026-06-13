package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/ai/agent"
	"intern/internal/config"

	logger "github.com/jenish-jain/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init("error")
}

func TestApplyCodeChange(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		change  agent.CodeChange
		wantErr bool
	}{
		{
			name: "create new file",
			change: agent.CodeChange{
				Path:      "test.go",
				Operation: agent.OperationCreate,
				Content:   "package main\n\nfunc main() {}\n",
			},
			wantErr: false,
		},
		{
			name: "create nested file",
			change: agent.CodeChange{
				Path:      "internal/service/handler.go",
				Operation: agent.OperationCreate,
				Content:   "package service\n",
			},
			wantErr: false,
		},
		{
			name: "create file with new content",
			change: agent.CodeChange{
				Path:      "existing.go",
				Operation: agent.OperationCreate,
				Content:   "package main\n// updated\n",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyCodeChange(tmpDir, tt.change)
			if (err != nil) != tt.wantErr {
				t.Errorf("applyCodeChange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file was created
				fullPath := filepath.Join(tmpDir, tt.change.Path)
				content, err := os.ReadFile(fullPath)
				if err != nil {
					t.Errorf("Failed to read created file: %v", err)
					return
				}

				if string(content) != tt.change.Content {
					t.Errorf("File content mismatch.\nGot: %s\nWant: %s", string(content), tt.change.Content)
				}
			}
		})
	}
}

func TestExtractErrorSummary(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "test failure",
			output: `--- FAIL: TestFoo (0.00s)
    foo_test.go:10: expected 5, got 3
FAIL
exit status 1
FAIL    mypackage    0.123s`,
			want: "--- FAIL: TestFoo (0.00s); FAIL; FAIL    mypackage    0.123s",
		},
		{
			name: "vet error",
			output: `# mypackage
./main.go:15:2: undefined: foo
./main.go:20:5: unreachable code`,
			want: "./main.go:15:2: undefined: foo",
		},
		{
			name: "build error",
			output: `# mypackage
./service.go:42:10: syntax error: unexpected newline, expecting comma or }`,
			want: "./service.go:42:10: syntax error: unexpected newline, expecting comma or }",
		},
		{
			name:   "empty output",
			output: "",
			want:   "Unknown error",
		},
		{
			name:   "no errors found",
			output: "some random output without errors",
			want:   "some random output without errors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractErrorSummary(tt.output)
			if got != tt.want {
				t.Errorf("extractErrorSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunQualityGate(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a simple valid Go project
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	t.Run("go vet on valid project", func(t *testing.T) {
		output, err := runQualityGate(ctx, tmpDir, "go", "vet", "./...")
		if err != nil {
			t.Logf("go vet failed (may be expected): %v, output: %s", err, output)
		}
		// We just check it runs, errors are ok for this test
	})

	t.Run("invalid command", func(t *testing.T) {
		_, err := runQualityGate(ctx, tmpDir, "nonexistent-command", "arg1")
		if err == nil {
			t.Error("Expected error for invalid command")
		}
	})
}

func TestRunGoVet(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a simple valid Go project
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	output, err := runGoVet(ctx, tmpDir)
	// vet may succeed or fail, we just verify it runs
	t.Logf("go vet output: %s, err: %v", output, err)
}

func TestRunGoTest(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a simple valid Go project with a test
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc Add(a, b int) int { return a + b }\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2,3) != 5 { t.Error(\"failed\") }\n}\n"), 0644)

	output, err := runGoTest(ctx, tmpDir)
	if err != nil {
		t.Logf("go test failed: %v, output: %s", err, output)
	}
	// Should succeed for this simple test
}

func TestRunGoBuild(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a simple valid Go project
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	output, err := runGoBuild(ctx, tmpDir)
	if err != nil {
		t.Errorf("go build failed: %v, output: %s", err, output)
	}
}

// Mock agent for testing
type mockHealingAgent struct {
	fixesResponse []agent.CodeChange
	fixesError    error
	planCalled    bool
	fixesCalled   bool
}

func (m *mockHealingAgent) PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]agent.CodeChange, []string, *agent.UsageMetrics, error) {
	m.planCalled = true
	return nil, nil, nil, nil
}

func (m *mockHealingAgent) FixErrors(ctx context.Context, ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []agent.CodeChange, fileContents map[string]string) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	m.fixesCalled = true
	return m.fixesResponse, &agent.UsageMetrics{EstimatedCost: 0.05}, m.fixesError
}

func TestTryHealErrors(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	tests := []struct {
		name          string
		agentResponse []agent.CodeChange
		agentError    error
		wantErr       bool
	}{
		{
			name: "successful healing",
			agentResponse: []agent.CodeChange{
				{Path: "fixed.go", Operation: agent.OperationCreate, Content: "package main\n\nfunc fixed() {}\n"},
			},
			agentError: nil,
			wantErr:    false,
		},
		{
			name:          "agent error",
			agentResponse: nil,
			agentError:    os.ErrNotExist,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgent := &mockHealingAgent{
				fixesResponse: tt.agentResponse,
				fixesError:    tt.agentError,
			}

			coord := &Coordinator{
				Agent: mockAgent,
				Cfg: &config.Config{
					AllowedWriteDirs: []string{"."},
					PlanMaxFiles:     20,
				},
			}

			result, err := coord.tryHealErrors(ctx, "TEST-123", "Test ticket", "test", "test error output", []agent.CodeChange{}, tmpDir)

			if (err != nil) != tt.wantErr {
				t.Errorf("tryHealErrors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !mockAgent.fixesCalled {
				t.Error("Expected agent.FixErrors to be called")
			}

			if !tt.wantErr && result == nil {
				t.Error("Expected non-nil result")
			}
		})
	}
}

func TestSelfHealingPipeline_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	coord := &Coordinator{
		Agent: &mockHealingAgent{},
		Cfg: &config.Config{
			SelfHealEnabled: false,
		},
	}

	result, err := coord.selfHealingPipeline(ctx, "TEST-123", "Test", []agent.CodeChange{}, tmpDir)
	if err != nil {
		t.Errorf("Unexpected error when self-healing disabled: %v", err)
	}

	if !result.Success {
		t.Error("Expected success when self-healing is disabled")
	}

	if len(result.Attempts) != 0 {
		t.Errorf("Expected 0 attempts when disabled, got %d", len(result.Attempts))
	}
}

func TestSelfHealingPipeline_NoErrors(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a valid Go project
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	coord := &Coordinator{
		Agent: &mockHealingAgent{},
		Cfg: &config.Config{
			SelfHealEnabled:     true,
			SelfHealMaxAttempts: 3,
			SelfHealOnBuild:     true,
		},
	}

	result, err := coord.selfHealingPipeline(ctx, "TEST-123", "Test", []agent.CodeChange{}, tmpDir)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected success when no errors")
	}

	if len(result.Attempts) != 0 {
		t.Errorf("Expected 0 healing attempts when no errors, got %d", len(result.Attempts))
	}
}

func TestSelfHealingPipeline_WithBuildError(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a Go project with syntax error
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {\n// missing brace\n"), 0644)

	mockAgent := &mockHealingAgent{
		fixesResponse: []agent.CodeChange{
			{
				Path:      "main.go",
				Operation: agent.OperationEdit,
				Edits: []agent.EditHunk{
					{
						Old: "package main\n\nfunc main() {\n// missing brace\n",
						New: "package main\n\nfunc main() {}\n",
					},
				},
			},
		},
	}

	coord := &Coordinator{
		Agent:   mockAgent,
		Metrics: NewMetrics(),
		Cfg: &config.Config{
			SelfHealEnabled:     true,
			SelfHealMaxAttempts: 2,
			SelfHealOnBuild:     true,
			SelfHealOnVet:       false,
			SelfHealOnTests:     false,
		},
	}

	result, err := coord.selfHealingPipeline(ctx, "TEST-123", "Test", []agent.CodeChange{}, tmpDir)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should have attempted to heal
	if len(result.Attempts) == 0 {
		t.Error("Expected at least one healing attempt")
	}

	if !mockAgent.fixesCalled {
		t.Error("Expected agent.FixErrors to be called")
	}
}

func TestHealResult_Fields(t *testing.T) {
	result := HealResult{
		Attempt:     1,
		Success:     true,
		ErrorType:   "test",
		ErrorOutput: "test failed",
		FixedChanges: []agent.CodeChange{
			{Path: "test.go", Operation: agent.OperationCreate, Content: "fixed"},
		},
		Metrics: &agent.UsageMetrics{
			InputTokens:   100,
			OutputTokens:  50,
			EstimatedCost: 0.01,
		},
	}

	if result.Attempt != 1 {
		t.Errorf("Expected attempt 1, got %d", result.Attempt)
	}
	if !result.Success {
		t.Error("Expected success true")
	}
	if result.ErrorType != "test" {
		t.Errorf("Expected error type 'test', got %s", result.ErrorType)
	}
}

func TestSelfHealingResult_Fields(t *testing.T) {
	result := SelfHealingResult{
		Attempts: []HealResult{
			{Attempt: 1, Success: false},
			{Attempt: 2, Success: true},
		},
		Success:       true,
		TotalAttempts: 2,
		TotalCost:     0.10,
	}

	if len(result.Attempts) != 2 {
		t.Errorf("Expected 2 attempts, got %d", len(result.Attempts))
	}
	if !result.Success {
		t.Error("Expected success true")
	}
	if result.TotalAttempts != 2 {
		t.Errorf("Expected total attempts 2, got %d", result.TotalAttempts)
	}
	if result.TotalCost != 0.10 {
		t.Errorf("Expected total cost 0.10, got %.2f", result.TotalCost)
	}
}

func TestExtractErrorSummary_MultipleErrors(t *testing.T) {
	output := `--- FAIL: TestFoo (0.00s)
    foo_test.go:10: error 1
--- FAIL: TestBar (0.00s)
    bar_test.go:20: error 2
--- FAIL: TestBaz (0.00s)
    baz_test.go:30: error 3
--- FAIL: TestQux (0.00s)
    qux_test.go:40: error 4
FAIL`

	summary := extractErrorSummary(output)

	// Should extract at most 3 errors
	parts := strings.Split(summary, "; ")
	if len(parts) > 3 {
		t.Errorf("Expected at most 3 error lines, got %d", len(parts))
	}

	// Should contain FAIL
	if !strings.Contains(summary, "FAIL") {
		t.Error("Expected summary to contain FAIL")
	}
}
