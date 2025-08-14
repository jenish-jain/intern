package ai

import (
	"context"
	"testing"

	"intern/internal/ai/mocks"

	"go.uber.org/mock/gomock"
)

func TestNewClient(t *testing.T) {
	c := NewClient("api-key")
	if c == nil {
		t.Fatal("expected client, got nil")
	}
}

func TestGenerateCode_Mock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	mock.EXPECT().GenerateCode(gomock.Any(), "prompt").Return("code", nil)
	code, err := mock.GenerateCode(context.Background(), "prompt")
	if err != nil || code != "code" {
		t.Errorf("expected code, got %v, err %v", code, err)
	}
}
