package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"intern/internal/backlog"
	"intern/internal/gate"
	"intern/internal/runner"

	logger "github.com/jenish-jain/logger"
)

// ConversationStage is where an architect conversation is.
type ConversationStage string

const (
	// ConvShaping: the architect is asking questions or drafting.
	ConvShaping ConversationStage = "shaping"
	// ConvAwaitingApproval: a valid backlog is on the plan PR, waiting
	// for the product owner to approve it.
	ConvAwaitingApproval ConversationStage = "awaiting_approval"
	// ConvApproved: the plan merged and its tickets were created. Further
	// messages start a new planning round.
	ConvApproved ConversationStage = "approved"
)

// Conversation is an architect thread with the product owner.
type Conversation struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id,omitempty"`
	Branch    string            `json:"branch"`
	PRURL     string            `json:"pr_url,omitempty"`
	Stage     ConversationStage `json:"stage"`
	Round     int               `json:"round"`
	CostUSD   float64           `json:"cost_usd"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// architectPaths are the only paths the architect may change.
var architectPaths = []string{"docs/", "CLAUDE.md", ".intern/"}

var approvalRe = regexp.MustCompile(`(?i)^\s*(approve|approved|lgtm|ship it|looks good,? approve|yes,? approve)\s*[.!]*\s*$`)

// IsApproval reports whether a message approves the proposed backlog.
func IsApproval(msg string) bool { return approvalRe.MatchString(msg) }

var unsafeBranchChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func planBranch(convID string, round int) string {
	id := unsafeBranchChars.ReplaceAllString(convID, "-")
	if len(id) > 40 {
		id = id[len(id)-40:]
	}
	return fmt.Sprintf("intern/plan-%s-r%d", strings.Trim(id, "-"), round)
}

// Converse handles one message from the product owner in conversation
// convID and returns the reply to post. It holds the repository lock for
// the whole exchange, since the architect works in the shared clone.
func (c *Coordinator) Converse(ctx context.Context, convID, message string) (string, error) {
	c.repoMu.Lock()
	defer c.repoMu.Unlock()
	return c.converse(ctx, convID, message)
}

// converse is Converse for callers that already hold the repository lock.
func (c *Coordinator) converse(ctx context.Context, convID, message string) (string, error) {
	defer c.returnToBase(ctx)

	conv, ok := c.State.GetConversation(convID)
	if !ok {
		conv = Conversation{ID: convID, Stage: ConvShaping, Round: 1, CreatedAt: time.Now()}
		conv.Branch = planBranch(convID, conv.Round)
	}
	if conv.Stage == ConvAwaitingApproval && IsApproval(message) {
		return c.approvePlan(ctx, &conv)
	}
	if c.budgetExceeded() {
		return fmt.Sprintf("The project budget is used up ($%.2f of $%.2f), so planning is paused. Raise PROJECT_BUDGET_USD to continue.", c.State.Spend(), c.Cfg.ProjectBudgetUSD), nil
	}
	if conv.Stage == ConvApproved {
		// A message after approval is a change request: plan it on a fresh
		// branch and PR, keeping the session so the architect remembers.
		conv.Round++
		conv.Branch = planBranch(convID, conv.Round)
		conv.PRURL = ""
		conv.Stage = ConvShaping
	}
	return c.planRound(ctx, &conv, message)
}

// planRound runs the architect on the conversation's plan branch, commits
// what it wrote, updates the plan PR and builds the reply.
func (c *Coordinator) planRound(ctx context.Context, conv *Conversation, message string) (string, error) {
	repoRoot := c.RepoPaths.Root()
	if err := c.prepareRepository(ctx); err != nil {
		return "", err
	}
	if conv.PRURL != "" {
		if err := c.Repository.FetchBranch(ctx, conv.Branch); err != nil {
			logger.Warn("Could not fetch plan branch; continuing from local state", "branch", conv.Branch, "error", err)
		}
	} else if err := c.Repository.CreateBranch(ctx, conv.Branch); err != nil {
		return "", fmt.Errorf("create plan branch: %w", err)
	}
	if err := c.Repository.SwitchBranch(ctx, conv.Branch); err != nil {
		return "", fmt.Errorf("switch to plan branch: %w", err)
	}
	base, err := gate.HeadSHA(ctx, repoRoot)
	if err != nil {
		return "", err
	}

	job := runner.Job{
		Role: runner.RoleArchitect, TicketKey: conv.ID, Summary: "project plan",
		Description: message, Feedback: c.architectContext(),
		Workdir: repoRoot, BaseBranch: c.baseBranch(), SessionID: conv.SessionID,
		Budget: runner.Budget{MaxCostUSD: c.Cfg.TicketBudgetUSD},
	}
	res, err := c.Runner.Run(ctx, job)
	if err != nil && job.SessionID != "" {
		// The session may be gone (new instance, cleared cache). The docs
		// on the branch carry the plan, so start a fresh session.
		logger.Warn("Architect session resume failed; starting a fresh session", "conversation", conv.ID, "error", err)
		job.SessionID = ""
		job.Feedback = "This continues an earlier planning conversation whose chat history is unavailable. The plan so far is in the repository docs and backlog.\n\n" + job.Feedback
		res, err = c.Runner.Run(ctx, job)
	}
	conv.CostUSD += res.Usage.CostUSD
	c.recordSpend(ctx, runner.RoleArchitect, res.Usage.CostUSD)
	if res.SessionID != "" {
		conv.SessionID = res.SessionID
	}
	conv.UpdatedAt = time.Now()
	c.State.SaveConversation(*conv)
	if err != nil {
		return "", fmt.Errorf("architect: %w", err)
	}

	committed, err := c.commitPlan(ctx, conv, base, res.Summary)
	if err != nil {
		return "", err
	}

	var reply strings.Builder
	reply.WriteString(strings.TrimSpace(res.Summary))
	if len(res.Notes) > 0 {
		reply.WriteString("\n\nAssumptions to confirm:\n• " + strings.Join(res.Notes, "\n• "))
	}
	switch res.Status {
	case runner.StatusNeedsHuman:
		reply.WriteString("\n\nQuestions:")
		for i, q := range res.Questions {
			fmt.Fprintf(&reply, "\n%d. %s", i+1, q)
		}
		conv.Stage = ConvShaping
	case runner.StatusDone:
		b, found, lerr := backlog.Load(repoRoot)
		known := c.State.BacklogKeyMap()
		switch {
		case lerr != nil:
			reply.WriteString("\n\nThe backlog file couldn't be read: " + lerr.Error())
			conv.Stage = ConvShaping
		case !found:
			reply.WriteString("\n\nThere's no backlog yet. Tell me when you'd like me to break the plan into tickets.")
			conv.Stage = ConvShaping
		default:
			if verr := b.Validate(known); verr != nil {
				reply.WriteString("\n\nThe backlog has problems I need to fix before you can approve it:\n" + verr.Error() + "\n\nReply with anything (for example \"fix the backlog\") and I'll correct it.")
				conv.Stage = ConvShaping
			} else {
				reply.WriteString("\n\n*Proposed backlog*\n" + b.Summary(known))
				reply.WriteString("\n\nReply *approve* to create these tickets, or tell me what to change.")
				conv.Stage = ConvAwaitingApproval
			}
		}
	default:
		conv.Stage = ConvShaping
	}
	if conv.PRURL != "" && committed {
		reply.WriteString("\n\nPlan: " + conv.PRURL)
	}
	conv.UpdatedAt = time.Now()
	c.State.SaveConversation(*conv)
	return reply.String(), nil
}

// commitPlan gates the architect's changes to the plan paths, commits and
// pushes them, and opens or updates the plan PR. It reports whether
// anything was committed.
func (c *Coordinator) commitPlan(ctx context.Context, conv *Conversation, base, summary string) (bool, error) {
	repoRoot := c.RepoPaths.Root()
	changes, violations, err := gate.Check(ctx, repoRoot, base, gate.Policy{OnlyPaths: architectPaths})
	if err != nil {
		return false, fmt.Errorf("gate: %w", err)
	}
	if len(violations) > 0 {
		msgs := make([]string, len(violations))
		for i, v := range violations {
			msgs[i] = v.String()
		}
		return false, fmt.Errorf("the architect changed files outside docs/, CLAUDE.md and .intern/: %s", strings.Join(msgs, "; "))
	}
	if len(changes) == 0 {
		return false, nil
	}
	if err := c.commitStaged(ctx, repoRoot, fmt.Sprintf("docs(plan): round %d", conv.Round)); err != nil {
		return false, err
	}
	if err := c.Repository.Push(ctx, conv.Branch); err != nil {
		return false, fmt.Errorf("push plan: %w", err)
	}
	body := "Project plan maintained by the intern architect from the product owner's conversation.\n\n" + strings.TrimSpace(summary)
	if conv.PRURL == "" {
		url, err := c.Repository.CreatePullRequest(ctx, c.baseBranch(), conv.Branch, fmt.Sprintf("Project plan (round %d)", conv.Round), body)
		if err != nil {
			return false, fmt.Errorf("open plan PR: %w", err)
		}
		conv.PRURL = url
	} else if err := c.Repository.CommentPR(ctx, conv.PRURL, "**Plan updated**\n\n"+strings.TrimSpace(summary)); err != nil {
		logger.Warn("Failed to comment on plan PR", "pr", conv.PRURL, "error", err)
	}
	return true, nil
}

// approvePlan merges the plan PR and imports its backlog into JIRA.
func (c *Coordinator) approvePlan(ctx context.Context, conv *Conversation) (string, error) {
	if conv.PRURL != "" {
		pr, err := c.Repository.GetPR(ctx, conv.PRURL)
		if err != nil {
			return "", fmt.Errorf("read plan PR: %w", err)
		}
		if !pr.Merged {
			if err := c.Repository.MergePR(ctx, conv.PRURL, c.Cfg.MergeMethod); err != nil {
				return fmt.Sprintf("I couldn't merge the plan (%v). Merge %s yourself, then reply *approve* again.", err, conv.PRURL), nil
			}
		}
	}
	// Import from the base branch, which now carries the approved plan.
	if err := c.prepareRepository(ctx); err != nil {
		return "", err
	}
	res, err := c.ImportBacklog(ctx, c.RepoPaths.Root())
	if err != nil {
		return fmt.Sprintf("The plan is merged, but creating tickets failed: %v\n\nReply *approve* to retry; tickets already created won't be duplicated.", err), nil
	}
	conv.Stage = ConvApproved
	conv.UpdatedAt = time.Now()
	c.State.SaveConversation(*conv)
	// The approving thread is where epic milestones are reported.
	c.State.SetProjectConversation(conv.ID)

	var b strings.Builder
	switch len(res.Created) {
	case 0:
		b.WriteString("The plan is merged. Every ticket in it already existed, so nothing new was created.")
	default:
		fmt.Fprintf(&b, "The plan is merged and I created %d tickets:", len(res.Created))
		for _, t := range res.Created {
			fmt.Fprintf(&b, "\n• %s → %s", t.ID, t.Key)
		}
		b.WriteString("\n\nThe team will pick them up in dependency order. Message me here any time to change the plan.")
	}
	return b.String(), nil
}

// architectContext lists tickets that already exist, which the architect
// must not change.
func (c *Coordinator) architectContext() string {
	known := c.State.BacklogKeyMap()
	if len(known) == 0 {
		return ""
	}
	ids := make([]string, 0, len(known))
	for id := range known {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("Tickets already created (being built or done; never change or remove them, add new tickets instead):\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "- %s (%s)\n", id, known[id])
	}
	return b.String()
}

// returnToBase leaves the shared clone clean and on the base branch.
func (c *Coordinator) returnToBase(ctx context.Context) {
	if err := discardLocalChanges(ctx, c.RepoPaths.Root()); err != nil {
		logger.Warn("Could not clean agent clone", "error", err)
	}
	if err := c.Repository.SwitchBranch(ctx, c.baseBranch()); err != nil {
		logger.Warn("Failed to return to base branch", "error", err)
	}
}
