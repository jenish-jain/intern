package agent_test

import (
	"intern/internal/ai/agent"
	"testing"
)

func TestSanitizeResponse(t *testing.T) {

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Plain JSON array",
			input: `[
				{"path": "file1.go", "operation": "add"},
				{"path": "file2.go", "operation": "modify"}
			]`,
			expected: `[
				{"path": "file1.go", "operation": "add"},
				{"path": "file2.go", "operation": "modify"}
			]`,
		},
		{
			name:  "JSON array with code fences",
			input: "```json\n[\n  {\"path\": \"file1.go\", \"operation\": \"add\"},\n  {\"path\": \"file2.go\", \"operation\": \"modify\"}\n]\n```",
			expected: `[
  {"path": "file1.go", "operation": "add"},
  {"path": "file2.go", "operation": "modify"}
]`,
		},
		{
			name:  "JSON array with extra text",
			input: "Here is the response:\n```json\n[\n  {\"path\": \"file1.go\", \"operation\": \"add\"},\n  {\"path\": \"file2.go\", \"operation\": \"modify\"}\n]\n```",
			expected: `[
  {"path": "file1.go", "operation": "add"},
  {"path": "file2.go", "operation": "modify"}
]`,
		},
		{
			name:     "No JSON array",
			input:    "No valid JSON here.",
			expected: "No valid JSON here.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.SanitizeResponse(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeResponse() = %q, want %q", result, tt.expected)
			}
		})
	}
}
