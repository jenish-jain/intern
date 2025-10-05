package ai

import (
	"fmt"
	"strings"
)

// PlanPromptOptions configures the prompt generation
type PlanPromptOptions struct {
	AllowBase64 bool
	MaxNotes    int // reserved for future use (e.g., additional hints)
}

// BuildPlanChangesPrompt builds a strict JSON-only prompt for planning code changes.
// It asks for a JSON array of CodeChange with optional content_b64 to avoid escaping issues.
func BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext string, opts PlanPromptOptions) string {
	var rules []string
	rules = append(rules, "Output ONLY compact JSON. No markdown, no backticks, no commentary.follow the instructions given in ticket carefully and adher to acceptance criteria if any in the ticket and make sure all are fullfilled and don't add any additional changes that are not in the ticket.")
	rules = append(rules, "Schema: [{\"path\":\"relative/path.ext\",\"operation\":\"create|update\",\"content\":\"full file content\"}]")
	if opts.AllowBase64 {
		rules = append(rules, "You MAY use {\"content_b64\":\"<base64>\"} instead of content for large or complex content.")
	}
	rules = append(rules, "try compiling code if possible before creating a changeset.")
	rules = append(rules, "Use POSIX-style relative paths under repo root.")

	return fmt.Sprintf(
		"You are a senior Go engineer\nTicket: %s - %s\nDescription:\n%s\n\nRepository context (truncated):\n%s\n\nRules:\n- %s\n\nJSON:",
		strings.TrimSpace(ticketKey),
		strings.TrimSpace(ticketSummary),
		strings.TrimSpace(ticketDescription),
		strings.TrimSpace(repoContext),
		strings.Join(rules, "\n- "),
	)
}
