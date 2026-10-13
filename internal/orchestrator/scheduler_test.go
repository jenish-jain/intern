package orchestrator

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/runner"
	"intern/internal/ticketing"
)

// overlapRunner proves a reviewer and a developer run at the same time:
// each signals that it started, then waits for the other.
type overlapRunner struct {
	devStarted, reviewStarted chan struct{}
	once                      sync.Once
	mu                        sync.Mutex
	overlapped                map[runner.Role]bool
}

func (o *overlapRunner) Run(_ context.Context, job runner.Job) (runner.Result, error) {
	wait := func(ch chan struct{}) bool {
		select {
		case <-ch:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}
	switch job.Role {
	case runner.RoleDeveloper:
		close(o.devStarted)
		ok := wait(o.reviewStarted)
		o.mu.Lock()
		o.overlapped[job.Role] = ok
		o.mu.Unlock()
		if err := os.WriteFile(job.Workdir+"/two.txt", []byte("two\n"), 0644); err != nil {
			return runner.Result{}, err
		}
		return runner.Result{Status: runner.StatusDone, Summary: "built"}, nil
	case runner.RoleReviewer:
		close(o.reviewStarted)
		ok := wait(o.devStarted)
		o.mu.Lock()
		o.overlapped[job.Role] = ok
		o.mu.Unlock()
		return runner.Result{Status: runner.StatusDone, Summary: "ok", Review: &runner.Review{Approve: true}}, nil
	}
	return runner.Result{}, nil
}

// A PR's review must not wait for developers building other tickets, even
// with one slot of each kind.
func TestCycle_ReviewRunsWhileDeveloperBuilds(t *testing.T) {
	or := &overlapRunner{devStarted: make(chan struct{}), reviewStarted: make(chan struct{}), overlapped: map[runner.Role]bool{}}
	c, _, ft := newFlowFixture(t, &config.Config{ReviewEnabled: true, MaxConcurrentTickets: 1, BranchPrefix: "feature/", MaxTicketAttempts: 3}, &roleRunner{})
	c.Runner = or
	c.ticketMetrics = map[string]*TicketMetrics{}
	c.State.SaveFlow(Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: "https://github.com/o/r/pull/9", Stage: StageReview, CreatedAt: time.Now()})
	ctx := context.Background()

	flows := c.startFlows(ctx)
	devs, _ := c.startTickets(ctx, []ticketing.Ticket{{Key: "P-2", Summary: "two"}}, c.processTicket)
	flows.Wait()
	devs.Wait()

	if !or.overlapped[runner.RoleDeveloper] || !or.overlapped[runner.RoleReviewer] {
		t.Fatalf("review and development ran one after the other: %v", or.overlapped)
	}
	if !c.State.IsProcessed("P-2") {
		t.Errorf("P-2 not built; comments %v", ft.comments["P-2"])
	}
	if f, ok := flowOf(c, "P-1"); !ok || f.Stage != StageMerge {
		t.Errorf("P-1 should be approved and ready, got %+v", f)
	}
}

func flowOf(c *Coordinator, key string) (Flow, bool) {
	for _, f := range c.State.ListFlows() {
		if f.TicketKey == key {
			return f, true
		}
	}
	return Flow{}, false
}

func tested(passed bool) func(runner.Job) runner.Result {
	return func(runner.Job) runner.Result {
		return runner.Result{Status: runner.StatusDone, Summary: "tested", Test: &runner.TestReport{Passed: passed}}
	}
}

// A later ticket's PR that passes first waits, queued, until the earlier
// one merges: PRs merge in plan order.
func TestFlow_MergesInPlanOrder(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleReviewer: {review(true)},
		runner.RoleTester:   {tested(true), tested(true)},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true, TestEnabled: true, AutoMerge: true, MaxConcurrentTickets: 2, BranchPrefix: "feature/"}, rr)
	if err := repo.git("branch", "feature/P-2", "feature/P-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p1 := "https://github.com/o/r/pull/1"
	p2 := "https://github.com/o/r/pull/2"
	c.State.SaveFlow(Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: p1, Stage: StageReview, CreatedAt: now})
	c.State.SaveFlow(Flow{TicketKey: "P-2", Branch: "feature/P-2", PRURL: p2, Stage: StageTest, CreatedAt: now.Add(time.Second)})
	ctx := context.Background()

	c.advanceFlows(ctx) // P-1 approved (now testing); P-2's tests pass first
	if f, _ := flowOf(c, "P-2"); f.Stage != StageQueued {
		t.Fatalf("P-2 passed before P-1 merged, so it should be queued, got %s", f.Stage)
	}
	if !strings.Contains(strings.Join(repo.comments, "\n"), "waits for P-1") {
		t.Errorf("the queued PR should say what it waits for, comments %v", repo.comments)
	}

	for i := 0; i < 4; i++ {
		c.advanceFlows(ctx)
	}
	if strings.Join(repo.mergedURLs, ",") != p1+","+p2 {
		t.Errorf("merge order = %v, want P-1 then P-2", repo.mergedURLs)
	}
}

func TestTicketBefore(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"IP-2", "IP-10", true}, // numeric, not lexical
		{"IP-10", "IP-2", false},
		{"IP-7", "IP-7", false},
		{"AB-9", "CD-1", true}, // different projects fall back to the key
	}
	for _, tc := range cases {
		if got := ticketBefore(tc.a, tc.b); got != tc.want {
			t.Errorf("ticketBefore(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
