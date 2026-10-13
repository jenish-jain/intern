package orchestrator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/config"
	"intern/internal/gate"
	"intern/internal/repository"
	"intern/internal/runner"
	"intern/internal/ticketing"
)

// gitRepoClient commits with the git CLI; other methods are unused here.
type gitRepoClient struct {
	repository.RepositoryClient
	dir string
}

func (g *gitRepoClient) Commit(_ context.Context, message string) error {
	cmd := exec.Command("git", "commit", "-q", "-m", message)
	cmd.Dir = g.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(string(out))
	}
	return nil
}

// scriptedRunner runs one step function per call.
type scriptedRunner struct {
	steps []func(job runner.Job) (runner.Result, error)
	jobs  []runner.Job
}

func (s *scriptedRunner) Run(_ context.Context, job runner.Job) (runner.Result, error) {
	s.jobs = append(s.jobs, job)
	i := len(s.jobs) - 1
	if i >= len(s.steps) {
		return runner.Result{}, errors.New("unexpected extra run")
	}
	return s.steps[i](job)
}

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	writeFile(t, dir, "README.md", "hi\n")
	cmd := exec.Command("sh", "-c", "git add . && git commit -q -m init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %s", out)
	}
	return dir
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func pipelineCoordinator(t *testing.T, dir string, r runner.Runner, cfg *config.Config) *Coordinator {
	t.Helper()
	if cfg.AllowedWriteDirs == nil {
		cfg.AllowedWriteDirs = []string{"*"}
	}
	return &Coordinator{
		Cfg:        cfg,
		Runner:     r,
		Repository: repository.NewRepositoryService(&gitRepoClient{dir: dir}),
		Metrics:    NewMetrics(),
	}
}

func done(summary string, cost float64) runner.Result {
	return runner.Result{Status: runner.StatusDone, Summary: summary, SessionID: "s1", Usage: runner.Usage{CostUSD: cost}}
}

const verifyContract = `verify:
  - name: has marker
    run: grep -qx ok src/app.txt
`

func TestDevelopAndVerify_PassesFirstTime(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".intern/project.yaml", verifyContract)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(j runner.Job) (runner.Result, error) {
			writeFile(t, j.Workdir, "src/app.txt", "ok\n")
			return done("wrote app", 0.2), nil
		},
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{})

	out, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Summary: "s", Workdir: dir}, base)
	if err != nil {
		t.Fatalf("developAndVerify: %v", err)
	}
	if len(out.Checks) != 1 || !out.Checks[0].Passed {
		t.Errorf("checks = %+v", out.Checks)
	}
	if out.Result.Usage.CostUSD != 0.2 {
		t.Errorf("usage = %+v", out.Result.Usage)
	}
	log, _ := runCommandCapture(context.Background(), dir, "git", "log", "--format=%s")
	if !strings.Contains(log, "feat(P-1): s") {
		t.Errorf("expected feature commit, log:\n%s", log)
	}
}

func TestDevelopAndVerify_FixRoundPassesFeedbackAndSession(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".intern/project.yaml", verifyContract)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(j runner.Job) (runner.Result, error) {
			writeFile(t, j.Workdir, "src/app.txt", "broken\n")
			return done("first try", 0.1), nil
		},
		func(j runner.Job) (runner.Result, error) {
			writeFile(t, j.Workdir, "src/app.txt", "ok\n")
			return done("fixed", 0.05), nil
		},
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{VerifyFixAttempts: 1})

	out, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Summary: "s", Workdir: dir}, base)
	if err != nil {
		t.Fatalf("developAndVerify: %v", err)
	}
	fix := sr.jobs[1]
	if !strings.Contains(fix.Feedback, "grep -qx ok src/app.txt") || fix.SessionID != "s1" {
		t.Errorf("fix job missing feedback or session: %+v", fix)
	}
	if got := out.Result.Usage.CostUSD; got < 0.149 || got > 0.151 {
		t.Errorf("total cost = %v, want 0.15", got)
	}
	if out.Result.Summary != "first try" {
		t.Errorf("PR summary should describe the work, got %q", out.Result.Summary)
	}
	if s := c.Metrics.Snapshot(); s.HealSuccesses != 1 {
		t.Errorf("heal successes = %d, want 1", s.HealSuccesses)
	}
}

func TestDevelopAndVerify_GivesUpAfterFixAttempts(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".intern/project.yaml", verifyContract)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(j runner.Job) (runner.Result, error) {
			writeFile(t, j.Workdir, "src/app.txt", "broken\n")
			return done("try", 0), nil
		},
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{VerifyFixAttempts: 0})
	_, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Workdir: dir}, base)
	if err == nil || !strings.Contains(err.Error(), "verify failed") {
		t.Fatalf("expected verify failure, got %v", err)
	}
}

func TestDevelopAndVerify_GateRejection(t *testing.T) {
	dir := initRepo(t)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(j runner.Job) (runner.Result, error) {
			writeFile(t, j.Workdir, "secrets/key.pem", "-----BEGIN RSA PRIVATE KEY-----\n")
			return done("oops", 0), nil
		},
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{})
	_, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Workdir: dir}, base)
	if err == nil || !strings.Contains(err.Error(), "rejected by gate") {
		t.Fatalf("expected gate rejection, got %v", err)
	}
}

func TestDevelopAndVerify_NoChanges(t *testing.T) {
	dir := initRepo(t)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(runner.Job) (runner.Result, error) { return done("nothing", 0), nil },
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{})
	if _, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Workdir: dir}, base); !errors.Is(err, errNoChanges) {
		t.Fatalf("expected errNoChanges, got %v", err)
	}
}

func TestDevelopAndVerify_NeedsHuman(t *testing.T) {
	dir := initRepo(t)
	base, _ := gate.HeadSHA(context.Background(), dir)
	sr := &scriptedRunner{steps: []func(runner.Job) (runner.Result, error){
		func(runner.Job) (runner.Result, error) {
			return runner.Result{Status: runner.StatusNeedsHuman, Questions: []string{"Which database?"}}, nil
		},
	}}
	c := pipelineCoordinator(t, dir, sr, &config.Config{})
	_, err := c.developAndVerify(context.Background(), runner.Job{TicketKey: "P-1", Workdir: dir}, base)
	var nh *NeedsHumanError
	if !errors.As(err, &nh) || nh.Questions[0] != "Which database?" {
		t.Fatalf("expected NeedsHumanError, got %v", err)
	}
}

func TestHandleTicketFailure_NeedsHumanComment(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{})
	tk := ticketing.Ticket{Key: "P-1", Summary: "s"}
	c.handleTicketFailure(context.Background(), tk, ticketFingerprint(tk), &NeedsHumanError{Questions: []string{"Which database?"}})
	got := ft.comments["P-1"][0]
	if !strings.Contains(got, "- Which database?") || !strings.Contains(got, "Add the answers") {
		t.Errorf("comment = %q", got)
	}
}

func TestBuildPRBody(t *testing.T) {
	body := buildPRBody(prBodyInput{
		TicketKey:   "P-1",
		Summary:     "Add login",
		WorkSummary: "Added a login handler.",
		Changes:     []gate.Change{{Status: "A", Path: "internal/login.go"}, {Status: "M", Path: "cmd/main.go"}},
		Notes:       []string{"used bcrypt cost 12"},
		Checks:      []string{"test: PASSED (3s)"},
	})
	for _, want := range []string{"## What was done", "Added a login handler.", "internal/login.go (added)", "cmd/main.go (modified)", "## Judgment calls to confirm", "- test: PASSED (3s)"} {
		if !strings.Contains(body, want) {
			t.Errorf("PR body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(buildPRBody(prBodyInput{TicketKey: "P-2"}), "No verify commands configured") {
		t.Error("expected hint when no verify commands")
	}
}
