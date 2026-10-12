package orchestrator

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"intern/internal/config"
	"intern/internal/repository"
	"intern/internal/runner"
)

// flowRepo is a git-backed repository client with scripted PR state.
type flowRepo struct {
	gitRepoClient
	mu       sync.Mutex
	pr       repository.PRState
	reviews  []string
	comments []string
	pushes   []string
	merges   []string
}

func (r *flowRepo) git(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return &exec.ExitError{Stderr: out}
	}
	return nil
}

func (r *flowRepo) SwitchBranch(_ context.Context, b string) error { return r.git("checkout", "-q", b) }
func (r *flowRepo) Push(_ context.Context, b string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes = append(r.pushes, b)
	return nil
}
func (r *flowRepo) FetchBranch(context.Context, string) error { return nil }
func (r *flowRepo) GetPR(context.Context, string) (repository.PRState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pr, nil
}
func (r *flowRepo) CommentPR(_ context.Context, _, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.comments = append(r.comments, body)
	return nil
}
func (r *flowRepo) ReviewPR(_ context.Context, _, body string, cs []repository.ReviewComment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cs {
		body += "\n" + c.Path + ":" + c.Body
	}
	r.reviews = append(r.reviews, body)
	return nil
}
func (r *flowRepo) AddLabels(_ context.Context, _ string, labels ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pr.Labels = append(r.pr.Labels, labels...)
	return nil
}
func (r *flowRepo) RemoveLabel(_ context.Context, _, label string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keep []string
	for _, l := range r.pr.Labels {
		if l != label {
			keep = append(keep, l)
		}
	}
	r.pr.Labels = keep
	return nil
}
func (r *flowRepo) MergePR(_ context.Context, url, method string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.merges = append(r.merges, method)
	r.pr.Merged = true
	return nil
}

// roleRunner answers each role from its own queue of steps.
type roleRunner struct {
	mu    sync.Mutex
	steps map[runner.Role][]func(runner.Job) runner.Result
	jobs  []runner.Job
}

func (r *roleRunner) Run(_ context.Context, job runner.Job) (runner.Result, error) {
	r.mu.Lock()
	r.jobs = append(r.jobs, job)
	q := r.steps[job.Role]
	if len(q) == 0 {
		r.mu.Unlock()
		panic("no scripted step for role " + string(job.Role))
	}
	step := q[0]
	r.steps[job.Role] = q[1:]
	r.mu.Unlock()
	return step(job), nil
}

func (r *roleRunner) count(role runner.Role) int {
	n := 0
	for _, j := range r.jobs {
		if j.Role == role {
			n++
		}
	}
	return n
}

func review(approve bool, comments ...runner.ReviewComment) func(runner.Job) runner.Result {
	return func(runner.Job) runner.Result {
		return runner.Result{Status: runner.StatusDone, Summary: "reviewed", Review: &runner.Review{Approve: approve, Comments: comments}, Usage: runner.Usage{CostUSD: 0.1}}
	}
}

// newFlowFixture returns a coordinator whose clone has base branch
// "master" and a ticket branch "feature/P-1" with the developer's work.
func newFlowFixture(t *testing.T, cfg *config.Config, rr *roleRunner) (*Coordinator, *flowRepo, *fakeTicketing) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	if out, err := exec.Command("git", "init", "-q", "-b", "master", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	repo := &flowRepo{gitRepoClient: gitRepoClient{dir: dir}}
	for _, args := range [][]string{
		{"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
		{"checkout", "-q", "-b", "feature/P-1"},
	} {
		if err := repo.git(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	writeFile(t, dir, "app.txt", "v1\n")
	if err := repo.git("add", "."); err != nil {
		t.Fatal(err)
	}
	if err := repo.git("commit", "-q", "-m", "dev work"); err != nil {
		t.Fatal(err)
	}
	if err := repo.git("checkout", "-q", "master"); err != nil {
		t.Fatal(err)
	}

	paths, err := repository.NewRepositoryPath(root, "repo")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseBranch = "master"
	if cfg.MaxReviewRounds == 0 {
		cfg.MaxReviewRounds = 2
	}
	if cfg.AllowedWriteDirs == nil {
		cfg.AllowedWriteDirs = []string{"*"}
	}
	c, ft := newLifecycleCoordinator(t, cfg)
	c.Runner = rr
	c.Repository = repository.NewRepositoryService(repo)
	c.RepoPaths = paths
	return c, repo, ft
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestFlow_ReviewRequestsChangesThenApproves(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleReviewer: {
			review(false, runner.ReviewComment{Path: "app.txt", Line: 1, Body: "should be v2"}),
			review(true),
		},
		runner.RoleDeveloper: {func(j runner.Job) runner.Result {
			writeFile(t, j.Workdir, "app.txt", "v2\n")
			return runner.Result{Status: runner.StatusDone, Summary: "bumped to v2", Usage: runner.Usage{CostUSD: 0.2}}
		}},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true}, rr)
	ctx := context.Background()

	c.startFlow(ctx, "P-1", "Bump", "Make it v2", "feature/P-1", "https://github.com/o/r/pull/7", 1.0)
	if !repo.pr.HasLabel("intern:review") {
		t.Fatalf("labels = %v", repo.pr.Labels)
	}

	c.advanceFlows(ctx) // review round 1: changes requested
	f := c.State.ListFlows()[0]
	if f.Stage != StageChangesRequested || !strings.Contains(f.Feedback, "app.txt:1: should be v2") {
		t.Fatalf("after review 1: %+v", f)
	}
	if !strings.Contains(repo.reviews[0], "Changes requested") {
		t.Errorf("review body = %q", repo.reviews[0])
	}
	if got := currentBranch(t, repo.dir); got != "master" {
		t.Errorf("clone left on %s, want master", got)
	}

	c.advanceFlows(ctx) // developer fixes and pushes
	f = c.State.ListFlows()[0]
	if f.Stage != StageReview || len(repo.pushes) != 1 || repo.pushes[0] != "feature/P-1" {
		t.Fatalf("after fix: stage %s pushes %v", f.Stage, repo.pushes)
	}
	dev := rr.jobs[len(rr.jobs)-1]
	if dev.Role != runner.RoleDeveloper || !strings.Contains(dev.Feedback, "should be v2") {
		t.Errorf("developer job missing review feedback: %+v", dev)
	}
	if out, _ := exec.Command("git", "-C", repo.dir, "show", "feature/P-1:app.txt").Output(); string(out) != "v2\n" {
		t.Errorf("fix not committed on the PR branch: %q", out)
	}

	c.advanceFlows(ctx) // review round 2: approved, no test or merge stage
	if flows := c.State.ListFlows(); len(flows) != 0 {
		t.Errorf("approved flow with no later stages should end, got %+v", flows)
	}
	if repo.pr.HasLabel("intern:review") {
		t.Errorf("stage label not removed: %v", repo.pr.Labels)
	}
	if rr.count(runner.RoleReviewer) != 2 {
		t.Errorf("reviewer ran %d times", rr.count(runner.RoleReviewer))
	}
}

func TestFlow_EscalatesAfterMaxRoundsAndResumes(t *testing.T) {
	reject := review(false, runner.ReviewComment{Path: "app.txt", Line: 1, Body: "no"})
	fix := func(j runner.Job) runner.Result {
		writeFile(t, j.Workdir, "app.txt", "try\n")
		return runner.Result{Status: runner.StatusDone, Summary: "tried"}
	}
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleReviewer:  {reject, reject, review(true)},
		runner.RoleDeveloper: {fix},
	}}
	c, repo, ft := newFlowFixture(t, &config.Config{ReviewEnabled: true, MaxReviewRounds: 1}, rr)
	ctx := context.Background()
	c.startFlow(ctx, "P-1", "s", "d", "feature/P-1", "https://github.com/o/r/pull/7", 0)

	c.advanceFlows(ctx) // round 1 rejected
	c.advanceFlows(ctx) // fix
	c.advanceFlows(ctx) // round 2 rejected, over the limit
	f := c.State.ListFlows()[0]
	if f.Stage != StageNeedsHuman || !strings.Contains(f.Reason, "after 2 rounds") {
		t.Fatalf("expected escalation, got %+v", f)
	}
	if !strings.Contains(strings.Join(repo.comments, "\n"), LabelResume) {
		t.Error("PR comment should explain how to resume")
	}
	if len(ft.comments["P-1"]) == 0 {
		t.Error("ticket should get an escalation comment")
	}

	c.advanceFlows(ctx) // waiting on a human: nothing runs
	if n := rr.count(runner.RoleReviewer); n != 2 {
		t.Fatalf("reviewer ran while waiting for a human: %d", n)
	}

	repo.pr.Labels = append(repo.pr.Labels, LabelResume)
	c.advanceFlows(ctx) // resumed back to review
	f = c.State.ListFlows()[0]
	if f.Stage != StageReview || f.ReviewRounds != 0 || repo.pr.HasLabel(LabelResume) {
		t.Fatalf("resume did not reset the flow: %+v labels %v", f, repo.pr.Labels)
	}
	c.advanceFlows(ctx)
	if len(c.State.ListFlows()) != 0 {
		t.Error("approved flow should end")
	}
}

func TestFlow_MergedPRDropsFlow(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true}, &roleRunner{})
	ctx := context.Background()
	c.startFlow(ctx, "P-1", "s", "d", "feature/P-1", "https://github.com/o/r/pull/7", 0)
	repo.pr.Merged = true
	c.advanceFlows(ctx)
	if len(c.State.ListFlows()) != 0 {
		t.Error("flow for a merged PR should be dropped")
	}
}

func TestStartFlow_NoStagesEnabled(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{}, &roleRunner{})
	c.startFlow(context.Background(), "P-1", "s", "d", "feature/P-1", "u/7", 0)
	if len(c.State.ListFlows()) != 0 || len(repo.pr.Labels) != 0 {
		t.Error("no flow should start when review, test and auto-merge are off")
	}
}

func TestState_FlowsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	s.SaveFlow(Flow{TicketKey: "P-1", Stage: StageReview, PRURL: "u"})
	s2 := NewState(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if fl := s2.ListFlows(); len(fl) != 1 || fl[0].Stage != StageReview {
		t.Errorf("flows after reload = %+v", fl)
	}
}
