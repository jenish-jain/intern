package agent_test

import (
	"intern/internal/ai/agent"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNeedFiles(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "object form",
			input:    `{"need_files":["a.tf","b.tf"]}`,
			expected: []string{"a.tf", "b.tf"},
		},
		{
			name:     "bare array form",
			input:    `["a.tf", "b.tf"]`,
			expected: []string{"a.tf", "b.tf"},
		},
		{
			name:     "changes array is not a need_files request",
			input:    `[{"path":"a.tf","operation":"edit","edits":[]}]`,
			expected: nil,
		},
		{
			name:     "empty object",
			input:    `{}`,
			expected: nil,
		},
		{
			name:     "not json",
			input:    `not json`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, agent.ParseNeedFiles(tt.input))
		})
	}
}
