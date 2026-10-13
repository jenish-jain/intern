package orchestrator

import (
	"context"
	"slices"
	"testing"

	"intern/internal/config"
	"intern/internal/repository"
	"intern/internal/ticketing"
)

// After a restart the process has no flows. An In Review ticket's PR must
// be found again by its branch.
func TestTrackReviewTicket_AfterRestart(t *testing.T) {
	inReview := ticketing.Ticket{Key: "IP-1", Summary: "Scaffold", Status: ticketing.StatusInReview}
	prURL := "https://github.com/acme/app/pull/14"
	cases := []struct {
		name      string
		pr        repository.PRState
		found     string
		wantStage Stage // "" means no flow
		wantDone  bool
		wantLabel string
	}{
		{name: "merged PR moves the ticket to Done", pr: repository.PRState{Merged: true, Closed: true}, found: prURL, wantDone: true},
		{name: "closed PR is left for a person", pr: repository.PRState{Closed: true}, found: prURL},
		{name: "no PR on the branch", found: ""},
		{name: "open PR resumes at its labelled stage", pr: repository.PRState{Labels: []string{"size/L", "intern:testing"}}, found: prURL, wantStage: StageTest},
		{name: "ready PR keeps waiting for its merge", pr: repository.PRState{Labels: []string{"intern:ready-to-merge"}}, found: prURL, wantStage: StageMerge},
		{name: "open PR without a stage label starts at review", pr: repository.PRState{}, found: prURL, wantStage: StageReview, wantLabel: "intern:review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, ft := newFlowFixture(t, &config.Config{ReviewEnabled: true, TestEnabled: true, BranchPrefix: "feature/", MaxTicketAttempts: 3}, &roleRunner{})
			repo.pr, repo.foundPR = tc.pr, tc.found

			c.dispatchTickets(context.Background(), []ticketing.Ticket{inReview}, func(context.Context, string, string, string) error {
				t.Fatal("an In Review ticket must not go back to the developer")
				return nil
			})

			if done := slices.Contains(ft.transitions, "IP-1:Done"); done != tc.wantDone {
				t.Errorf("moved to Done = %v, want %v (transitions %v)", done, tc.wantDone, ft.transitions)
			}
			flows := c.State.ListFlows()
			switch {
			case tc.wantStage == "" && len(flows) != 0:
				t.Errorf("want no flow, got %+v", flows)
			case tc.wantStage != "" && (len(flows) != 1 || flows[0].Stage != tc.wantStage || flows[0].PRURL != prURL || flows[0].Branch != "feature/ip-1"):
				t.Errorf("want one flow at %s for %s, got %+v", tc.wantStage, prURL, flows)
			}
			if tc.wantLabel != "" && !repo.pr.HasLabel(tc.wantLabel) {
				t.Errorf("PR should get %s, labels %v", tc.wantLabel, repo.pr.Labels)
			}
		})
	}
}

// A flow this process already tracks is left to advanceFlows.
func TestTrackReviewTicket_TrackedFlowUntouched(t *testing.T) {
	c, repo, ft := newFlowFixture(t, &config.Config{ReviewEnabled: true, BranchPrefix: "feature/"}, &roleRunner{})
	repo.pr, repo.foundPR = repository.PRState{Merged: true}, "https://github.com/acme/app/pull/14"
	c.State.SaveFlow(Flow{TicketKey: "IP-1", Stage: StageReview, PRURL: "https://github.com/acme/app/pull/14"})

	c.trackReviewTicket(context.Background(), ticketing.Ticket{Key: "IP-1", Status: ticketing.StatusInReview})

	if len(ft.transitions) != 0 {
		t.Errorf("a tracked flow shouldn't be touched, got transitions %v", ft.transitions)
	}
}
