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

func (t *Service) workflow() (Workflow, error) {
	w, ok := t.Client.(Workflow)
	if !ok {
		return nil, ErrUnsupported
	}
	return w, nil
}

func (t *Service) GetTicket(ctx context.Context, key string) (Ticket, error) {
	w, err := t.workflow()
	if err != nil {
		return Ticket{}, err
	}
	return w.GetTicket(ctx, key)
}

func (t *Service) AddComment(ctx context.Context, key, body string) error {
	w, err := t.workflow()
	if err != nil {
		return err
	}
	return w.AddComment(ctx, key, body)
}

func (t *Service) CreateTicket(ctx context.Context, nt NewTicket) (string, error) {
	w, err := t.workflow()
	if err != nil {
		return "", err
	}
	return w.CreateTicket(ctx, nt)
}

func (t *Service) LinkBlocks(ctx context.Context, blockerKey, blockedKey string) error {
	w, err := t.workflow()
	if err != nil {
		return err
	}
	return w.LinkBlocks(ctx, blockerKey, blockedKey)
}
