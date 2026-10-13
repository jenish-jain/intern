package orchestrator

import (
	"context"
	"strconv"
	"strings"
	"sync"

	logger "github.com/jenish-jain/logger"
)

// Job kinds. Developers build new tickets; flow jobs review, fix and test
// open PRs. Each kind has its own MAX_CONCURRENT_TICKETS slots, so reviews
// keep moving while developers are busy and vice versa.
const (
	jobDeveloper = "developer"
	jobFlow      = "flow"
)

// scheduler tracks which tickets have a job running. A ticket has at most
// one job at a time. Jobs run in their own goroutines and worktrees; the
// poll loop starts them and moves on instead of waiting.
type scheduler struct {
	mu      sync.Mutex
	running map[string]string // ticket key -> job kind
	counts  map[string]int    // job kind -> running jobs
	wg      sync.WaitGroup    // every running job, for shutdown
}

// tryStart claims a slot of kind for key, unless key already has a job or
// kind has limit jobs running.
func (s *scheduler) tryStart(key, kind string, limit int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running, s.counts = map[string]string{}, map[string]int{}
	}
	if _, busy := s.running[key]; busy || s.counts[kind] >= limit {
		return false
	}
	s.running[key] = kind
	s.counts[kind]++
	s.wg.Add(1)
	return true
}

func (s *scheduler) done(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind, ok := s.running[key]; ok {
		delete(s.running, key)
		s.counts[kind]--
		s.wg.Done()
	}
}

func (s *scheduler) isRunning(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[key]
	return ok
}

// keys returns the tickets with a job of kind running.
func (s *scheduler) keys(kind string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, v := range s.running {
		if v == kind {
			out = append(out, k)
		}
	}
	return out
}

// wait blocks until every running job has finished.
func (s *scheduler) wait() { s.wg.Wait() }

// workers is how many jobs of each kind may run at once.
func (c *Coordinator) workers() int {
	if c.Cfg.MaxConcurrentTickets > 0 {
		return c.Cfg.MaxConcurrentTickets
	}
	return 1
}

// spawn runs job for key in its own goroutine if a slot of kind is free,
// and reports whether it started. The job is added to wg as well, so a
// caller can wait for the jobs it started.
func (c *Coordinator) spawn(key, kind string, wg *sync.WaitGroup, job func()) bool {
	if !c.sched.tryStart(key, kind, c.workers()) {
		return false
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer c.sched.done(key)
		job()
	}()
	return true
}

// runsModel reports whether a flow's next step runs a model job (review,
// fix or test) and so needs a slot. The other stages only check GitHub
// and run inline, so a PR waiting on a person never holds up a review.
func runsModel(s Stage) bool {
	return s == StageReview || s == StageChangesRequested || s == StageTest
}

// startFlows moves every open flow a step: model steps start in their own
// goroutines while slots allow, the rest run inline. It returns without
// waiting for the jobs it started; the returned group waits for them.
func (c *Coordinator) startFlows(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, f := range c.State.ListFlows() {
		if ctx.Err() != nil {
			break
		}
		f := f
		if c.sched.isRunning(f.TicketKey) {
			continue
		}
		if !runsModel(f.Stage) {
			if err := c.advanceFlow(ctx, &f); err != nil {
				logger.Error("Flow step failed", "ticket", f.TicketKey, "stage", f.Stage, "error", err)
			}
			continue
		}
		c.spawn(f.TicketKey, jobFlow, &wg, func() {
			if err := c.advanceFlow(ctx, &f); err != nil {
				logger.Error("Flow step failed", "ticket", f.TicketKey, "stage", f.Stage, "error", err)
			}
		})
	}
	return &wg
}

// advanceFlows moves every open flow one step and waits for the steps it
// started.
func (c *Coordinator) advanceFlows(ctx context.Context) {
	c.startFlows(ctx).Wait()
}

// mergeBlocker returns the earliest ticket before key in plan order that
// is still open as a PR or being built, or "" when there is none. PRs merge
// in plan order even when a later one is reviewed and tested first.
func (c *Coordinator) mergeBlocker(key string) string {
	open := c.sched.keys(jobDeveloper)
	for _, f := range c.State.ListFlows() {
		open = append(open, f.TicketKey)
	}
	best := ""
	for _, k := range open {
		if k != key && ticketBefore(k, key) && (best == "" || ticketBefore(k, best)) {
			best = k
		}
	}
	return best
}

// ticketBefore orders ticket keys by plan order: tickets are created in
// backlog order, so within a project a lower number comes first.
func ticketBefore(a, b string) bool {
	pa, na, oka := splitKey(a)
	pb, nb, okb := splitKey(b)
	if oka && okb && pa == pb {
		return na < nb
	}
	return a < b
}

func splitKey(k string) (string, int, bool) {
	i := strings.LastIndexByte(k, '-')
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(k[i+1:])
	return k[:i], n, err == nil
}
