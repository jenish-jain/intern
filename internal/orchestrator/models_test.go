package orchestrator

import (
	"context"
	"strings"
	"testing"

	"intern/internal/config"
	"intern/internal/runner"
)

func TestTicketTier(t *testing.T) {
	const s, m, l = "Epic: E1 | Size: S", "Epic: E1 | Size: M", "Epic: E1 | Size: L"
	cases := []struct {
		desc, priority string
		escalations    int
		want           int
	}{
		{s, "Medium", 0, 0},
		{m, "Medium", 0, 1},
		{l, "Low", 0, 2},
		{"no size given", "", 0, 1}, // unsized counts as M
		{s, "High", 0, 1},           // critical work steps up
		{s, "Highest", 1, 2},        // and struggling steps up again
		{l, "Highest", 2, 2},        // capped at L
		{s, "Medium", 1, 1},
	}
	for _, tc := range cases {
		if got := ticketTier(tc.desc, tc.priority, tc.escalations); got != tc.want {
			t.Errorf("ticketTier(%q, %q, %d) = %d, want %d", tc.desc, tc.priority, tc.escalations, got, tc.want)
		}
	}
}

func TestModelFor(t *testing.T) {
	c, _ := newLifecycleCoordinator(t, &config.Config{ModelRouting: config.DefaultModelRouting()})
	c.rememberTicket("IP-2", "Medium", 0)
	c.rememberTicket("IP-9", "Highest", 0)
	small := "Page flip\n\nEpic: E1 | Size: S"
	cases := []struct {
		role        runner.Role
		key         string
		escalations int
		want        string
	}{
		{runner.RoleArchitect, "", 0, "opus"},
		{runner.RoleDeveloper, "IP-2", 0, "haiku"},  // simple work starts on the cheapest model
		{runner.RoleDeveloper, "IP-2", 1, "sonnet"}, // and steps up when it struggles
		{runner.RoleTester, "IP-2", 0, "haiku"},
		{runner.RoleTester, "IP-9", 0, "sonnet"},  // a critical small ticket is tested like a medium one
		{runner.RoleDeveloper, "IP-2", 2, "opus"}, // repeated failures reach the strongest model
		{runner.RoleReviewer, "IP-9", 1, "opus"},
	}
	for _, tc := range cases {
		if got := c.modelFor(tc.role, tc.key, small, tc.escalations); got != tc.want {
			t.Errorf("modelFor(%s, %s, esc %d) = %q, want %q", tc.role, tc.key, tc.escalations, got, tc.want)
		}
	}

	c.Cfg.ClaudeModel = "opus"
	if got := c.modelFor(runner.RoleTester, "IP-2", small, 0); got != "" {
		t.Errorf("CLAUDE_MODEL must override routing (empty model = runner default), got %q", got)
	}
}

// Reviewer and tester jobs carry the routed model.
func TestFlow_JobsUseRoutedModels(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleReviewer: {review(true)},
		runner.RoleTester:   {tested(true)},
	}}
	c, _, _ := newFlowFixture(t, &config.Config{ReviewEnabled: true, TestEnabled: true, ModelRouting: config.DefaultModelRouting()}, rr)
	c.State.SaveFlow(Flow{TicketKey: "P-1", Branch: "feature/P-1", PRURL: "https://github.com/o/r/pull/1",
		Description: "Small change\n\nEpic: E1 | Size: S", Stage: StageReview})
	c.advanceFlows(context.Background())
	c.advanceFlows(context.Background())
	got := map[runner.Role]string{}
	for _, j := range rr.jobs {
		got[j.Role] = j.Model
	}
	if got[runner.RoleReviewer] != "haiku" || got[runner.RoleTester] != "haiku" {
		t.Errorf("models = %v, want haiku for reviewing and testing a one-line change", got)
	}
}

// Reviewers and testers are routed by the change actually made: a ticket
// planned as M whose implementation is small is checked by the cheap model,
// and a large change gets the strong one.
func TestDiffTier(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{ModelRouting: config.DefaultModelRouting()}, &roleRunner{})
	ctx := context.Background()
	if err := repo.git("checkout", "-q", "feature/P-1"); err != nil {
		t.Fatal(err)
	}
	medium := "Game engine\n\nEpic: E1 | Size: M"

	if tier, ok := diffTier(ctx, repo.dir, "master"); !ok || tier != 0 {
		t.Fatalf("one-line change: tier %d ok %v, want 0", tier, ok)
	}
	if got := c.modelForChange(ctx, runner.RoleReviewer, "P-1", medium, repo.dir, 0); got != "haiku" {
		t.Errorf("small implementation of an M ticket reviewed by %q, want haiku", got)
	}

	// A big lockfile doesn't make a change complex.
	writeFile(t, repo.dir, "package-lock.json", strings.Repeat("{}\n", 2000))
	if err := repo.git("add", "."); err != nil {
		t.Fatal(err)
	}
	if err := repo.git("commit", "-q", "-m", "lock"); err != nil {
		t.Fatal(err)
	}
	if tier, _ := diffTier(ctx, repo.dir, "master"); tier != 0 {
		t.Errorf("lockfile counted as complexity: tier %d", tier)
	}

	writeFile(t, repo.dir, "engine.js", strings.Repeat("x = 1\n", 500))
	if err := repo.git("add", "."); err != nil {
		t.Fatal(err)
	}
	if err := repo.git("commit", "-q", "-m", "engine"); err != nil {
		t.Fatal(err)
	}
	if got := c.modelForChange(ctx, runner.RoleReviewer, "P-1", medium, repo.dir, 0); got != "opus" {
		t.Errorf("500-line change reviewed by %q, want opus", got)
	}
}
