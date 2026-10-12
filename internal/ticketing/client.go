package ticketing

import (
	"context"
	"errors"
)

type Client interface {
	HealthCheck(ctx context.Context) error
	GetTickets(ctx context.Context, assignee, project string) ([]Ticket, error)
	UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error
}

// Workflow is implemented by backends that support the richer operations
// the multi-role workflow needs: reading a single ticket, commenting
// (escalations, failure reasons), and creating linked tickets (backlog
// import). It is optional; callers go through Service, which returns
// ErrUnsupported when the configured backend doesn't implement it.
type Workflow interface {
	GetTicket(ctx context.Context, key string) (Ticket, error)
	AddComment(ctx context.Context, key, body string) error
	CreateTicket(ctx context.Context, t NewTicket) (string, error)
	// LinkBlocks records that blockerKey blocks blockedKey.
	LinkBlocks(ctx context.Context, blockerKey, blockedKey string) error
	// FindUserID resolves a username, email or account ID to the
	// backend's user ID.
	FindUserID(ctx context.Context, query string) (string, error)
}

// ErrUnsupported is returned by Service when the backend lacks an
// optional capability.
var ErrUnsupported = errors.New("operation not supported by ticketing backend")
