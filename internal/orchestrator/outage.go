package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// ModelUnavailableError means the model API refuses every request until a
// person fixes something (a spend limit, no credit, a rejected key). It
// isn't the ticket's fault, so it doesn't count as an attempt.
type ModelUnavailableError struct {
	Reason string
	// Resumes is when the API says access returns, if it said.
	Resumes time.Time
}

func (e *ModelUnavailableError) Error() string {
	msg := "model API unavailable: " + e.Reason
	if !e.Resumes.IsZero() {
		msg += fmt.Sprintf(" (the API says access returns %s)", e.Resumes.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return msg
}

// Sleeper puts the service intern runs in to sleep, so it stops using
// compute until a person wakes it. See internal/cloudrun.
type Sleeper interface {
	Sleep(ctx context.Context) error
	WakeCommand(ctx context.Context) string
	SleepCommand(ctx context.Context) string
}

// outage tracks consecutive unrecoverable model errors and whether intern
// has gone to sleep. Sleep lasts until the process restarts: waking the
// service starts a fresh process.
type outage struct {
	mu          sync.Mutex
	consecutive int
	asleep      *ModelUnavailableError
}

var regainAccess = regexp.MustCompile(`regain access on (\d{4}-\d{2}-\d{2}) at (\d{2}:\d{2}) UTC`)

// detectOutage recognizes errors that mean every model request will fail
// until a person acts, as opposed to one job failing.
func detectOutage(text string) (*ModelUnavailableError, bool) {
	lower := strings.ToLower(text)
	var reason string
	switch {
	case strings.Contains(lower, "reached your specified api usage limits"):
		reason = "the Anthropic API spend limit set for this organization or workspace was reached (raise it at console.anthropic.com > Settings > Limits)"
	case strings.Contains(lower, "credit balance is too low"):
		reason = "the Anthropic credit balance is too low (console.anthropic.com > Settings > Billing)"
	case strings.Contains(lower, "invalid x-api-key"), strings.Contains(lower, "authentication_error"):
		reason = "ANTHROPIC_API_KEY was rejected"
	default:
		return nil, false
	}
	e := &ModelUnavailableError{Reason: reason}
	if m := regainAccess.FindStringSubmatch(text); m != nil {
		if t, err := time.Parse("2006-01-02 15:04", m[1]+" "+m[2]); err == nil {
			e.Resumes = t
		}
	}
	return e, true
}

// modelPaused reports whether intern is asleep after an outage.
func (c *Coordinator) modelPaused() (*ModelUnavailableError, bool) {
	c.outage.mu.Lock()
	defer c.outage.mu.Unlock()
	return c.outage.asleep, c.outage.asleep != nil
}

// runModel runs a model job unless intern is asleep. A job that hits an
// unrecoverable API error counts toward SLEEP_AFTER_FAILURES; reaching it
// puts intern to sleep. Any successful job resets the count.
func (c *Coordinator) runModel(ctx context.Context, job runner.Job) (runner.Result, error) {
	if e, paused := c.modelPaused(); paused {
		return runner.Result{Status: runner.StatusFailed, Summary: e.Error()}, e
	}
	res, err := c.Runner.Run(ctx, job)
	text := res.Summary
	if err != nil {
		text += " " + err.Error()
	}
	e, unavailable := detectOutage(text)
	c.outage.mu.Lock()
	if !unavailable {
		c.outage.consecutive = 0
		c.outage.mu.Unlock()
		return res, err
	}
	c.outage.consecutive++
	n := c.outage.consecutive
	sleep := c.Cfg.SleepAfterFailures > 0 && n >= c.Cfg.SleepAfterFailures && c.outage.asleep == nil
	if sleep {
		c.outage.asleep = e
	}
	c.outage.mu.Unlock()

	logger.Error("Model API unavailable", "reason", e.Reason, "consecutive", n, "sleep_after", c.Cfg.SleepAfterFailures)
	if sleep {
		c.goToSleep(ctx, e, n)
	}
	return res, e
}

// goToSleep stops all work after an unrecoverable outage, says so in
// Slack with how to wake intern, and scales the service to zero when it
// can, so it stops using compute.
func (c *Coordinator) goToSleep(ctx context.Context, e *ModelUnavailableError, failures int) {
	msg := fmt.Sprintf(":zzz: intern is going to sleep after %d model jobs in a row failed: %s.", failures, e.Reason)
	if !e.Resumes.IsZero() {
		msg += fmt.Sprintf(" The API says access returns %s.", e.Resumes.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if c.Sleeper == nil {
		logger.Error("Going to sleep: all model work stopped until restart", "reason", e.Reason)
		c.notify(ctx, msg+" All work has stopped. Fix the cause, then restart intern to resume.")
		return
	}
	wake := c.Sleeper.WakeCommand(ctx)
	if err := c.Sleeper.Sleep(ctx); err != nil {
		logger.Error("Could not scale the service to zero; staying up with all work stopped", "error", err)
		c.notify(ctx, msg+fmt.Sprintf(" All work has stopped, but I couldn't scale the service down to stop compute billing (%v). To do it yourself:\n`%s`\nWhen the cause is fixed, wake it with:\n`%s`",
			err, c.Sleeper.SleepCommand(ctx), wake))
		return
	}
	logger.Error("Scaled the service to zero; sleeping until woken", "reason", e.Reason)
	c.notify(ctx, msg+fmt.Sprintf(" The service is scaled to zero, so it uses no compute. When the cause is fixed, wake it with:\n`%s`", wake))
}
