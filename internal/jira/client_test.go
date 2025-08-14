package jira

import (
	"context"
	"errors"
	"testing"

	"ai-intern-agent/internal/jira/mocks"

	"go.uber.org/mock/gomock"
)

func TestNewClient(t *testing.T) {
	c, err := NewClient("http://jira", "user", "token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c == nil {
		t.Fatal("expected client, got nil")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	mock.EXPECT().HealthCheck(gomock.Any()).Return(nil)
	if err := mock.HealthCheck(context.Background()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	mock.EXPECT().HealthCheck(gomock.Any()).Return(errors.New("fail"))
	if err := mock.HealthCheck(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestRaw(t *testing.T) {
	c, _ := NewClient("http://jira", "user", "token")
	if c.Raw() == nil {
		t.Error("expected non-nil Raw client")
	}
}
