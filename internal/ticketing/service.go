package ticketing

import (
	"context"
)

type TicketingService struct {
	Client Client
}

func NewTicketingService(client Client) *TicketingService {
	return &TicketingService{Client: client}
}

func (t *TicketingService) GetTickets(ctx context.Context, assignee, project string) ([]Ticket, error) {
	return t.Client.GetTickets(ctx, assignee, project)
}

func (t *TicketingService) UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error {
	return t.Client.UpdateTicketStatus(ctx, ticketKey, status, transitions)
}
