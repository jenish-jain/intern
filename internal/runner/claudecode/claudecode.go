// Package claudecode runs a role's job with the Claude Code CLI in
// headless mode. The model explores the repository, edits files and runs
// commands itself, then returns a structured result.
package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"intern/internal/personas"
	"intern/internal/project"
	"intern/internal/runner"
	"intern/internal/sandbox"

	logger "github.com/jenish-jain/logger"
)

// DefaultTimeout bounds one job when the job sets no timeout.
const DefaultTimeout = 45 * time.Minute

// commonProps are the result fields every role returns.
const commonProps = `"status":{"type":"string","enum":["done","needs_human","failed"]},` +
	`"summary":{"type":"string"},` +
	`"notes":{"type":"array","items":{"type":"string"}},` +
	`"questions":{"type":"array","items":{"type":"string"}}`

// schemas are the structured outputs each role must end with.
var schemas = map[runner.Role]string{
	runner.RoleDeveloper: `{"type":"object","additionalProperties":false,"required":["status","summary"],"properties":{` + commonProps + `}}`,
	runner.RoleReviewer: `{"type":"object","additionalProperties":false,"required":["status","summary","verdict"],"properties":{` + commonProps + `,` +
		`"verdict":{"type":"string","enum":["approve","request_changes"]},` +
		`"comments":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["path","line","body"],"properties":{` +
		`"path":{"type":"string"},"line":{"type":"integer"},"body":{"type":"string"}}}}}}`,
	runner.RoleTester: `{"type":"object","additionalProperties":false,"required":["status","summary","tests_passed"],"properties":{` + commonProps + `,` +
		`"tests_passed":{"type":"boolean"},` +
		`"failures":{"type":"array","items":{"type":"string"}}}}`,
	runner.RoleArchitect: `{"type":"object","additionalProperties":false,"required":["status","summary"],"properties":{` + commonProps + `}}`,
}

// editTools change the workspace; reviewers don't get them.
var editTools = []string{"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash(git add:*)", "Bash(git commit:*)"}

// readOnlyTools are always allowed when the sandbox is not isolated.
// Roles that edit also get editTools.
var readOnlyTools = []string{
	"Read", "Glob", "Grep", "TodoWrite",
	"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
	"Bash(ls:*)", "Bash(cat:*)", "Bash(find:*)",
}

// deniedTools are never allowed: the orchestrator owns remote side effects.
var deniedTools = []string{
	"Bash(git push:*)", "Bash(git remote:*)", "Bash(gh:*)", "WebSearch",
}

// Config configures the runner.
type Config struct {
	// Binary is the Claude Code CLI ("claude" when empty).
	Binary string
	// Model is passed to --model when set (an alias like "opus" or a full ID).
	Model string
	// Effort is passed to --effort when set.
	Effort string
	// APIKey is passed to the process as ANTHROPIC_API_KEY when set.
	// Without it the CLI uses its own login, which only works locally.
	APIKey  string
	Sandbox sandbox.Sandbox
	// AllowedCommands are extra Bash command prefixes the agent may run
	// when the sandbox is not isolated, e.g. "go test", "npm run".
	AllowedCommands []string
	// DefaultBudgetUSD caps spend per job when the job sets no budget.
	DefaultBudgetUSD float64
	// Timeout bounds a job when the job sets none (DefaultTimeout if 0).
	Timeout time.Duration
}

// Runner runs jobs with Claude Code.
type Runner struct {
	cfg Config
}

var _ runner.Runner = (*Runner)(nil)

// New returns a Claude Code runner.
func New(cfg Config) *Runner {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	if cfg.Sandbox == nil {
		cfg.Sandbox = &sandbox.Local{}
	}
	return &Runner{cfg: cfg}
}

// Run runs the job and maps the CLI's final result event to a Result.
func (r *Runner) Run(ctx context.Context, job runner.Job) (runner.Result, error) {
	persona, err := personas.For(job.Role)
	if err != nil {
		return runner.Result{}, err
	}
	if schemas[job.Role] == "" {
		return runner.Result{}, fmt.Errorf("no result schema for role %q", job.Role)
	}
	timeout := job.Budget.Timeout
	if timeout <= 0 {
		timeout = r.cfg.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := r.args(job, persona)
	var env []string
	if r.cfg.APIKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+r.cfg.APIKey)
	}
	env = append(env, "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")

	var stdout, stderr bytes.Buffer
	logger.Info("Starting Claude Code job", "ticket", job.TicketKey, "role", job.Role, "resume", job.SessionID != "")
	runErr := r.cfg.Sandbox.Run(ctx, sandbox.Cmd{
		Dir:    job.Workdir,
		Args:   append([]string{r.cfg.Binary}, args...),
		Env:    env,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	final, parseErr := parseStream(&stdout)
	if final == nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return runner.Result{}, fmt.Errorf("claude code job timed out after %s", timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if runErr != nil {
			return runner.Result{}, fmt.Errorf("claude code: %w: %s", runErr, truncate(msg, 2000))
		}
		if parseErr != nil {
			return runner.Result{}, fmt.Errorf("claude code output: %w", parseErr)
		}
		return runner.Result{}, fmt.Errorf("claude code produced no result event: %s", truncate(msg, 2000))
	}
	return final.toResult(), nil
}

// args builds the CLI arguments for a job.
func (r *Runner) args(job runner.Job, persona string) []string {
	args := []string{
		"-p", prompt(job),
		"--output-format", "stream-json",
		"--verbose",
		"--append-system-prompt", persona,
		"--json-schema", schemas[job.Role],
		// Load only the target repo's settings, never the host user's.
		"--setting-sources", "project",
	}
	if job.SessionID != "" {
		args = append(args, "--resume", job.SessionID)
	}
	if r.cfg.Model != "" {
		args = append(args, "--model", r.cfg.Model)
	}
	if r.cfg.Effort != "" {
		args = append(args, "--effort", r.cfg.Effort)
	}
	budget := job.Budget.MaxCostUSD
	if budget <= 0 {
		budget = r.cfg.DefaultBudgetUSD
	}
	if budget > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(budget, 'f', 2, 64))
	}
	if r.cfg.Sandbox.Isolated() {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		allowed := append([]string{}, readOnlyTools...)
		if job.Role != runner.RoleReviewer {
			allowed = append(allowed, editTools...)
		}
		for _, c := range append(append([]string{}, r.cfg.AllowedCommands...), projectCommands(job.Workdir)...) {
			if c = strings.TrimSpace(c); c != "" {
				allowed = append(allowed, "Bash("+c+":*)")
			}
		}
		args = append(args, "--permission-mode", "acceptEdits", "--allowedTools", strings.Join(allowed, ","))
	}
	denied := append([]string{}, deniedTools...)
	if job.Role == runner.RoleReviewer {
		denied = append(denied, editTools...)
	}
	return append(args, "--disallowedTools", strings.Join(denied, ","))
}

// projectCommands returns the target repo's setup and verify commands, so
// a local-sandbox agent may run exactly what will be checked.
func projectCommands(workdir string) []string {
	cfg, found, err := project.Load(workdir)
	if err != nil || !found {
		return nil
	}
	var cmds []string
	for _, c := range append(append([]project.Command{}, cfg.Setup...), cfg.Verify...) {
		if strings.ContainsAny(c.Run, "\n;&|") {
			continue // compound commands can't be expressed as a prefix rule
		}
		cmds = append(cmds, strings.TrimSpace(c.Run))
	}
	return cmds
}

// prompt is the user message for a job. A follow-up (resumed) job only
// needs the new input; the ticket is already in the session.
func prompt(job runner.Job) string {
	if job.SessionID != "" && job.Feedback != "" {
		return job.Feedback
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Ticket %s: %s\n\n", job.TicketKey, job.Summary)
	if strings.TrimSpace(job.Description) != "" {
		b.WriteString("## Description\n")
		b.WriteString(strings.TrimSpace(job.Description))
		b.WriteString("\n")
	}
	if job.Feedback != "" {
		b.WriteString("\n## Feedback on the previous attempt\n")
		b.WriteString(job.Feedback)
		b.WriteString("\n")
	}
	return b.String()
}

// resultEvent is the CLI's final stream-json event.
type resultEvent struct {
	Type             string  `json:"type"`
	Subtype          string  `json:"subtype"`
	IsError          bool    `json:"is_error"`
	Result           string  `json:"result"`
	SessionID        string  `json:"session_id"`
	NumTurns         int     `json:"num_turns"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	TerminalReason   string  `json:"terminal_reason"`
	StructuredOutput *struct {
		Status    string   `json:"status"`
		Summary   string   `json:"summary"`
		Notes     []string `json:"notes"`
		Questions []string `json:"questions"`
		Verdict   string   `json:"verdict"`
		Comments  []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Body string `json:"body"`
		} `json:"comments"`
		TestsPassed *bool    `json:"tests_passed"`
		Failures    []string `json:"failures"`
	} `json:"structured_output"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
}

// parseStream reads stream-json lines and returns the last result event.
func parseStream(r io.Reader) (*resultEvent, error) {
	var final *resultEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &head) != nil || head.Type != "result" {
			continue
		}
		var ev resultEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return final, fmt.Errorf("decode result event: %w", err)
		}
		final = &ev
	}
	return final, sc.Err()
}

func (e *resultEvent) toResult() runner.Result {
	res := runner.Result{
		SessionID: e.SessionID,
		Turns:     e.NumTurns,
		Usage: runner.Usage{
			InputTokens:  e.Usage.InputTokens + e.Usage.CacheCreationInputTokens + e.Usage.CacheReadInputTokens,
			OutputTokens: e.Usage.OutputTokens,
			CostUSD:      e.TotalCostUSD,
		},
	}
	if e.IsError || e.Subtype != "success" {
		res.Status = runner.StatusFailed
		reason := e.Subtype
		if e.TerminalReason != "" && e.TerminalReason != "completed" {
			reason += " (" + e.TerminalReason + ")"
		}
		res.Summary = strings.TrimSpace("Claude Code stopped: " + reason + ". " + truncate(e.Result, 1000))
		return res
	}
	so := e.StructuredOutput
	if so == nil {
		res.Status = runner.StatusFailed
		res.Summary = "Claude Code finished without the structured result. Last message: " + truncate(e.Result, 1000)
		return res
	}
	res.Summary, res.Notes, res.Questions = so.Summary, so.Notes, so.Questions
	if so.Verdict != "" {
		rv := &runner.Review{Approve: so.Verdict == "approve"}
		for _, c := range so.Comments {
			rv.Comments = append(rv.Comments, runner.ReviewComment{Path: c.Path, Line: c.Line, Body: c.Body})
		}
		res.Review = rv
	}
	if so.TestsPassed != nil {
		res.Test = &runner.TestReport{Passed: *so.TestsPassed, Failures: so.Failures}
	}
	switch so.Status {
	case "done":
		res.Status = runner.StatusDone
	case "needs_human":
		res.Status = runner.StatusNeedsHuman
	default:
		res.Status = runner.StatusFailed
	}
	return res
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
