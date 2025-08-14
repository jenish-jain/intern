package jira

import (
	"context"
	"testing"

	"ai-intern-agent/internal/jira/mocks"

	"go.uber.org/mock/gomock"
)

func TestUpdateTicketStatus_NotImplemented(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	err := UpdateTicketStatus(context.Background(), mock, "TICKET-1", "Done")
	if err == nil || err.Error() == "" {
		t.Error("expected not implemented error")
	}
}
