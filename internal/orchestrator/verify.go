package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"intern/internal/config"
	"intern/internal/gate"
	"intern/internal/project"
	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// NeedsHumanError means the runner stopped because it needs an answer.
// The questions are posted on the ticket; editing the ticket with the
// answer makes it eligible for another attempt.
type NeedsHumanError struct {
	Questions []string
	Summary   string
}

func (e *NeedsHumanError) Error() string {
	return "needs human input: " + strings.Join(e.Questions, "; ")
}

// Commit identity for intern's commits.
const (
	commitAuthorName  = "AI Intern"
	commitAuthorEmail = "ai-intern@example.com"
)

// errNoChanges means the runner finished without changing anything.
var errNoChanges = errors.New("no effective changes were produced for this ticket")

// legacyVerifyCommands maps the original Go-only quality gate flags onto
// verify commands, for target repos without .intern/project.yaml.
func legacyVerifyCommands(cfg *config.Config) []project.Command {
	var cmds []project.Command
	if cfg.RunVetBeforePR {
		cmds = append(cmds, project.Command{Name: "go vet", Run: "go vet ./..."})
	}
	if cfg.RunTestsBeforePR {
		cmds = append(cmds, project.Command{Name: "go test", Run: "go test ./..."})
	}
	return cmds
}

// verifyCommands loads the project contract and returns it with the
// commands to run on a change: setup then verify, or the legacy Go
// commands when the repo has no contract.
func (c *Coordinator) verifyCommands(repoRoot string) (project.Config, []project.Command, error) {
	proj, found, err := project.Load(repoRoot)
	if err != nil {
		return proj, nil, err
	}
	if !found {
		return proj, legacyVerifyCommands(c.Cfg), nil
	}
	return proj, append(append([]project.Command{}, proj.Setup...), proj.Verify...), nil
}

// workOutcome is the result of running a job through gate and verify.
type workOutcome struct {
	Result  runner.Result // last runner result; Usage is the total across rounds
	Changes []gate.Change
	Checks  []project.CheckResult
	Notes   []string // judgment-call notes from every round
}

// runJob runs a job and turns a non-done status into an error.
func (c *Coordinator) runJob(ctx context.Context, job runner.Job) (runner.Result, error) {
	res, err := c.Runner.Run(ctx, job)
	if err != nil {
		return res, err
	}
	switch res.Status {
	case runner.StatusDone:
		return res, nil
	case runner.StatusNeedsHuman:
		qs := res.Questions
		if len(qs) == 0 {
			qs = []string{res.Summary}
		}
		return res, &NeedsHumanError{Questions: qs, Summary: res.Summary}
	default:
		msg := res.Summary
		if msg == "" {
			msg = "runner reported failure"
		}
		return res, fmt.Errorf("runner failed: %s", msg)
	}
}

// developAndVerify runs the developer job, then loops: gate the changes,
// commit them, run the project's verify commands, and on failure hand the
// output back to the runner, up to the configured number of fix rounds.
func (c *Coordinator) developAndVerify(ctx context.Context, job runner.Job, base string) (workOutcome, error) {
	repoRoot := job.Workdir
	var out workOutcome

	res, err := c.runJob(ctx, job)
	out.addUsage(res.Usage)
	out.Result = res
	out.Notes = append(out.Notes, res.Notes...)
	if err != nil {
		return out, err
	}

	proj, verifyCmds, err := c.verifyCommands(repoRoot)
	if err != nil {
		return out, err
	}
	policy := gate.Policy{
		AllowedDirs:    c.Cfg.AllowedWriteDirs,
		MaxFiles:       c.Cfg.PlanMaxFiles,
		ProtectedPaths: proj.ProtectedPaths,
	}

	for round := 0; ; round++ {
		if err := checkContext(ctx, job.TicketKey, "before gate"); err != nil {
			return out, err
		}
		changes, violations, err := gate.Check(ctx, repoRoot, base, policy)
		if err != nil {
			return out, fmt.Errorf("gate: %w", err)
		}
		if len(violations) > 0 {
			msgs := make([]string, len(violations))
			for i, v := range violations {
				msgs[i] = v.String()
			}
			return out, fmt.Errorf("change rejected by gate:\n- %s", strings.Join(msgs, "\n- "))
		}
		if len(changes) == 0 {
			return out, errNoChanges
		}
		out.Changes = changes

		msg := fmt.Sprintf("feat(%s): %s", job.TicketKey, job.Summary)
		if round > 0 {
			msg = fmt.Sprintf("fix(%s): address verify failures (round %d)", job.TicketKey, round)
		}
		if err := c.commitStaged(ctx, repoRoot, msg); err != nil {
			return out, err
		}

		checks, ok := project.RunCommands(ctx, repoRoot, verifyCmds)
		out.Checks = checks
		if ok {
			if round > 0 {
				c.Metrics.IncHealSuccesses()
			}
			return out, nil
		}
		report := project.FailureReport(checks)
		if round >= c.Cfg.VerifyFixAttempts {
			if round > 0 {
				c.Metrics.IncHealFailures()
			}
			return out, fmt.Errorf("verify failed after %d fix rounds:\n%s", round, report)
		}

		logger.Warn("Verify failed; asking runner to fix", "ticket", job.TicketKey, "round", round+1)
		c.Metrics.AddHealAttempts(1)
		fixJob := job
		fixJob.Feedback = "The change fails the project's verify commands. Fix the code so they pass.\n\n" + report
		fixJob.SessionID = out.Result.SessionID
		res, err := c.runJob(ctx, fixJob)
		out.addUsage(res.Usage)
		out.Notes = append(out.Notes, res.Notes...)
		if res.SessionID != "" {
			out.Result.SessionID = res.SessionID
		}
		if err != nil {
			return out, err
		}
	}
}

// addUsage adds u to the outcome's running total.
func (o *workOutcome) addUsage(u runner.Usage) {
	o.Result.Usage = o.Result.Usage.Add(u)
}

// commitStaged commits whatever gate.Check staged, if anything.
func (c *Coordinator) commitStaged(ctx context.Context, repoRoot, message string) error {
	out, err := runCommandCapture(ctx, repoRoot, "git", "diff", "--cached", "--quiet")
	if err == nil {
		return nil // nothing staged; the runner committed everything itself
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("git diff --cached: %w: %s", err, out)
	}
	// The git CLI commits in whichever checkout repoRoot is: the main clone
	// or a ticket's worktree.
	if out, err := runCommandCapture(ctx, repoRoot, "git",
		"-c", "user.name="+commitAuthorName, "-c", "user.email="+commitAuthorEmail,
		"commit", "-q", "--no-verify", "-m", message); err != nil {
		return fmt.Errorf("commit: %w: %s", err, out)
	}
	return nil
}
