package orchestrator

import (
	"fmt"
	"intern/internal/ai/agent"
	"strings"
)

func buildPRTitle(ticketKey, summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ticketKey
	}
	return fmt.Sprintf("%s: %s", ticketKey, summary)
}

// buildPRBody renders a markdown body including ticket info, description and file list
func buildPRBody(ticketKey, summary, description string, changes []agent.CodeChange, notes []string) string {
	var b strings.Builder
	b.WriteString("## Ticket\n")
	b.WriteString(fmt.Sprintf("- Key: %s\n", ticketKey))
	if strings.TrimSpace(summary) != "" {
		b.WriteString(fmt.Sprintf("- Summary: %s\n", summary))
	}
	b.WriteString("\n## Description\n")
	if strings.TrimSpace(description) == "" {
		b.WriteString("(no description provided)\n")
	} else {
		b.WriteString(description)
		b.WriteString("\n")
	}
	b.WriteString("\n## Changeset\n")
	if len(changes) == 0 {
		b.WriteString("(no changes)\n")
	} else {
		for _, ch := range changes {
			b.WriteString(fmt.Sprintf("- %s (%s)\n", ch.Path, ch.Operation))
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n## Notes\n")
		for _, n := range notes {
			b.WriteString(fmt.Sprintf("- %s\n", n))
		}
	}
	b.WriteString("\n## Checklist\n")
	b.WriteString("- [ ] Code compiles\n")
	b.WriteString("- [ ] Tests (if any) pass locally\n")
	b.WriteString("- [ ] Review requested\n")
	return b.String()
}
