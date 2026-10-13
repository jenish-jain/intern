package orchestrator

import (
	"fmt"
	"strings"

	"intern/internal/gate"
)

func buildPRTitle(ticketKey, summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ticketKey
	}
	return fmt.Sprintf("%s: %s", ticketKey, summary)
}

// prBodyInput is what goes into a PR description.
type prBodyInput struct {
	TicketKey   string
	Summary     string
	Description string
	WorkSummary string // the runner's own account of what it did
	Changes     []gate.Change
	Notes       []string // judgment calls for a human to confirm
	Checks      []string // verify results
}

var changeVerbs = map[string]string{"A": "added", "M": "modified", "D": "deleted", "T": "type changed"}

// buildPRBody renders the PR description in markdown.
func buildPRBody(in prBodyInput) string {
	var b strings.Builder
	b.WriteString("## Ticket\n")
	b.WriteString(fmt.Sprintf("- Key: %s\n", in.TicketKey))
	if strings.TrimSpace(in.Summary) != "" {
		b.WriteString(fmt.Sprintf("- Summary: %s\n", in.Summary))
	}
	b.WriteString("\n## Description\n")
	if strings.TrimSpace(in.Description) == "" {
		b.WriteString("(no description provided)\n")
	} else {
		b.WriteString(in.Description)
		b.WriteString("\n")
	}
	if strings.TrimSpace(in.WorkSummary) != "" {
		b.WriteString("\n## What was done\n")
		b.WriteString(strings.TrimSpace(in.WorkSummary))
		b.WriteString("\n")
	}
	b.WriteString("\n## Changeset\n")
	if len(in.Changes) == 0 {
		b.WriteString("(no changes)\n")
	} else {
		for _, ch := range in.Changes {
			verb := changeVerbs[ch.Status]
			if verb == "" {
				verb = ch.Status
			}
			b.WriteString(fmt.Sprintf("- %s (%s)\n", ch.Path, verb))
		}
	}
	if len(in.Notes) > 0 {
		b.WriteString("\n## Judgment calls to confirm\n")
		for _, n := range in.Notes {
			b.WriteString(fmt.Sprintf("- %s\n", n))
		}
	}
	b.WriteString("\n## Verify\n")
	if len(in.Checks) == 0 {
		b.WriteString("No verify commands configured (add .intern/project.yaml to the repo).\n")
	} else {
		for _, c := range in.Checks {
			if strings.HasPrefix(c, "```") {
				b.WriteString(c + "\n")
			} else {
				b.WriteString("- " + c + "\n")
			}
		}
	}
	return b.String()
}
