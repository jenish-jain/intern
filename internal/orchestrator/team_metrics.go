package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// teamMetrics counts what the multi-role flow does: spend per role and
// the outcomes that show whether autonomy is working.
type teamMetrics struct {
	mu          sync.Mutex
	roleCost    map[runner.Role]float64
	approvals   int64
	rejections  int64
	testPasses  int64
	testFails   int64
	merges      int64
	escalations int64
}

// TeamSnapshot is a point-in-time copy of team metrics.
type TeamSnapshot struct {
	RoleCost    map[runner.Role]float64
	Approvals   int64
	Rejections  int64
	TestPasses  int64
	TestFails   int64
	Merges      int64
	Escalations int64
}

func (t *teamMetrics) addCost(role runner.Role, usd float64) {
	if usd <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.roleCost == nil {
		t.roleCost = map[runner.Role]float64{}
	}
	t.roleCost[role] += usd
}

func (t *teamMetrics) count(field *int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*field++
}

func (t *teamMetrics) snapshot() TeamSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	cost := make(map[runner.Role]float64, len(t.roleCost))
	for k, v := range t.roleCost {
		cost[k] = v
	}
	return TeamSnapshot{RoleCost: cost, Approvals: t.approvals, Rejections: t.rejections,
		TestPasses: t.testPasses, TestFails: t.testFails, Merges: t.merges, Escalations: t.escalations}
}

// Team returns the team metrics snapshot.
func (c *Coordinator) Team() TeamSnapshot { return c.team.snapshot() }

// recordSpend adds a job's cost to the per-role metrics and the persisted
// project total, and announces once when the project budget is reached.
func (c *Coordinator) recordSpend(ctx context.Context, role runner.Role, usd float64) {
	if usd <= 0 {
		return
	}
	c.team.addCost(role, usd)
	total := c.State.AddSpend(usd)
	if c.Cfg.ProjectBudgetUSD > 0 && total >= c.Cfg.ProjectBudgetUSD {
		c.budgetOnce.Do(func() {
			msg := fmt.Sprintf(":octagonal_sign: Project budget reached: $%.2f spent of $%.2f. New tickets, reviews, tests and planning are paused; merges of ready PRs continue. Raise PROJECT_BUDGET_USD to resume.", total, c.Cfg.ProjectBudgetUSD)
			logger.Warn("Project budget reached; pausing model work", "spent", total, "budget", c.Cfg.ProjectBudgetUSD)
			c.notify(ctx, msg)
		})
	}
}

// budgetExceeded reports whether the project budget is used up.
func (c *Coordinator) budgetExceeded() bool {
	return c.Cfg.ProjectBudgetUSD > 0 && c.State.Spend() >= c.Cfg.ProjectBudgetUSD
}

// formatRoleCosts renders per-role spend as Prometheus lines.
func formatRoleCosts(cost map[runner.Role]float64) string {
	roles := make([]string, 0, len(cost))
	for r := range cost {
		roles = append(roles, string(r))
	}
	sort.Strings(roles)
	out := "# HELP ai_intern_role_cost_dollars Model spend by persona\n# TYPE ai_intern_role_cost_dollars counter\n"
	for _, r := range roles {
		out += fmt.Sprintf("ai_intern_role_cost_dollars{role=%q} %.6f\n", r, cost[runner.Role(r)])
	}
	return out + "\n"
}
