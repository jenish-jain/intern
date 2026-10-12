package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/ai/agent"
	"intern/internal/config"
	"intern/internal/runner"

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


// scriptedAgent returns canned plans and fixes and records what it saw.
type scriptedAgent struct {
	plan        []agent.CodeChange
	fixes       []agent.CodeChange
	fixPrevious []agent.CodeChange
	fixFiles    map[string]string
	fixOutput   string
}

func (a *scriptedAgent) PlanChanges(context.Context, string, string, string, string) ([]agent.CodeChange, []string, *agent.UsageMetrics, error) {
	return a.plan, nil, &agent.UsageMetrics{InputTokens: 100, OutputTokens: 10, EstimatedCost: 0.01}, nil
}

func (a *scriptedAgent) FixErrors(_ context.Context, _, _, _, errorOutput string, previous []agent.CodeChange, files map[string]string) ([]agent.CodeChange, *agent.UsageMetrics, error) {
	a.fixOutput, a.fixPrevious, a.fixFiles = errorOutput, previous, files
	return a.fixes, &agent.UsageMetrics{InputTokens: 50, OutputTokens: 5, EstimatedCost: 0.005}, nil
}

func TestLegacyRunner_PlanThenFix(t *testing.T) {
	dir := t.TempDir()
	ag := &scriptedAgent{
		plan: []agent.CodeChange{
			{Path: "internal/a.go", Operation: agent.OperationCreate, Content: "package internal\n", Note: "named it a.go"},
		},
		fixes: []agent.CodeChange{
			{Path: "internal/a.go", Operation: agent.OperationEdit, Edits: []agent.EditHunk{{Old: "package internal\n", New: "package internal\n\nfunc A() {}\n"}}},
		},
	}
	c := &Coordinator{
		Agent:   ag,
		Cfg:     &config.Config{AllowedWriteDirs: []string{"internal"}, PlanMaxFiles: 10, ContextMaxFiles: 5, ContextMaxBytes: 1024},
		Metrics: NewMetrics(),
	}
	r := newLegacyRunner(c)
	job := runner.Job{Role: runner.RoleDeveloper, TicketKey: "P-1", Summary: "add A", Workdir: dir}

	res, err := r.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if res.Status != runner.StatusDone || res.Usage.CostUSD != 0.01 {
		t.Errorf("unexpected plan result %+v", res)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "named it a.go") {
		t.Errorf("notes = %v", res.Notes)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/a.go")); err != nil {
		t.Fatalf("planned file not written: %v", err)
	}

	job.Feedback = "internal/a.go:3:1: missing function A"
	res, err = r.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	if len(ag.fixPrevious) != 1 || ag.fixPrevious[0].Path != "internal/a.go" {
		t.Errorf("fix did not receive previous changes: %+v", ag.fixPrevious)
	}
	if _, ok := ag.fixFiles["internal/a.go"]; !ok {
		t.Errorf("fix did not receive current file content: %v", ag.fixFiles)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "internal/a.go"))
	if !strings.Contains(string(data), "func A()") {
		t.Errorf("fix not applied: %q", data)
	}
}

func TestLegacyRunner_RejectsOtherRoles(t *testing.T) {
	r := newLegacyRunner(&Coordinator{})
	if _, err := r.Run(context.Background(), runner.Job{Role: runner.RoleReviewer}); err == nil {
		t.Error("expected error for non-developer role")
	}
}

func TestCollectFileContents_UsesErrorPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg/b.go"), []byte("package pkg\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := collectFileContents(dir, nil, "./pkg/b.go:1:1: oops\n../../etc/passwd.go:1: nope")
	if len(files) != 1 || files["pkg/b.go"] != "package pkg\n" {
		t.Errorf("files = %v", files)
	}
}
