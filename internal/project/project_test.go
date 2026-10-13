package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(dir, ConfigPath)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_MissingIsNotAnError(t *testing.T) {
	_, found, err := Load(t.TempDir())
	if err != nil || found {
		t.Fatalf("Load on empty dir = found %v, err %v", found, err)
	}
}

func TestLoad_ParsesContract(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
setup:
  - name: deps
    run: npm ci
verify:
  - name: build
    run: npm run build
  - name: test
    run: npm test
    timeout: 20m
protected_paths: [".github/", ".intern/"]
`)
	cfg, found, err := Load(dir)
	if err != nil || !found {
		t.Fatalf("Load: found %v err %v", found, err)
	}
	if len(cfg.Setup) != 1 || len(cfg.Verify) != 2 || cfg.Verify[1].Timeout != "20m" {
		t.Errorf("unexpected config %+v", cfg)
	}
	if len(cfg.ProtectedPaths) != 2 {
		t.Errorf("protected paths = %v", cfg.ProtectedPaths)
	}
}

func TestLoad_RejectsBadCommands(t *testing.T) {
	for name, content := range map[string]string{
		"empty run":   "verify:\n  - name: x\n",
		"bad timeout": "verify:\n  - name: x\n    run: true\n    timeout: soon\n",
		"bad yaml":    "verify: [",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, content)
			if _, _, err := Load(dir); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestRunCommands_StopsAtFirstFailure(t *testing.T) {
	dir := t.TempDir()
	results, ok := RunCommands(context.Background(), dir, []Command{
		{Name: "pass", Run: "echo fine"},
		{Name: "fail", Run: "echo broken >&2; exit 3"},
		{Name: "never", Run: "touch ran"},
	})
	if ok {
		t.Fatal("expected failure")
	}
	if len(results) != 2 || !results[0].Passed || results[1].Passed {
		t.Fatalf("unexpected results %+v", results)
	}
	if !strings.Contains(results[1].Output, "broken") {
		t.Errorf("failure output not captured: %q", results[1].Output)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); !os.IsNotExist(err) {
		t.Error("commands after a failure must not run")
	}
	if rep := FailureReport(results); !strings.Contains(rep, "exit 3") || strings.Contains(rep, "echo fine") {
		t.Errorf("FailureReport = %q", rep)
	}
	sum := strings.Join(Summary(results), "\n")
	if !strings.Contains(sum, "pass: PASSED") || !strings.Contains(sum, "fail: FAILED") {
		t.Errorf("Summary = %q", sum)
	}
}

func TestRunCommands_Timeout(t *testing.T) {
	start := time.Now()
	// A compound command keeps sh as the parent, so this checks that the
	// child sleep is killed too rather than holding the output pipe open.
	results, ok := RunCommands(context.Background(), t.TempDir(), []Command{{Name: "slow", Run: "sleep 5; echo done", Timeout: "100ms"}})
	if ok || !strings.Contains(results[0].Output, "timed out") {
		t.Fatalf("expected timeout, got %+v", results)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout took %s; child process was not killed", elapsed)
	}
}
