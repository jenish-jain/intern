package ticketing

import (
	"context"
	"errors"
	"testing"
)

type baseOnly struct{}

func (baseOnly) HealthCheck(context.Context) error                            { return nil }
func (baseOnly) GetTickets(context.Context, string, string) ([]Ticket, error) { return nil, nil }
func (baseOnly) UpdateTicketStatus(context.Context, string, string, map[string]string) error {
	return nil
}

func TestService_WorkflowUnsupported(t *testing.T) {
	s := NewService(baseOnly{})
	if err := s.AddComment(context.Background(), "K", "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("AddComment err = %v, want ErrUnsupported", err)
	}
	if _, err := s.CreateTicket(context.Background(), NewTicket{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("CreateTicket err = %v, want ErrUnsupported", err)
	}
}
