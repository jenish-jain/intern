// Package runner defines the contract between the orchestrator and the
// engine that does a role's work on a ticket. The orchestrator owns
// everything with side effects outside the workspace (git push, PRs,
// ticket transitions); a Runner only reads and edits files in Job.Workdir
// and reports what it did.
package runner

import (
	"context"
	"time"
)

// Role is the persona a job runs as.
type Role string

const (
	RoleArchitect Role = "architect"
	RoleDeveloper Role = "developer"
	RoleReviewer  Role = "reviewer"
	RoleTester    Role = "tester"
)

// Budget bounds a single job. Zero values mean "no limit from the job";
// runners may still apply their own defaults.
type Budget struct {
	MaxCostUSD float64
	Timeout    time.Duration
}

// Job is one unit of work for a role on a ticket.
type Job struct {
	Role        Role
	TicketKey   string
	Summary     string
	Description string
	// Workdir is the repository checkout the runner works in, already on
	// the ticket's branch.
	Workdir string
	// Feedback carries extra input for a follow-up run: review comments,
	// failing verify output, or a human's answer to a question.
	Feedback string
	// SessionID resumes a previous session of the same role when the
	// runner supports it (empty starts fresh).
	SessionID string
	Budget    Budget
}

// Status is the outcome a runner reports for a job.
type Status string

const (
	// StatusDone means the runner finished the work it was asked to do.
	StatusDone Status = "done"
	// StatusNeedsHuman means the runner could not proceed without an
	// answer; Result.Questions says what it needs.
	StatusNeedsHuman Status = "needs_human"
	// StatusFailed means the runner tried and could not complete the work.
	StatusFailed Status = "failed"
)

// Usage is the model usage a job incurred.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// Add returns the sum of two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + o.InputTokens,
		OutputTokens: u.OutputTokens + o.OutputTokens,
		CostUSD:      u.CostUSD + o.CostUSD,
	}
}

// Result is what a runner reports after a job.
type Result struct {
	Status  Status
	Summary string
	// Notes are judgment calls a human should confirm, surfaced in the PR.
	Notes []string
	// Questions are what the runner needs answered when Status is
	// StatusNeedsHuman.
	Questions []string
	Usage     Usage
	SessionID string
	Turns     int

	// Review is set by reviewer jobs.
	Review *Review
	// Test is set by tester jobs.
	Test *TestReport
}

// Review is a reviewer's verdict on a change.
type Review struct {
	Approve  bool
	Comments []ReviewComment
}

// ReviewComment is a comment on a specific line of a changed file.
type ReviewComment struct {
	Path string
	Line int
	Body string
}

// TestReport is a tester's account of acceptance testing.
type TestReport struct {
	// Passed is true when the change meets the ticket's acceptance
	// criteria under the tests the tester ran.
	Passed bool
	// Failures describe each unmet criterion or failing test.
	Failures []string
}

// Runner performs a job in its workdir.
type Runner interface {
	Run(ctx context.Context, job Job) (Result, error)
}
