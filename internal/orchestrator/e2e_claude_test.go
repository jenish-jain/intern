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

// TestE2E_ClaudeCodeReviewerAndTester runs the real reviewer and tester
// personas on a branch with a planted bug. Opt-in like the developer test.
func TestE2E_ClaudeCodeReviewerAndTester(t *testing.T) {
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
	sh := func(script string) {
		t.Helper()
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", script, out)
		}
	}
	writeFile(t, dir, "go.mod", "module example.com/calc\n\ngo 1.21\n")
	writeFile(t, dir, ".intern/project.yaml", "verify:\n  - name: test\n    run: go test ./...\n")
	sh("git add . && git commit -q -m contract && git branch -M master && git checkout -q -b feature/CALC-2")
	// Planted bug: Max returns the smaller value; the developer's test misses it.
	writeFile(t, dir, "calc/max.go", "package calc\n\n// Max returns the larger of a and b.\nfunc Max(a, b int) int {\n\tif a < b {\n\t\treturn a\n\t}\n\treturn b\n}\n")
	writeFile(t, dir, "calc/max_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestMaxEqual(t *testing.T) {\n\tif Max(2, 2) != 2 {\n\t\tt.Fatal(\"want 2\")\n\t}\n}\n")
	sh("git add . && git commit -q -m 'add Max'")

	r := claudecode.New(claudecode.Config{Model: model, APIKey: os.Getenv("ANTHROPIC_API_KEY"), DefaultBudgetUSD: 1.0, Timeout: 10 * time.Minute})
	job := runner.Job{
		TicketKey: "CALC-2", Summary: "Add Max",
		Description: "Add func Max(a, b int) int in package calc.\n\nAcceptance criteria:\n- Max(3, 7) == 7\n- Max(7, 3) == 7\n- Max(-1, -5) == -1",
		Workdir:     dir, BaseBranch: "master",
	}
	ctx := context.Background()

	job.Role = runner.RoleReviewer
	rev, err := r.Run(ctx, job)
	t.Logf("review: status=%s approve=%v comments=%d cost=$%.4f summary=%s", rev.Status, rev.Review != nil && rev.Review.Approve, func() int {
		if rev.Review == nil {
			return 0
		}
		return len(rev.Review.Comments)
	}(), rev.Usage.CostUSD, rev.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Review == nil || rev.Review.Approve {
		t.Errorf("reviewer should request changes for the inverted comparison")
	}
	if out, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("reviewer modified the workspace:\n%s", out)
	}

	job.Role = runner.RoleTester
	tst, err := r.Run(ctx, job)
	t.Logf("test: status=%s passed=%v failures=%v cost=$%.4f", tst.Status, tst.Test != nil && tst.Test.Passed, func() []string {
		if tst.Test == nil {
			return nil
		}
		return tst.Test.Failures
	}(), tst.Usage.CostUSD)
	if err != nil {
		t.Fatal(err)
	}
	if tst.Test == nil || tst.Test.Passed {
		t.Errorf("tester should report the acceptance criteria failing")
	}
}
