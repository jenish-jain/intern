package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"intern/internal/config"
	"intern/internal/runner"
)

const planBacklog = `epics:
  - id: SETUP
    title: Skeleton
    tickets:
      - id: SETUP-1
        title: Scaffold
        description: Create the layout so verify passes.
        acceptance_criteria: ["go test ./... passes"]
  - id: NOTES
    title: Notes
    tickets:
      - id: NOTES-1
        title: Create a note
        description: POST /notes stores a note.
        acceptance_criteria: ["returns 201 with the note id"]
        blocked_by: [SETUP-1]
`

func architectStep(status runner.Status, summary string, write func(dir string), questions ...string) func(runner.Job) runner.Result {
	return func(j runner.Job) runner.Result {
		if write != nil {
			write(j.Workdir)
		}
		return runner.Result{Status: status, Summary: summary, Questions: questions, SessionID: "arch-1", Usage: runner.Usage{CostUSD: 0.3}}
	}
}

func TestConverse_QuestionsDraftApproveAndChange(t *testing.T) {
	var fx *testing.T = t
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleArchitect: {
			architectStep(runner.StatusNeedsHuman, "I read the PRD.", func(dir string) {
				writeFile(fx, dir, "docs/PRD.md", "# Notes app\n")
			}, "Do notes need sharing? (default: no)"),
			architectStep(runner.StatusDone, "Planned a notes API in two epics.", func(dir string) {
				writeFile(fx, dir, "docs/architecture.md", "# Architecture\n")
				writeFile(fx, dir, ".intern/backlog.yaml", planBacklog)
			}),
			architectStep(runner.StatusDone, "Added note deletion.", func(dir string) {
				writeFile(fx, dir, ".intern/backlog.yaml", planBacklog+`      - id: NOTES-2
        title: Delete a note
        description: DELETE /notes/{id}.
        acceptance_criteria: ["returns 204"]
        blocked_by: [NOTES-1]
`)
			}),
		},
	}}
	c, repo, ft := newFlowFixture(t, &config.Config{JiraProject: "P", JiraAgentAccountID: "acct", MergeMethod: "squash"}, rr)
	ctx := context.Background()

	reply, err := c.Converse(ctx, "C1-123.456", "Build a notes app: users create and delete notes.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "1. Do notes need sharing?") {
		t.Errorf("reply should ask the question:\n%s", reply)
	}
	conv, _ := c.State.GetConversation("C1-123.456")
	if conv.Stage != ConvShaping || conv.SessionID != "arch-1" || conv.PRURL == "" {
		t.Fatalf("after round 1: %+v", conv)
	}
	if got := currentBranch(t, repo.dir); got != "master" {
		t.Errorf("clone left on %s", got)
	}
	first := rr.jobs[0]
	if first.Role != runner.RoleArchitect || !strings.Contains(first.Description, "notes app") || first.SessionID != "" {
		t.Errorf("first architect job = %+v", first)
	}

	reply, err = c.Converse(ctx, "C1-123.456", "No sharing.")
	if err != nil {
		t.Fatal(err)
	}
	if rr.jobs[1].SessionID != "arch-1" {
		t.Error("second message should resume the architect session")
	}
	for _, want := range []string{"Planned a notes API", "*Proposed backlog*", "NOTES-1 Create a note", "Reply *approve*"} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q:\n%s", want, reply)
		}
	}
	if conv, _ = c.State.GetConversation("C1-123.456"); conv.Stage != ConvAwaitingApproval {
		t.Fatalf("stage = %s", conv.Stage)
	}
	if out, _ := exec.Command("git", "-C", repo.dir, "show", conv.Branch+":docs/architecture.md").Output(); len(out) == 0 {
		t.Error("plan docs not committed on the plan branch")
	}

	reply, err = c.Converse(ctx, "C1-123.456", "approve")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "created 2 tickets") || !strings.Contains(reply, "NOTES-1 → P-102") {
		t.Errorf("approval reply:\n%s", reply)
	}
	if len(ft.created) != 2 || ft.links[0] != "P-101>P-102" {
		t.Errorf("created %d, links %v", len(ft.created), ft.links)
	}
	if _, err := os.Stat(filepath.Join(repo.dir, ".intern/backlog.yaml")); err != nil {
		t.Error("approved plan should be on master")
	}

	reply, err = c.Converse(ctx, "C1-123.456", "Also let users delete notes.")
	if err != nil {
		t.Fatal(err)
	}
	conv, _ = c.State.GetConversation("C1-123.456")
	if conv.Round != 2 || !strings.HasSuffix(conv.Branch, "-r2") || conv.Stage != ConvAwaitingApproval {
		t.Fatalf("change request should open round 2: %+v", conv)
	}
	if !strings.Contains(rr.jobs[2].Feedback, "NOTES-1 (P-102)") {
		t.Errorf("architect should be told which tickets exist:\n%s", rr.jobs[2].Feedback)
	}
	if !strings.Contains(reply, "already P-101") || !strings.Contains(reply, "NOTES-2 Delete a note") {
		t.Errorf("round 2 reply:\n%s", reply)
	}
	if _, err := c.Converse(ctx, "C1-123.456", "approve"); err != nil {
		t.Fatal(err)
	}
	if len(ft.created) != 3 || ft.created[2].Summary != "Delete a note" {
		t.Errorf("round 2 should create only the new ticket, created %d", len(ft.created))
	}
}

func TestConverse_RejectsCodeChanges(t *testing.T) {
	var fx *testing.T = t
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleArchitect: {architectStep(runner.StatusDone, "done", func(dir string) {
			writeFile(fx, dir, "main.go", "package main\n")
		})},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{}, rr)
	_, err := c.Converse(context.Background(), "C2", "build it")
	if err == nil || !strings.Contains(err.Error(), "outside docs/") {
		t.Fatalf("expected gate error, got %v", err)
	}
	if len(repo.pushes) != 0 {
		t.Error("nothing should be pushed")
	}
	if _, err := os.Stat(filepath.Join(repo.dir, "main.go")); !os.IsNotExist(err) {
		t.Error("rejected change should be discarded")
	}
}

func TestConverse_InvalidBacklogIsNotApprovable(t *testing.T) {
	var fx *testing.T = t
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleArchitect: {architectStep(runner.StatusDone, "planned", func(dir string) {
			writeFile(fx, dir, ".intern/backlog.yaml", "epics:\n  - id: E\n    title: E\n    tickets:\n      - id: A\n        title: A\n        description: d\n        acceptance_criteria: []\n")
		})},
	}}
	c, _, ft := newFlowFixture(t, &config.Config{}, rr)
	ctx := context.Background()
	reply, err := c.Converse(ctx, "C3", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "no acceptance criteria") {
		t.Errorf("reply should show validation problems:\n%s", reply)
	}
	if conv, _ := c.State.GetConversation("C3"); conv.Stage != ConvShaping {
		t.Errorf("stage = %s", conv.Stage)
	}
	// "approve" now goes to the architect as a message, not to import.
	rr.steps[runner.RoleArchitect] = append(rr.steps[runner.RoleArchitect], architectStep(runner.StatusDone, "fixed", nil))
	if _, err := c.Converse(ctx, "C3", "approve"); err != nil {
		t.Fatal(err)
	}
	if len(ft.created) != 0 {
		t.Error("an invalid backlog must not be imported")
	}
}

func TestIsApproval(t *testing.T) {
	for msg, want := range map[string]bool{
		"approve": true, "Approved!": true, " LGTM ": true, "ship it.": true,
		"approve but rename AUTH-1": false, "don't approve": false, "": false,
	} {
		if got := IsApproval(msg); got != want {
			t.Errorf("IsApproval(%q) = %v", msg, got)
		}
	}
}

func TestConverse_LegacyRunnerExplainsSetup(t *testing.T) {
	c, repo, _ := newFlowFixture(t, &config.Config{}, &roleRunner{})
	c.Runner = newLegacyRunner(c)
	reply, err := c.Converse(context.Background(), "C9", "plan: build a todo app")
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if !strings.Contains(reply, "AI_RUNNER=claude-code") {
		t.Errorf("reply should explain the runner setting, got %q", reply)
	}
	if len(repo.pushes) != 0 {
		t.Error("nothing should be pushed")
	}
}
