package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/gate"
	"intern/internal/runner"
	"intern/internal/runner/claudecode"
)

// TestE2E_ClaudeCodeDeveloper runs the real Claude Code CLI through the
// developer pipeline on a tiny Go repo: run, gate, commit, verify.
// It spends real money, so it only runs when INTERN_E2E_CLAUDE=1.
// INTERN_E2E_MODEL picks the model (default "haiku").
func TestE2E_ClaudeCodeDeveloper(t *testing.T) {
	if os.Getenv("INTERN_E2E_CLAUDE") != "1" {
		t.Skip("set INTERN_E2E_CLAUDE=1 to run against the real Claude Code CLI")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed")
	}
	model := os.Getenv("INTERN_E2E_MODEL")
	if model == "" {
		model = "haiku"
	}

	dir := initRepo(t)
	writeFile(t, dir, "go.mod", "module example.com/calc\n\ngo 1.21\n")
	writeFile(t, dir, ".intern/project.yaml", "verify:\n  - name: test\n    run: go test ./...\nprotected_paths: [\".intern/\"]\n")
	cmd := exec.Command("sh", "-c", "git add . && git commit -q -m contract")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %s", out)
	}
	base, err := gate.HeadSHA(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	r := claudecode.New(claudecode.Config{
		Model:            model,
		APIKey:           os.Getenv("ANTHROPIC_API_KEY"),
		DefaultBudgetUSD: 1.0,
		Timeout:          10 * time.Minute,
	})
	c := pipelineCoordinator(t, dir, r, &config.Config{VerifyFixAttempts: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	out, err := c.developAndVerify(ctx, runner.Job{
		Role:        runner.RoleDeveloper,
		TicketKey:   "CALC-1",
		Summary:     "Add an Add function",
		Description: "Create package calc in calc/calc.go with func Add(a, b int) int that returns the sum, plus a table-driven test in calc/calc_test.go.",
		Workdir:     dir,
	}, base)
	t.Logf("summary: %s", out.Result.Summary)
	t.Logf("notes: %v", out.Notes)
	t.Logf("cost: $%.4f, turns: %d", out.Result.Usage.CostUSD, out.Result.Turns)
	if err != nil {
		t.Fatalf("developAndVerify: %v", err)
	}
	var paths []string
	for _, ch := range out.Changes {
		paths = append(paths, ch.Path)
	}
	if !strings.Contains(strings.Join(paths, " "), "calc/calc.go") {
		t.Errorf("expected calc/calc.go in changes, got %v", paths)
	}
	if len(out.Checks) == 0 || !out.Checks[len(out.Checks)-1].Passed {
		t.Errorf("verify did not pass: %+v", out.Checks)
	}
}
