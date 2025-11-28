package agent

import (
	"fmt"
	"strings"
)

// PlanPromptOptions configures the prompt generation
type PlanPromptOptions struct {
	AllowBase64 bool
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
	rules = append(rules, "NEVER modify go.mod or go.sum files - these are managed by Go tooling (go get, go mod tidy). Dependencies are already configured.")
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

// BuildFixErrorsPrompt builds a prompt for fixing errors in previously generated code.
// This is used by the self-healing system to iteratively improve code that fails quality gates.
func BuildFixErrorsPrompt(ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []CodeChange, opts PlanPromptOptions) string {
	var rules []string
	rules = append(rules, "Output ONLY compact JSON. No markdown, no backticks, no commentary.")
	rules = append(rules, "Schema: [{\"path\":\"relative/path.ext\",\"operation\":\"create|update\",\"content\":\"full file content\"}]")
	if opts.AllowBase64 {
		rules = append(rules, "You MAY use {\"content_b64\":\"<base64>\"} instead of content for large or complex content.")
	}
	rules = append(rules, "NEVER modify go.mod or go.sum files - these are managed by Go tooling. If errors mention missing go.sum entries, ignore them - the build system will handle this.")
	rules = append(rules, "Fix ONLY the errors shown below. Do not make unrelated changes.")
	rules = append(rules, "Provide SIMPLE, MINIMAL implementations. Prefer standard library over third-party APIs.")
	rules = append(rules, "For authentication: use ONLY jwt-go for JWT validation. Avoid complex third-party auth libraries.")
	rules = append(rules, "If you see interface/type errors with third-party libraries, REMOVE the library usage entirely and use simpler alternatives.")
	rules = append(rules, "Keep implementations under 100 lines when possible to ensure complete JSON output.")
	rules = append(rules, "Use POSIX-style relative paths under repo root.")

	// Build summary of previous changes
	var changesSummary strings.Builder
	changesSummary.WriteString("Previous changes you made:\n")
	for _, change := range previousChanges {
		changesSummary.WriteString(fmt.Sprintf("- %s (%s)\n", change.Path, change.Operation))
	}

	return fmt.Sprintf(
		"You are a senior Go engineer fixing errors in code you previously generated.\n\n"+
			"Original ticket: %s - %s\n\n"+
			"%s\n"+
			"Error type: %s\n"+
			"Error output:\n```\n%s\n```\n\n"+
			"Your task: Fix the errors above using the SIMPLEST possible solution.\n"+
			"IMPORTANT: Generate complete, valid JSON. If your response is too long, simplify the code further.\n\n"+
			"Rules:\n- %s\n\nJSON:",
		strings.TrimSpace(ticketKey),
		strings.TrimSpace(ticketSummary),
		changesSummary.String(),
		errorType,
		strings.TrimSpace(errorOutput),
		strings.Join(rules, "\n- "),
	)
}
