package agent

import "context"

type Agent interface {
	PlanChanges(ctx context.Context, ticketKey, ticketSummary, ticketDescription, repoContext string) ([]CodeChange, error)
}
