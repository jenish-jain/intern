package ticketing

import "context"

type Client interface {
	HealthCheck(ctx context.Context) error
	GetTickets(ctx context.Context, assignee, project string) ([]Ticket, error)
	UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error
}
