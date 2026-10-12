package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/runner"
	"intern/internal/ticketing"
)

// parallelRunner writes a file named after the ticket and tracks how many
// jobs run at once.
type parallelRunner struct {
	active, peak int32
	mu           sync.Mutex
	dirs         map[string]string
}

func (p *parallelRunner) Run(_ context.Context, job runner.Job) (runner.Result, error) {
	n := atomic.AddInt32(&p.active, 1)
	for {
		peak := atomic.LoadInt32(&p.peak)
		if n <= peak || atomic.CompareAndSwapInt32(&p.peak, peak, n) {
			break
		}
	}
	defer atomic.AddInt32(&p.active, -1)
	p.mu.Lock()
	p.dirs[job.TicketKey] = job.Workdir
	p.mu.Unlock()
	if err := os.WriteFile(job.Workdir+"/"+job.TicketKey+".txt", []byte(job.TicketKey+"\n"), 0644); err != nil {
		return runner.Result{}, err
	}
	time.Sleep(300 * time.Millisecond) // overlap with the other ticket
	return runner.Result{Status: runner.StatusDone, Summary: "wrote " + job.TicketKey}, nil
}

func TestProcessTicket_ParallelWorktrees(t *testing.T) {
	pr := &parallelRunner{dirs: map[string]string{}}
	c, repo, ft := newFlowFixture(t, &config.Config{MaxConcurrentTickets: 2, BranchPrefix: "feature/", MaxTicketAttempts: 3}, &roleRunner{})
	c.Runner = pr
	c.ticketMetrics = map[string]*TicketMetrics{}
	ctx := context.Background()

	tickets := []ticketing.Ticket{{Key: "P-10", Summary: "ten"}, {Key: "P-11", Summary: "eleven"}}
	c.dispatchTickets(ctx, tickets, c.processTicket)

	if atomic.LoadInt32(&pr.peak) != 2 {
		t.Errorf("tickets did not run in parallel (peak %d)", pr.peak)
	}
	if pr.dirs["P-10"] == pr.dirs["P-11"] || strings.HasPrefix(pr.dirs["P-10"], repo.dir+"/") {
		t.Errorf("each ticket needs its own worktree outside the clone: %v", pr.dirs)
	}
	for _, k := range []string{"P-10", "P-11"} {
		if !c.State.IsProcessed(k) {
			t.Errorf("%s not processed; comments %v", k, ft.comments[k])
		}
		other := map[string]string{"P-10": "P-11", "P-11": "P-10"}[k]
		branch := "feature/" + k
		if out, err := exec.Command("git", "-C", repo.dir, "show", branch+":"+k+".txt").Output(); err != nil || string(out) != k+"\n" {
			t.Errorf("%s missing its own file: %q %v", branch, out, err)
		}
		if err := exec.Command("git", "-C", repo.dir, "cat-file", "-e", branch+":"+other+".txt").Run(); err == nil {
			t.Errorf("%s contains %s's work", branch, other)
		}
		if _, err := os.Stat(pr.dirs[k]); !os.IsNotExist(err) {
			t.Errorf("worktree %s not cleaned up", pr.dirs[k])
		}
	}
	if len(repo.pushes) != 2 || len(repo.prHeads) != 2 {
		t.Errorf("pushes %v PRs %v", repo.pushes, repo.prHeads)
	}
	if got := currentBranch(t, repo.dir); got != "master" {
		t.Errorf("main clone moved to %s", got)
	}
	log, _ := exec.Command("git", "-C", repo.dir, "log", "-1", "--format=%an", "feature/P-10").Output()
	if strings.TrimSpace(string(log)) != commitAuthorName {
		t.Errorf("commit author = %q", log)
	}
}

func TestAddWorktree_ReplacesStaleWorktree(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{}, &roleRunner{})
	ctx := context.Background()
	dir, cleanup, err := c.addWorktree(ctx, "feature/P-1")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "stale.txt", "x")
	// Simulate a crash: no cleanup. A second add must still succeed and
	// start from master, not the old branch tip.
	dir2, cleanup2, err := c.addWorktree(ctx, "feature/P-1")
	if err != nil {
		t.Fatalf("re-adding a stale worktree: %v", err)
	}
	defer cleanup2()
	_ = cleanup
	if _, err := os.Stat(dir2 + "/stale.txt"); !os.IsNotExist(err) {
		t.Error("stale files survived")
	}
	if _, err := os.Stat(dir2 + "/app.txt"); !os.IsNotExist(err) {
		t.Error("worktree should be reset to master, not the old branch tip")
	}
	_ = repo
}
