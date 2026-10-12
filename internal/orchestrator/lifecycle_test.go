package orchestrator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/journal"
	"intern/internal/repository"
	"intern/internal/ticketing"
)

// fakeTicketing records status transitions and comments.
type fakeTicketing struct {
	mu          sync.Mutex
	transitions []string // "KEY:Status"
	comments    map[string][]string
}

func (f *fakeTicketing) HealthCheck(context.Context) error { return nil }
func (f *fakeTicketing) GetTickets(context.Context, string, string) ([]ticketing.Ticket, error) {
	return nil, nil
}
func (f *fakeTicketing) UpdateTicketStatus(_ context.Context, key, status string, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transitions = append(f.transitions, key+":"+status)
	return nil
}
func (f *fakeTicketing) GetTicket(context.Context, string) (ticketing.Ticket, error) {
	return ticketing.Ticket{}, nil
}
func (f *fakeTicketing) AddComment(_ context.Context, key, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.comments == nil {
		f.comments = map[string][]string{}
	}
	f.comments[key] = append(f.comments[key], body)
	return nil
}
func (f *fakeTicketing) CreateTicket(context.Context, ticketing.NewTicket) (string, error) {
	return "", nil
}
func (f *fakeTicketing) LinkBlocks(context.Context, string, string) error { return nil }

// fakeRepo implements only IsPRMerged; other methods panic if called.
type fakeRepo struct {
	repository.RepositoryClient
	merged map[string]bool
}

func (f *fakeRepo) IsPRMerged(_ context.Context, prURL string) (bool, error) {
	return f.merged[prURL], nil
}

func newLifecycleCoordinator(t *testing.T, cfg *config.Config) (*Coordinator, *fakeTicketing) {
	t.Helper()
	ft := &fakeTicketing{}
	if cfg.MaxTicketAttempts == 0 {
		cfg.MaxTicketAttempts = 2
	}
	if cfg.MaxConcurrentTickets == 0 {
		cfg.MaxConcurrentTickets = 1
	}
	return &Coordinator{
		Ticketing: ticketing.NewService(ft),
		Cfg:       cfg,
		State:     NewState(filepath.Join(t.TempDir(), "state.json")),
		Metrics:   NewMetrics(),
	}, ft
}

func TestDispatch_SuccessMarksProcessed(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{})
	tk := ticketing.Ticket{Key: "P-1", Summary: "s", Description: "d"}

	c.dispatchTickets(context.Background(), []ticketing.Ticket{tk}, func(context.Context, string, string, string) error {
		return nil
	})

	if !c.State.IsProcessed("P-1") {
		t.Error("expected ticket marked processed")
	}
	if len(ft.comments["P-1"]) != 0 {
		t.Errorf("unexpected comments: %v", ft.comments["P-1"])
	}
}

func TestDispatch_FailureCommentsAndReturnsToTodo(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{})
	tk := ticketing.Ticket{Key: "P-1", Summary: "s", Description: "d"}
	fail := func(context.Context, string, string, string) error { return errors.New("quality gates still failing") }

	c.dispatchTickets(context.Background(), []ticketing.Ticket{tk}, fail)

	if c.State.IsProcessed("P-1") {
		t.Error("failed ticket must not be marked processed")
	}
	if got := c.State.Attempts("P-1", ticketFingerprint(tk)); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if len(ft.comments["P-1"]) != 1 || !strings.Contains(ft.comments["P-1"][0], "attempt 1 of 2 failed") {
		t.Errorf("unexpected comments: %v", ft.comments["P-1"])
	}
	if len(ft.transitions) != 1 || ft.transitions[0] != "P-1:To Do" {
		t.Errorf("transitions = %v, want [P-1:To Do]", ft.transitions)
	}
}

func TestDispatch_StopsAtLimitUntilTicketEdited(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{MaxTicketAttempts: 2})
	tk := ticketing.Ticket{Key: "P-1", Summary: "s", Description: "d"}
	calls := 0
	fail := func(context.Context, string, string, string) error { calls++; return errors.New("boom") }

	for i := 0; i < 4; i++ {
		c.dispatchTickets(context.Background(), []ticketing.Ticket{tk}, fail)
	}
	if calls != 2 {
		t.Fatalf("process called %d times, want 2 (limit)", calls)
	}
	last := ft.comments["P-1"][len(ft.comments["P-1"])-1]
	if !strings.Contains(last, "stopped retrying") {
		t.Errorf("final comment should say retries stopped, got %q", last)
	}

	edited := tk
	edited.Description = "d, clarified"
	c.dispatchTickets(context.Background(), []ticketing.Ticket{edited}, fail)
	if calls != 3 {
		t.Errorf("edited ticket should be retried; process called %d times, want 3", calls)
	}
}

func TestDispatch_PanicCountsAsFailure(t *testing.T) {
	c, _ := newLifecycleCoordinator(t, &config.Config{})
	tk := ticketing.Ticket{Key: "P-1"}
	c.dispatchTickets(context.Background(), []ticketing.Ticket{tk}, func(context.Context, string, string, string) error {
		panic("nil map")
	})
	if got := c.State.Attempts("P-1", ticketFingerprint(tk)); got != 1 {
		t.Errorf("attempts after panic = %d, want 1", got)
	}
}

func TestSetStatus_DryRunDoesNotTouchBoard(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{DryRun: true})
	c.setStatus(context.Background(), "P-1", ticketing.StatusInProgress)
	c.comment(context.Background(), "P-1", "hello")
	if len(ft.transitions) != 0 || len(ft.comments) != 0 {
		t.Errorf("dry run touched the board: %v %v", ft.transitions, ft.comments)
	}
}

func TestReconcileMerged_MovesMergedTicketsToDone(t *testing.T) {
	c, ft := newLifecycleCoordinator(t, &config.Config{})
	c.Journal = journal.Load(t.TempDir())
	for _, e := range []journal.Entry{
		{TicketKey: "P-1", Summary: "a", PRURL: "https://github.com/o/r/pull/1", Timestamp: time.Now()},
		{TicketKey: "P-2", Summary: "b", PRURL: "https://github.com/o/r/pull/2", Timestamp: time.Now()},
	} {
		if err := c.Journal.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	c.Repository = repository.NewRepositoryService(&fakeRepo{merged: map[string]bool{"https://github.com/o/r/pull/1": true}})

	c.reconcileMerged(context.Background())
	if len(ft.transitions) != 1 || ft.transitions[0] != "P-1:Done" {
		t.Fatalf("transitions = %v, want [P-1:Done]", ft.transitions)
	}

	// Already-merged entries are not transitioned again.
	c.reconcileMerged(context.Background())
	if len(ft.transitions) != 1 {
		t.Errorf("second reconcile re-transitioned: %v", ft.transitions)
	}
}

func TestDiscardLocalChanges_KeepsAgentState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	write("main.go", "package main\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")

	// A clean tree except for agent state is left alone.
	write(".ai-intern/journal.json", "{}")
	if err := discardLocalChanges(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	write("main.go", "package main\n// partial edit\n")
	write("internal/new.go", "package internal\n")
	if err := discardLocalChanges(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(data) != "package main\n" {
		t.Errorf("tracked edit not reverted: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/new.go")); !os.IsNotExist(err) {
		t.Error("untracked file not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ai-intern/journal.json")); err != nil {
		t.Error("agent state directory must be preserved")
	}
}
