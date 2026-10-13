package orchestrator

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"intern/internal/config"
	"intern/internal/runner"
)

func TestBudget_PausesWorkAndNotifiesOnce(t *testing.T) {
	c, _ := newLifecycleCoordinator(t, &config.Config{ProjectBudgetUSD: 1.0})
	n := &recordingNotifier{}
	c.Notifier = n
	ctx := context.Background()

	c.recordSpend(ctx, runner.RoleDeveloper, 0.6)
	if c.budgetExceeded() || len(n.msgs) != 0 {
		t.Fatal("under budget should not pause")
	}
	c.recordSpend(ctx, runner.RoleReviewer, 0.5)
	c.recordSpend(ctx, runner.RoleTester, 0.1)
	if !c.budgetExceeded() {
		t.Fatal("budget should be exceeded at $1.20")
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "$1.10 spent of $1.00") {
		t.Errorf("notifications = %v", n.msgs)
	}
	snap := c.Team()
	if snap.RoleCost[runner.RoleDeveloper] != 0.6 || snap.RoleCost[runner.RoleReviewer] != 0.5 {
		t.Errorf("role cost = %v", snap.RoleCost)
	}
}

func TestBudget_FlowsPauseButMergesContinue(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{}}
	c, repo, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true, AutoMerge: true, ProjectBudgetUSD: 0.01}, rr)
	ctx := context.Background()
	c.State.AddSpend(0.02)

	c.startFlow(ctx, "P-1", "s", "d", "feature/P-1", "https://github.com/o/r/pull/7", 0)
	c.advanceFlows(ctx) // review would need the model: paused
	if len(rr.jobs) != 0 {
		t.Fatal("reviewer ran over budget")
	}

	f := c.State.ListFlows()[0]
	c.moveFlow(ctx, &f, StageMerge)
	c.advanceFlows(ctx) // merging costs nothing: continues
	if len(repo.merges) != 1 {
		t.Error("merge should still happen over budget")
	}
}

func TestBudget_ArchitectPaused(t *testing.T) {
	c, _, _ := newFlowFixture(t, &config.Config{ProjectBudgetUSD: 1}, &roleRunner{})
	c.State.AddSpend(5)
	reply, err := c.Converse(context.Background(), "C1", "add a feature")
	if err != nil || !strings.Contains(reply, "budget is used up") {
		t.Errorf("reply = %q, %v", reply, err)
	}
}

func TestMetricsServer_TeamMetrics(t *testing.T) {
	c, _ := newLifecycleCoordinator(t, &config.Config{})
	c.recordSpend(context.Background(), runner.RoleArchitect, 0.25)
	c.team.count(&c.team.merges)
	ms := NewMetricsServer(c.Metrics, 0)
	ms.Team, ms.Spend = c.Team, c.State.Spend
	rec := httptest.NewRecorder()
	ms.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{`ai_intern_role_cost_dollars{role="architect"} 0.250000`, "ai_intern_merges_total 1", "ai_intern_project_spend_dollars 0.250000"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}
