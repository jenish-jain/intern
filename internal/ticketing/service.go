package ticketing

import (
	"context"
)

type Service struct {
	Client Client
}

func NewService(client Client) *Service {
	return &Service{Client: client}
}

func (t *Service) GetTickets(ctx context.Context, assignee, project string) ([]Ticket, error) {
	return t.Client.GetTickets(ctx, assignee, project)
}

func (t *Service) UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error {
	return t.Client.UpdateTicketStatus(ctx, ticketKey, status, transitions)
}
