package orchestrator

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"intern/internal/config"
	"intern/internal/journal"
	"intern/internal/runner"
)

// threadNotifier records channel and conversation notifications.
type threadNotifier struct {
	mu      sync.Mutex
	channel []string
	threads map[string][]string
}

func (n *threadNotifier) Notify(_ context.Context, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channel = append(n.channel, text)
	return nil
}

func (n *threadNotifier) NotifyConversation(_ context.Context, convID, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.threads == nil {
		n.threads = map[string][]string{}
	}
	n.threads[convID] = append(n.threads[convID], text)
	return nil
}

func TestCheckEpics_AnnouncesAndRunsArchitectReview(t *testing.T) {
	rr := &roleRunner{steps: map[runner.Role][]func(runner.Job) runner.Result{
		runner.RoleArchitect: {
			architectStep(runner.StatusDone, "Epic SETUP is on track.", nil),
			architectStep(runner.StatusDone, "Epic NOTES matches the architecture; no follow-ups.", nil),
		},
	}}
	c, repo, _ := newFlowFixture(t, &config.Config{AIRunner: "claude-code", EpicReview: true}, rr)
	n := &threadNotifier{}
	c.Notifier = n
	ctx := context.Background()

	// The approved backlog is on master.
	writeFile(t, repo.dir, ".intern/backlog.yaml", planBacklog)
	if err := exec.Command("sh", "-c", "cd "+repo.dir+" && git add . && git commit -q -m plan").Run(); err != nil {
		t.Fatal(err)
	}
	c.State.SetBacklogKey("SETUP-1", "P-101")
	c.State.SetBacklogKey("NOTES-1", "P-102")
	c.State.SetProjectConversation("conv-C1-1.2")
	c.State.SaveConversation(Conversation{ID: "conv-C1-1.2", Stage: ConvApproved, Round: 1, Branch: "intern/plan-x-r1"})

	c.Journal = journal.Load(t.TempDir())
	for i, k := range []string{"P-101", "P-102"} {
		if err := c.Journal.Append(journal.Entry{TicketKey: k, PRURL: "https://github.com/o/r/pull/" + string(rune('1'+i)), Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// Only SETUP merged: nothing is announced for NOTES yet.
	_, _ = c.Journal.Reconcile(ctx, func(_ context.Context, url string) (bool, error) { return strings.HasSuffix(url, "/1"), nil })
	c.checkEpics(ctx)
	if len(n.channel) != 1 || !strings.Contains(n.channel[0], "Epic SETUP (Skeleton) is complete: P-101") {
		t.Fatalf("channel = %v", n.channel)
	}

	_, _ = c.Journal.Reconcile(ctx, func(context.Context, string) (bool, error) { return true, nil })
	c.checkEpics(ctx)
	c.checkEpics(ctx) // idempotent
	if len(n.channel) != 2 || !strings.Contains(n.channel[1], "Epic NOTES") {
		t.Fatalf("channel = %v", n.channel)
	}
	thread := n.threads["conv-C1-1.2"]
	if len(thread) != 2 || !strings.Contains(thread[1], "Epic NOTES matches the architecture") {
		t.Errorf("thread posts = %v", thread)
	}
	if rr.count(runner.RoleArchitect) != 2 {
		t.Errorf("architect should review each completed epic once, ran %d times", rr.count(runner.RoleArchitect))
	}
	last := rr.jobs[len(rr.jobs)-1]
	if !strings.Contains(last.Description, "Epic NOTES (Notes) is complete") || !strings.Contains(last.Description, "ADRs") {
		t.Errorf("architect message = %q", last.Description)
	}
}
