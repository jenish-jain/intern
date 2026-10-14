package orchestrator

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"intern/internal/config"
	"intern/internal/repository"
	"intern/internal/runner"
)

// advanceMaster adds a commit to master in the fixture's clone.
func advanceMaster(t *testing.T, repo *flowRepo, file, content string) {
	t.Helper()
	for _, args := range [][]string{{"checkout", "-q", "master"}} {
		if err := repo.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, repo.dir, file, content)
	if err := repo.git("add", "."); err != nil {
		t.Fatal(err)
	}
	if err := repo.git("commit", "-q", "-m", "master moves on"); err != nil {
		t.Fatal(err)
	}
}

func branchHas(t *testing.T, repo *flowRepo, ref string) bool {
	t.Helper()
	return exec.Command("git", "-C", repo.dir, "merge-base", "--is-ancestor", ref, "feature/P-1").Run() == nil
}

func conflictFlow() Flow {
	return Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: "https://github.com/o/r/pull/1",
		Description: "Small change\n\nEpic: E1 | Size: S", Stage: StageChangesRequested, Conflict: true}
}

// A base branch that merges cleanly needs no model at all.
func TestResolveConflict_CleanMergeNeedsNoModel(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true}, &roleRunner{}) // any model call panics
	advanceMaster(t, repo, "other.txt", "unrelated\n")
	c.State.SaveFlow(conflictFlow())

	c.advanceFlows(context.Background())

	if !branchHas(t, repo, "master") {
		t.Error("master should be merged into the PR branch")
	}
	if f, _ := flowOf(c, "P-1"); f.Stage != StageReview || f.Conflict {
		t.Errorf("flow = %+v, want back in review with the conflict cleared", f)
	}
	if len(repo.pushes) != 1 || !strings.Contains(strings.Join(repo.comments, "\n"), "merged without conflicts") {
		t.Errorf("pushes %v, comments %v", repo.pushes, repo.comments)
	}
}

// Real conflicts go to the developer, a tier up, with the file list, and
// the result must be a concluded merge.
func TestResolveConflict_DeveloperResolvesOnAStrongerModel(t *testing.T) {
	var fx *testing.T = t
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleDeveloper: {func(j runner.Job) runner.Result {
			if !strings.Contains(j.Feedback, "app.txt") {
				fx.Errorf("feedback should list the conflicted file: %q", j.Feedback)
			}
			writeFile(fx, j.Workdir, "app.txt", "v1 and master\n")
			for _, args := range [][]string{{"add", "app.txt"}, {"commit", "-q", "--no-edit"}} {
				if out, err := exec.Command("git", append([]string{"-C", j.Workdir}, args...)...).CombinedOutput(); err != nil {
					fx.Fatalf("git %v: %s", args, out)
				}
			}
			return runner.Result{Status: runner.StatusDone, Summary: "kept both"}
		}},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true, ModelRouting: config.DefaultModelRouting()}, rr)
	advanceMaster(t, repo, "app.txt", "master\n") // the branch also adds app.txt: a conflict
	f := conflictFlow()
	f.Conflict = false // as after a restart: the conflict is detected, not remembered
	c.State.SaveFlow(f)

	c.advanceFlows(context.Background())

	if len(rr.jobs) != 1 || rr.jobs[0].Model != "sonnet" {
		t.Fatalf("a size S ticket's conflict should go to sonnet, jobs %+v", rr.jobs)
	}
	if !branchHas(t, repo, "master") {
		t.Error("master should be part of the branch after the resolution")
	}
	if got, _ := flowOf(c, "P-1"); got.Stage != StageReview {
		t.Errorf("stage = %s, want review", got.Stage)
	}
}

// A developer that reports done without finishing the merge is caught.
func TestResolveConflict_UnfinishedMergeEscalates(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleDeveloper: {func(runner.Job) runner.Result {
			return runner.Result{Status: runner.StatusDone, Summary: "looked at it, all fine"}
		}},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true}, rr)
	advanceMaster(t, repo, "app.txt", "master\n")
	c.State.SaveFlow(conflictFlow())

	c.advanceFlows(context.Background())

	got, _ := flowOf(c, "P-1")
	if got.Stage != StageNeedsHuman || !strings.Contains(got.Reason, "conflicts") || !strings.Contains(got.Reason, "looked at it, all fine") {
		t.Errorf("want an escalation that names the conflict and quotes the developer, got %s %q", got.Stage, got.Reason)
	}
	if len(repo.pushes) != 0 {
		t.Errorf("nothing should be pushed, got %v", repo.pushes)
	}
}

// An auto-merge that hits a conflict marks the flow for resolution.
func TestRunMerge_ConflictFlagsTheFlow(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{AutoMerge: true}, &roleRunner{})
	repo.mergeErr = repository.ErrMergeConflict
	c.State.SaveFlow(Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: "https://github.com/o/r/pull/1", Stage: StageMerge})

	c.advanceFlows(context.Background())

	if got, _ := flowOf(c, "P-1"); got.Stage != StageChangesRequested || !got.Conflict || got.Feedback != "" {
		t.Errorf("flow = %+v, want changes-requested with Conflict set", got)
	}
}

// A fix round with nothing left to change and a passing verify goes back
// to review instead of escalating.
func TestRunDeveloperFix_NothingToChangeAndVerifyPasses(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleDeveloper: {func(runner.Job) runner.Result {
			return runner.Result{Status: runner.StatusDone, Summary: "already fixed on the branch"}
		}},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true}, rr)
	c.State.SaveFlow(Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: "https://github.com/o/r/pull/1",
		Stage: StageChangesRequested, Feedback: "CI failed: names not saved"})

	c.advanceFlows(context.Background())

	if got, _ := flowOf(c, "P-1"); got.Stage != StageReview {
		t.Errorf("stage = %s, want review", got.Stage)
	}
	if !strings.Contains(strings.Join(repo.comments, "\n"), "no changes needed") {
		t.Errorf("comments %v", repo.comments)
	}
}
