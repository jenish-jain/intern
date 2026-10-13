package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/runner"
	"intern/internal/ticketing"
)

// The error production hit on 2026-10-13, as the Claude Code runner reports it.
const spendLimitSummary = "Claude Code stopped: success (api_error). API Error: 400 You have reached your specified API usage limits. You will regain access on 2026-11-01 at 00:00 UTC."

func TestDetectOutage(t *testing.T) {
	e, ok := detectOutage(spendLimitSummary)
	if !ok || !strings.Contains(e.Reason, "spend limit") {
		t.Fatalf("spend limit not recognized: %+v %v", e, ok)
	}
	if want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC); !e.Resumes.Equal(want) {
		t.Errorf("Resumes = %v, want %v", e.Resumes, want)
	}
	for text, want := range map[string]bool{
		"API Error: 400 Your credit balance is too low to access the Anthropic API": true,
		"authentication_error: invalid x-api-key":                                   true,
		"Claude Code stopped: error_max_turns":                                      false,
		"API Error: 529 Overloaded":                                                 false,
	} {
		if _, got := detectOutage(text); got != want {
			t.Errorf("detectOutage(%q) = %v, want %v", text, got, want)
		}
	}
}

// outageRunner answers with a fixed sequence of summaries; "ok" succeeds.
type outageRunner struct {
	answers []string
	calls   int
}

func (s *outageRunner) Run(context.Context, runner.Job) (runner.Result, error) {
	a := s.answers[s.calls%len(s.answers)]
	s.calls++
	if a == "ok" {
		return runner.Result{Status: runner.StatusDone, Summary: "done"}, nil
	}
	return runner.Result{Status: runner.StatusFailed, Summary: a}, nil
}

type fakeSleeper struct {
	slept int
	err   error
}

func (f *fakeSleeper) Sleep(context.Context) error { f.slept++; return f.err }
func (f *fakeSleeper) WakeCommand(context.Context) string {
	return "gcloud run services update intern --scaling=auto"
}
func (f *fakeSleeper) SleepCommand(context.Context) string {
	return "gcloud run services update intern --scaling=0"
}

func outageCoordinator(t *testing.T, sleepAfter int, answers ...string) (*Coordinator, *outageRunner, *fakeSleeper, *recordingNotifier) {
	t.Helper()
	c, _ := newLifecycleCoordinator(t, &config.Config{SleepAfterFailures: sleepAfter})
	sr := &outageRunner{answers: answers}
	sl := &fakeSleeper{}
	n := &recordingNotifier{}
	c.Runner, c.Sleeper, c.Notifier = sr, sl, n
	return c, sr, sl, n
}

func TestRunModel_SleepsAfterConsecutiveOutages(t *testing.T) {
	c, sr, sl, n := outageCoordinator(t, 3, spendLimitSummary)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		_, err := c.runModel(ctx, runner.Job{})
		var mu *ModelUnavailableError
		if !errors.As(err, &mu) {
			t.Fatalf("call %d: want ModelUnavailableError, got %v", i, err)
		}
		if _, asleep := c.modelPaused(); asleep != (i == 3) {
			t.Errorf("after %d failures asleep = %v", i, asleep)
		}
	}
	if sl.slept != 1 {
		t.Errorf("service scaled to zero %d times, want once", sl.slept)
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "--scaling=auto") || !strings.Contains(n.msgs[0], "2026-11-01") {
		t.Errorf("want one Slack message with the wake command and reset date, got %q", n.msgs)
	}

	// Asleep: nothing more reaches the API, and the poll does nothing.
	if _, err := c.runModel(ctx, runner.Job{}); err == nil || sr.calls != 3 {
		t.Errorf("asleep intern still ran a job (calls %d, err %v)", sr.calls, err)
	}
	if d := c.cycle(ctx, 30*time.Second); d != time.Hour {
		t.Errorf("asleep cycle should idle for an hour, got %v", d)
	}
}

func TestRunModel_SuccessResetsTheCount(t *testing.T) {
	c, _, sl, _ := outageCoordinator(t, 3, spendLimitSummary, spendLimitSummary, "ok", spendLimitSummary, spendLimitSummary)
	for i := 0; i < 5; i++ {
		_, _ = c.runModel(context.Background(), runner.Job{})
	}
	if _, asleep := c.modelPaused(); asleep || sl.slept != 0 {
		t.Errorf("failures weren't consecutive, so intern shouldn't sleep (asleep %v, slept %d)", asleep, sl.slept)
	}
}

func TestRunModel_ZeroNeverSleeps(t *testing.T) {
	c, _, sl, _ := outageCoordinator(t, 0, spendLimitSummary)
	for i := 0; i < 10; i++ {
		_, _ = c.runModel(context.Background(), runner.Job{})
	}
	if _, asleep := c.modelPaused(); asleep || sl.slept != 0 {
		t.Error("SLEEP_AFTER_FAILURES=0 must never sleep")
	}
}

func TestRunModel_SleepRefusedStillStopsWork(t *testing.T) {
	c, _, sl, n := outageCoordinator(t, 1, spendLimitSummary)
	sl.err = errors.New("HTTP 403: permission run.services.update denied")
	_, _ = c.runModel(context.Background(), runner.Job{})
	if _, asleep := c.modelPaused(); !asleep {
		t.Error("work must stop even when the service can't scale down")
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "--scaling=0") || !strings.Contains(n.msgs[0], "403") {
		t.Errorf("Slack should explain the refusal and give the command, got %q", n.msgs)
	}
}

// An outage isn't the ticket's fault: it goes back to To Do without
// using an attempt.
func TestHandleTicketFailure_OutageIsNotAnAttempt(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{MaxTicketAttempts: 3})
	tk := ticketing.Ticket{Key: "IP-5", Summary: "s"}
	fp := ticketFingerprint(tk)
	c.handleTicketFailure(context.Background(), tk, fp, &ModelUnavailableError{Reason: "spend limit"})
	if n := c.State.Attempts("IP-5", fp); n != 0 {
		t.Errorf("attempts = %d, want 0", n)
	}
	if len(ft.comments["IP-5"]) != 0 {
		t.Errorf("no ticket comment expected, got %v", ft.comments["IP-5"])
	}
}

// A failing flow step is announced once per distinct error, not every poll.
func TestFlowStepFailed_NotifiesOnce(t *testing.T) {
	c, _ := newLifecycleCoordinator(t, &config.Config{})
	n := &recordingNotifier{}
	c.Notifier = n
	f := Flow{TicketKey: "IP-5", Stage: StageTest, PRURL: "https://github.com/o/r/pull/21"}
	for i := 0; i < 3; i++ {
		c.flowStepFailed(context.Background(), f, errors.New("tester did not report a result"))
	}
	c.flowStepFailed(context.Background(), f, &ModelUnavailableError{Reason: "spend limit"})
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "IP-5") {
		t.Errorf("want one notification, got %q", n.msgs)
	}
}
