package orchestrator

import (
	"context"
	"path"
	"regexp"
	"strconv"
	"strings"

	"intern/internal/config"
	"intern/internal/runner"
)

// ticketMeta is what model routing needs about a ticket beyond its text.
type ticketMeta struct {
	Priority string
	Attempts int // failed attempts before this run
}

// rememberTicket records a ticket's priority and attempts for routing.
func (c *Coordinator) rememberTicket(key, priority string, attempts int) {
	c.ticketMeta.Store(key, ticketMeta{Priority: priority, Attempts: attempts})
}

func (c *Coordinator) metaOf(key string) ticketMeta {
	if v, ok := c.ticketMeta.Load(key); ok {
		return v.(ticketMeta)
	}
	return ticketMeta{}
}

// sizeLine finds the size the architect gave a ticket; the backlog writes
// it into the description as "Epic: ... | Size: M".
var sizeLine = regexp.MustCompile(`\bSize: ([SML])\b`)

// sizeTier is the tier for the size the architect planned: 0 (S) to 2
// (L), and M when the ticket wasn't sized.
func sizeTier(description string) int {
	if m := sizeLine.FindStringSubmatch(description); m != nil {
		return strings.Index("SML", m[1])
	}
	return 1
}

// bumpTier raises a tier by one for High or Highest priority and by
// escalations for work that has already struggled, capped at L.
func bumpTier(tier int, priority string, escalations int) int {
	switch strings.ToLower(priority) {
	case "highest", "high", "critical", "blocker":
		tier++
	}
	tier += escalations
	if tier > 2 {
		tier = 2
	}
	return tier
}

// ticketTier is a ticket's tier before any code exists: its planned size,
// bumped by priority and escalations.
func ticketTier(description, priority string, escalations int) int {
	return bumpTier(sizeTier(description), priority, escalations)
}

// Change-size thresholds for diffTier, in changed lines excluding
// lockfiles and binary files.
const (
	smallChangeLines = 120
	smallChangeFiles = 5
	largeChangeLines = 400
)

// lockfiles change mechanically with dependencies; they say nothing about
// how hard a change is to review or test.
var lockfiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "go.sum": true,
	"Cargo.lock": true, "poetry.lock": true, "Gemfile.lock": true, "composer.lock": true, "uv.lock": true,
}

// diffTier measures the change on a PR branch against the base: 0 for a
// small change (at most 120 lines in at most 5 files), 2 for a large one
// (over 400 lines), 1 otherwise. ok is false when it can't be measured.
func diffTier(ctx context.Context, dir, base string) (tier int, ok bool) {
	out, err := runCommandCapture(ctx, dir, "git", "diff", "--numstat", base+"...HEAD")
	if err != nil {
		return 0, false
	}
	lines, files := 0, 0
	for _, row := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(row)
		if len(f) < 3 || lockfiles[path.Base(f[2])] {
			continue
		}
		add, errA := strconv.Atoi(f[0])
		del, errD := strconv.Atoi(f[1])
		if errA != nil || errD != nil {
			continue // binary file
		}
		lines += add + del
		files++
	}
	switch {
	case lines <= smallChangeLines && files <= smallChangeFiles:
		return 0, true
	case lines > largeChangeLines:
		return 2, true
	}
	return 1, true
}

// modelForChange picks the reviewer's or tester's model from the change
// actually made, not the size planned: a simple implementation is checked
// by a cheap model even if the ticket looked bigger. Priority still bumps
// it. Falls back to the ticket's tier when the diff can't be measured.
func (c *Coordinator) modelForChange(ctx context.Context, role runner.Role, key, description, dir string, escalations int) string {
	if c.Cfg.ClaudeModel != "" {
		return ""
	}
	tier, ok := diffTier(ctx, dir, c.baseBranch())
	if !ok {
		tier = sizeTier(description)
	}
	return c.routedModel(role, bumpTier(tier, c.metaOf(key).Priority, escalations))
}

// modelFor picks the model for a job. CLAUDE_MODEL, when set, applies to
// every job; otherwise MODEL_ROUTING maps the role and the ticket's tier
// to a model.
func (c *Coordinator) modelFor(role runner.Role, key, description string, escalations int) string {
	if c.Cfg.ClaudeModel != "" {
		return ""
	}
	return c.routedModel(role, ticketTier(description, c.metaOf(key).Priority, escalations))
}

// routedModel looks up MODEL_ROUTING for a role and tier.
func (c *Coordinator) routedModel(role runner.Role, tier int) string {
	routing := c.Cfg.ModelRouting
	if routing == nil {
		routing = config.DefaultModelRouting()
	}
	models, ok := routing[string(role)]
	if !ok {
		return ""
	}
	return models[tier]
}
