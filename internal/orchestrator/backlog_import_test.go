package orchestrator

import (
	"context"
	"strings"
	"testing"

	"intern/internal/config"
)

const importSample = `
epics:
  - id: SETUP
    title: Skeleton
    tickets:
      - id: SETUP-1
        title: Scaffold
        description: Create the layout.
        acceptance_criteria: ["builds"]
  - id: AUTH
    title: Auth
    tickets:
      - id: AUTH-2
        title: Login
        description: POST /login.
        acceptance_criteria: ["200 on valid credentials"]
        blocked_by: [AUTH-1]
      - id: AUTH-1
        title: User store
        description: Store users.
        acceptance_criteria: ["hashed passwords"]
        blocked_by: [SETUP-1]
`

func TestImportBacklog_CreatesInOrderWithLinks(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{JiraProject: "P", AgentUsername: "intern"})
	dir := t.TempDir()
	writeFile(t, dir, ".intern/backlog.yaml", importSample)

	res, err := c.ImportBacklog(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, ct := range res.Created {
		order = append(order, ct.ID+"="+ct.Key)
	}
	if got := strings.Join(order, ","); got != "SETUP-1=P-101,AUTH-1=P-102,AUTH-2=P-103" {
		t.Errorf("created = %s", got)
	}
	if got := strings.Join(ft.links, ","); got != "P-101>P-102,P-102>P-103" {
		t.Errorf("links = %s", got)
	}
	first := ft.created[0]
	if first.AssigneeID != "acct-intern" || first.IssueType != "Task" || first.Project != "P" {
		t.Errorf("ticket fields = %+v", first)
	}
	if !strings.Contains(first.Description, "Acceptance criteria:\n- builds") || first.Labels[1] != "epic-setup" {
		t.Errorf("ticket content = %+v", first)
	}

	// Re-importing creates nothing new.
	res, err = c.ImportBacklog(context.Background(), dir)
	if err != nil || len(res.Created) != 0 || res.Existing != 3 {
		t.Errorf("re-import = %+v, %v", res, err)
	}
}

func TestImportBacklog_ResumesAfterFailure(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{JiraProject: "P", JiraAgentAccountID: "acct-x"})
	dir := t.TempDir()
	writeFile(t, dir, ".intern/backlog.yaml", importSample)

	ft.failCreateAt = 2
	if _, err := c.ImportBacklog(context.Background(), dir); err == nil {
		t.Fatal("expected failure on second create")
	}
	if keys := c.State.BacklogKeyMap(); keys["SETUP-1"] != "P-101" || len(keys) != 1 {
		t.Fatalf("mapping after partial import = %v", keys)
	}

	ft.failCreateAt = 0
	res, err := c.ImportBacklog(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 2 || res.Existing != 1 || len(ft.created) != 3 {
		t.Errorf("resume created %+v (total %d)", res, len(ft.created))
	}
	if ft.created[1].AssigneeID != "acct-x" {
		t.Error("configured account ID should be used without lookup")
	}
}

func TestImportBacklog_RejectsInvalid(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{JiraProject: "P"})
	dir := t.TempDir()
	writeFile(t, dir, ".intern/backlog.yaml", "epics:\n  - id: E\n    title: E\n    tickets:\n      - id: A\n        title: A\n        description: d\n        acceptance_criteria: []\n")
	if _, err := c.ImportBacklog(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "no acceptance criteria") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if len(ft.created) != 0 {
		t.Error("nothing should be created from an invalid backlog")
	}
}
