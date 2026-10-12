package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/runner"
	"intern/internal/sandbox"

	logger "github.com/jenish-jain/logger"
)

func TestMain(m *testing.M) {
	logger.Init("error")
	os.Exit(m.Run())
}

// fakeCLI writes a script that records its arguments (one per line) and
// prints stdout, then exits with code.
func fakeCLI(t *testing.T, stdout string, code int) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	outFile := filepath.Join(dir, "out")
	if err := os.WriteFile(outFile, []byte(stdout), 0644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + argsFile + "'; done\n" +
		"env > '" + argsFile + ".env'\ncat '" + outFile + "'\necho 'some warning' >&2\nexit " + string(rune('0'+code)) + "\n"
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func readArgs(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func devJob(dir string) runner.Job {
	return runner.Job{Role: runner.RoleDeveloper, TicketKey: "P-1", Summary: "Add login", Description: "Users log in with email.", Workdir: dir}
}

func TestRun_ParsesRealResultEvent(t *testing.T) {
	bin, argsFile := fakeCLI(t, fixture(t, "success.jsonl"), 0)
	r := New(Config{Binary: bin, Model: "opus", APIKey: "sk-test", DefaultBudgetUSD: 5, AllowedCommands: []string{"go test"}})

	res, err := r.Run(context.Background(), devJob(t.TempDir()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != runner.StatusDone || !strings.Contains(res.Summary, "Appended the line") {
		t.Errorf("unexpected result %+v", res)
	}
	if res.SessionID != "1eabf12c-31ed-4d49-9dac-b6e5a1c83159" || res.Turns != 4 {
		t.Errorf("session/turns not parsed: %+v", res)
	}
	if res.Usage.CostUSD <= 0 || res.Usage.InputTokens != 8+18216+35984 || res.Usage.OutputTokens != 362 {
		t.Errorf("usage not parsed: %+v", res.Usage)
	}

	args := readArgs(t, argsFile)
	for _, want := range []string{
		"-p\nTicket P-1: Add login", "--output-format\nstream-json", "--json-schema", "--setting-sources\nproject",
		"--model\nopus", "--max-budget-usd\n5.00", "--permission-mode\nacceptEdits", "Bash(go test:*)", "--disallowedTools\nBash(git push:*)",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q", want)
		}
	}
	if strings.Contains(args, "--dangerously-skip-permissions") || strings.Contains(args, "--resume") {
		t.Error("unexpected flags for a fresh job in a local sandbox")
	}
	env := readArgs(t, argsFile+".env")
	if !strings.Contains(env, "ANTHROPIC_API_KEY=sk-test") {
		t.Error("API key not passed to the CLI")
	}
}

func resultLine(fields string) string {
	return `{"type":"system","subtype":"init","session_id":"s9"}` + "\n" +
		`{"type":"result","session_id":"s9","num_turns":3,"total_cost_usd":1.25,"usage":{"input_tokens":10,"output_tokens":20},` + fields + "}\n"
}

func TestRun_StatusMapping(t *testing.T) {
	cases := []struct {
		name      string
		out       string
		status    runner.Status
		summary   string
		questions int
	}{
		{"needs human", resultLine(`"subtype":"success","is_error":false,"structured_output":{"status":"needs_human","summary":"blocked","questions":["Which DB?"]}`), runner.StatusNeedsHuman, "blocked", 1},
		{"model reports failure", resultLine(`"subtype":"success","is_error":false,"structured_output":{"status":"failed","summary":"cannot build"}`), runner.StatusFailed, "cannot build", 0},
		{"budget exhausted", resultLine(`"subtype":"error_max_budget_usd","is_error":true,"terminal_reason":"budget","result":""`), runner.StatusFailed, "error_max_budget_usd (budget)", 0},
		{"no structured output", resultLine(`"subtype":"success","is_error":false,"result":"I am done"`), runner.StatusFailed, "without the structured result", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := fakeCLI(t, tc.out, 0)
			res, err := New(Config{Binary: bin}).Run(context.Background(), devJob(t.TempDir()))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Status != tc.status || !strings.Contains(res.Summary, tc.summary) || len(res.Questions) != tc.questions {
				t.Errorf("got %+v", res)
			}
			if res.Usage.CostUSD != 1.25 {
				t.Errorf("cost must be reported even on failure: %+v", res.Usage)
			}
		})
	}
}

func TestRun_CrashWithoutResult(t *testing.T) {
	bin, _ := fakeCLI(t, "not json\n", 1)
	_, err := New(Config{Binary: bin}).Run(context.Background(), devJob(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "some warning") {
		t.Fatalf("expected error carrying stderr, got %v", err)
	}
}

// isolated wraps the local sandbox but claims isolation, to check the
// permission flags the runner picks for container sandboxes.
type isolated struct{ sandbox.Local }

func (isolated) Isolated() bool { return true }

func TestRun_ResumeWithFeedbackInIsolatedSandbox(t *testing.T) {
	bin, argsFile := fakeCLI(t, resultLine(`"subtype":"success","is_error":false,"structured_output":{"status":"done","summary":"fixed"}`), 0)
	job := devJob(t.TempDir())
	job.SessionID = "s1"
	job.Feedback = "verify failed: go test"
	if _, err := New(Config{Binary: bin, Sandbox: &isolated{}}).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if !strings.Contains(args, "-p\nverify failed: go test\n") {
		t.Errorf("resumed prompt should be just the feedback:\n%s", args)
	}
	for _, want := range []string{"--resume\ns1", "--dangerously-skip-permissions"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q", want)
		}
	}
	if strings.Contains(args, "--permission-mode") {
		t.Error("isolated sandbox should not restrict permissions")
	}
}

func TestRun_UnknownRole(t *testing.T) {
	_, err := New(Config{}).Run(context.Background(), runner.Job{Role: "janitor"})
	if err == nil {
		t.Error("expected error for role without persona")
	}
}

func TestRun_AllowsProjectVerifyCommandsLocally(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".intern"), 0755); err != nil {
		t.Fatal(err)
	}
	contract := "verify:\n  - name: test\n    run: cargo test --all\n  - name: compound\n    run: cd web && npm test\n"
	if err := os.WriteFile(filepath.Join(dir, ".intern/project.yaml"), []byte(contract), 0644); err != nil {
		t.Fatal(err)
	}
	bin, argsFile := fakeCLI(t, resultLine(`"subtype":"success","is_error":false,"structured_output":{"status":"done","summary":"ok"}`), 0)
	if _, err := New(Config{Binary: bin}).Run(context.Background(), devJob(dir)); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if !strings.Contains(args, "Bash(cargo test --all:*)") {
		t.Error("project verify command not allowed")
	}
	if strings.Contains(args, "cd web") {
		t.Error("compound command must not become an allow rule")
	}
}
