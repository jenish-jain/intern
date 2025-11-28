package orchestrator

import (
	"intern/internal/ai/agent"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenish-jain/logger"
	"github.com/stretchr/testify/assert"
)

func TestValidatePlannedChanges(t *testing.T) {
	allowedDirs := []string{"internal", "cmd", "pkg"}
	maxFiles := 3

	tests := []struct {
		name     string
		changes  []agent.CodeChange
		expected int
		hasError bool
	}{
		{
			name: "valid changes",
			changes: []agent.CodeChange{
				{Path: "internal/service.go", Content: "package internal", Operation: "create"},
				{Path: "cmd/main.go", Content: "package main", Operation: "update"},
			},
			expected: 2,
			hasError: false,
		},
		{
			name: "path traversal blocked",
			changes: []agent.CodeChange{
				{Path: "../../../etc/passwd", Content: "malicious", Operation: "create"},
				{Path: "internal/service.go", Content: "package internal", Operation: "create"},
			},
			expected: 1,
			hasError: false,
		},
		{
			name: "absolute paths blocked",
			changes: []agent.CodeChange{
				{Path: "/etc/passwd", Content: "malicious", Operation: "create"},
				{Path: "internal/service.go", Content: "package internal", Operation: "create"},
			},
			expected: 1,
			hasError: false,
		},
		{
			name: "disallowed directories blocked",
			changes: []agent.CodeChange{
				{Path: "malicious/file.go", Content: "bad content", Operation: "create"},
				{Path: "internal/service.go", Content: "package internal", Operation: "create"},
			},
			expected: 1,
			hasError: false,
		},
		{
			name: "empty content blocked",
			changes: []agent.CodeChange{
				{Path: "internal/service.go", Content: "", Operation: "create"},
				{Path: "cmd/main.go", Content: "package main", Operation: "create"},
			},
			expected: 1,
			hasError: false,
		},
		{
			name: "empty path blocked",
			changes: []agent.CodeChange{
				{Path: "", Content: "content", Operation: "create"},
				{Path: "internal/service.go", Content: "package internal", Operation: "create"},
			},
			expected: 1,
			hasError: false,
		},
		{
			name: "max files limit",
			changes: []agent.CodeChange{
				{Path: "internal/service1.go", Content: "package internal", Operation: "create"},
				{Path: "internal/service2.go", Content: "package internal", Operation: "create"},
				{Path: "internal/service3.go", Content: "package internal", Operation: "create"},
				{Path: "internal/service4.go", Content: "package internal", Operation: "create"},
				{Path: "internal/service5.go", Content: "package internal", Operation: "create"},
			},
			expected: 3, // limited by maxFiles
			hasError: false,
		},
		{
			name: "all changes invalid",
			changes: []agent.CodeChange{
				{Path: "../../../etc/passwd", Content: "malicious", Operation: "create"},
				{Path: "/etc/passwd", Content: "malicious", Operation: "create"},
				{Path: "malicious/file.go", Content: "bad content", Operation: "create"},
			},
			expected: 0,
			hasError: true,
		},
		{
			name: "delete operations with empty content accepted",
			changes: []agent.CodeChange{
				{Path: "internal/service.go", Content: "", Operation: agent.OperationDelete},
				{Path: "cmd/main.go", Content: "", Operation: agent.OperationDelete},
			},
			expected: 2,
			hasError: false,
		},
		{
			name: "mixed create and delete operations",
			changes: []agent.CodeChange{
				{Path: "internal/new.go", Content: "package internal", Operation: agent.OperationCreate},
				{Path: "cmd/old.go", Content: "", Operation: agent.OperationDelete},
				{Path: "pkg/updated.go", Content: "package pkg", Operation: agent.OperationUpdate},
			},
			expected: 3,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger.Init("debug")
			result, err := validatePlannedChanges("/fake/root", tt.changes, allowedDirs, maxFiles)

			if tt.hasError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result, tt.expected)

				// Verify all returned changes are valid
				for _, change := range result {
					assert.NotEmpty(t, change.Path)
					// Content can be empty for delete operations
					if change.Operation != agent.OperationDelete {
						assert.NotEmpty(t, change.Content)
					}
					assert.False(t, strings.HasPrefix(change.Path, ".."))
					assert.False(t, filepath.IsAbs(change.Path))
				}
			}
		})
	}
}

func TestFirstSegment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"internal/service.go", "internal"},
		{"cmd/main.go", "cmd"},
		{"single.go", "single.go"},
		{"", ""},
		{"internal/nested/deep/file.go", "internal"},
	}

	for _, tt := range tests {
		result := firstSegment(tt.input)
		assert.Equal(t, tt.expected, result, "firstSegment(%q)", tt.input)
	}
}

func TestInList(t *testing.T) {
	list := []string{"internal", "cmd", "pkg"}

	tests := []struct {
		input    string
		expected bool
	}{
		{"internal", true},
		{"cmd", true},
		{"pkg", true},
		{"notallowed", false},
		{"", false},
	}

	for _, tt := range tests {
		result := inList(tt.input, list)
		assert.Equal(t, tt.expected, result, "inList(%q, %v)", tt.input, list)
	}
}
