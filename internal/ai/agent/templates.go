package agent

import (
	"fmt"
	"sort"
	"strings"
)

// maxFixContextFileBytes caps how much of any single file's content is
// included in the fix-errors prompt, keeping prompt size bounded when
// previous changes touched large files.
const maxFixContextFileBytes = 20 * 1024

// PlanPromptOptions configures the prompt generation
type PlanPromptOptions struct {
	AllowBase64 bool
}

// BuildPlanChangesPrompt builds a strict JSON-only prompt for planning code changes.

func BuildPlanChangesPrompt(ticketKey, ticketSummary, ticketDescription, repoContext string, opts PlanPromptOptions) string {
	rules := []string{
		"Output ONLY a compact JSON array. No markdown, no backticks, no commentary.",
		`For NEW files: {"path":"relative/path.go","operation":"create","content":"<full file content>"}`,
		`For EXISTING files: {"path":"relative/path.go","operation":"edit","edits":[{"old":"<exact lines copied verbatim from the file, including indentation>","new":"<replacement lines>"}]}`,
		`To delete: {"path":"relative/path.go","operation":"delete"}`,
		"NEVER use operation create on a file that already exists in the repository context - use edit.",
		"In each edit, the old block MUST be copied character-for-character from the provided file content, and MUST be unique within the file. Include 2-3 unchanged surrounding lines for uniqueness.",
		"Keep edits minimal: change only what the ticket requires. Do not reformat or rewrite untouched code.",
		"NEVER modify go.mod or go.sum.",
		"Fulfil every acceptance criterion in the ticket; add nothing beyond it.",
		"Use POSIX-style relative paths under the repo root.",
		`Files marked "(full content)" are shown verbatim and may be edited. Files marked "(signatures only)" show declarations without bodies and CANNOT be safely edited.`,
		`Only use operation=edit on files shown with full content. If you need to modify a file shown as signatures-only, respond with ONLY {"need_files":["relative/path.go", ...]} (no array, no other keys) and nothing else - you will be given the full content and asked again.`,
	}
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
// fileContents maps repo-relative paths to their current on-disk content; it covers every
// file from previousChanges plus every file referenced in errorOutput, so the model can see
// what's actually on disk instead of guessing from a path list.
func BuildFixErrorsPrompt(ticketKey, ticketSummary, errorType, errorOutput string, previousChanges []CodeChange, fileContents map[string]string, opts PlanPromptOptions) string {
	rules := []string{
		"Output ONLY a compact JSON array. No markdown, no backticks, no commentary.",
		`For files shown below: {"path":"relative/path.go","operation":"edit","edits":[{"old":"<exact lines copied verbatim from the file content below, including indentation>","new":"<replacement lines>"}]}`,
		`For genuinely NEW files only: {"path":"relative/path.go","operation":"create","content":"<full file content>"}`,
		`To delete: {"path":"relative/path.go","operation":"delete"}`,
		"Prefer operation=edit for any file shown below - do NOT rewrite a whole file with operation=create. Full-file rewrites during healing are how regressions get introduced.",
		"In each edit, the old block MUST be copied character-for-character from the file content below, and MUST be unique within the file. Include 2-3 unchanged surrounding lines for uniqueness.",
		"Fix ONLY the errors shown below. Do not make unrelated changes.",
		"NEVER modify go.mod or go.sum files - these are managed by Go tooling. If errors mention missing go.sum entries, ignore them - the build system will handle this.",
		"Provide SIMPLE, MINIMAL implementations. Prefer standard library over third-party APIs.",
		"For authentication: use ONLY jwt-go for JWT validation. Avoid complex third-party auth libraries.",
		"If you see interface/type errors with third-party libraries, REMOVE the library usage entirely and use simpler alternatives.",
		"Use POSIX-style relative paths under repo root.",
	}

	// Track which paths were touched by the previous attempt vs. merely
	// referenced in the error output, for labeling below.
	prevOps := make(map[string]CodeChangeOperation, len(previousChanges))
	for _, change := range previousChanges {
		prevOps[change.Path] = change.Operation
	}

	paths := make([]string, 0, len(fileContents))
	for p := range fileContents {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var filesSection strings.Builder
	filesSection.WriteString("Current file contents (this is the ground truth - read it before writing edits):\n")
	for _, p := range paths {
		label := "referenced in error output"
		if op, ok := prevOps[p]; ok {
			label = fmt.Sprintf("previously written via operation=%s", op)
		}
		content := fileContents[p]
		if len(content) > maxFixContextFileBytes {
			content = content[:maxFixContextFileBytes] + "\n... (truncated — focus edits on the error locations)"
		}
		filesSection.WriteString(fmt.Sprintf("\n--- %s (%s) ---\n%s\n", p, label, content))
	}
	if len(paths) == 0 {
		filesSection.WriteString("(no file contents available)\n")
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
		filesSection.String(),
		errorType,
		strings.TrimSpace(errorOutput),
		strings.Join(rules, "\n- "),
	)
}
