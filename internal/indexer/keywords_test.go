package indexer

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string // at least these keywords should be present
	}{
		{
			name:  "simple text with technical terms",
			input: "Fix authentication bug in login system",
			expected: []string{
				"authentication",
				"login",
				"system",
			},
		},
		{
			name:  "text with file path",
			input: "Update internal/auth/login.go to handle invalid passwords",
			expected: []string{
				"internal/auth/login.go",
				"internal",
				"auth",
				"login",
				"handle",
				"invalid",
				"passwords",
			},
		},
		{
			name:  "text with camelCase identifiers",
			input: "Fix getUserData function in AuthService",
			expected: []string{
				"getuserdata",
				"get",
				"user",
				"data",
				"authservice",
				"service",
				"function",
			},
		},
		{
			name:  "text with snake_case identifiers",
			input: "Update user_service and get_auth_token functions",
			expected: []string{
				"user_service",
				"user",
				"service",
				"get_auth_token",
				"token",
				"functions",
			},
		},
		{
			name:  "mixed case with stop words",
			input: "This is a bug in the authentication system that needs to be fixed",
			expected: []string{
				"bug",
				"authentication",
				"system",
				"needs",
				"fixed",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "only stop words",
			input: "a an and the is it",
			expected: []string{
				// Should be empty or minimal since all are stop words
			},
		},
		{
			name:  "real ticket example",
			input: "PROJ-123: Fix nil pointer dereference in internal/orchestrator/coordinator.go when processing tickets with empty descriptions",
			expected: []string{
				"internal/orchestrator/coordinator.go",
				"orchestrator",
				"coordinator",
				"nil",
				"pointer",
				"processing",
				"tickets",
				"empty",
				"descriptions",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractKeywords(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			// Convert to map for easier checking
			resultMap := make(map[string]bool)
			for _, kw := range result {
				resultMap[kw] = true
			}

			// Check that all expected keywords are present
			for _, expected := range tt.expected {
				assert.True(t, resultMap[expected],
					"Expected keyword '%s' not found in result: %v", expected, result)
			}
		})
	}
}

func TestExtractFilePaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single file path",
			input:    "Fix bug in internal/auth/login.go",
			expected: []string{"internal/auth/login.go"},
		},
		{
			name:     "multiple file paths",
			input:    "Update internal/auth/service.go and internal/auth/handler.go",
			expected: []string{"internal/auth/service.go", "internal/auth/handler.go"},
		},
		{
			name:     "nested path",
			input:    "Change cmd/agent/main.go configuration",
			expected: []string{"cmd/agent/main.go"},
		},
		{
			name:     "no file paths",
			input:    "Fix authentication bug",
			expected: []string{},
		},
		{
			name:     "path with hyphens and underscores",
			input:    "Update src/my-component/user_service.ts",
			expected: []string{"src/my-component/user_service.ts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFilePaths(tt.input)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestExtractIdentifiers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string // identifiers that should be present
	}{
		{
			name:  "camelCase identifier",
			input: "Fix getUserData function",
			contains: []string{
				"getuserdata",
				"get",
				"user",
				"data",
			},
		},
		{
			name:  "PascalCase identifier",
			input: "Update AuthService class",
			contains: []string{
				"authservice",
				"auth",
				"service",
			},
		},
		{
			name:  "snake_case identifier",
			input: "Modify user_service implementation",
			contains: []string{
				"user_service",
				"user",
				"service",
			},
		},
		{
			name:  "mixed identifiers",
			input: "Call getUserToken from auth_service",
			contains: []string{
				"getusertoken",
				"user",
				"token",
				"auth_service",
				"auth",
				"service",
			},
		},
		{
			name:     "no identifiers",
			input:    "Fix the bug",
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIdentifiers(tt.input)

			resultMap := make(map[string]bool)
			for _, id := range result {
				resultMap[id] = true
			}

			for _, expected := range tt.contains {
				assert.True(t, resultMap[expected],
					"Expected identifier '%s' not found in: %v", expected, result)
			}
		})
	}
}

func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "getUserData",
			expected: []string{"get", "user", "data"},
		},
		{
			input:    "AuthService",
			expected: []string{"auth", "service"},
		},
		{
			input:    "HTTPSConnection",
			expected: []string{"h", "t", "t", "p", "s", "connection"},
		},
		{
			input:    "simple",
			expected: []string{"simple"},
		},
		{
			input:    "a",
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitCamelCase(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeKeyword(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Login.go",
			expected: "login",
		},
		{
			input:    "UserService",
			expected: "userservice",
		},
		{
			input:    "auth.yaml",
			expected: "auth",
		},
		{
			input:    "\"authentication\"",
			expected: "authentication",
		},
		{
			input:    "service,",
			expected: "service",
		},
		{
			input:    "config.json",
			expected: "config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeKeyword(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractKeywords_StopWordsFiltered(t *testing.T) {
	input := "This is a test with the and or but for"
	result := ExtractKeywords(input)

	// Convert to map for checking
	resultMap := make(map[string]bool)
	for _, kw := range result {
		resultMap[kw] = true
	}

	// Stop words should not be present
	stopWordsToCheck := []string{"this", "is", "a", "the", "and", "or", "but", "for", "with"}
	for _, sw := range stopWordsToCheck {
		assert.False(t, resultMap[sw],
			"Stop word '%s' should be filtered out but was found in: %v", sw, result)
	}

	// "test" should be present (not a stop word)
	assert.True(t, resultMap["test"], "'test' should be present in result")
}

func TestExtractKeywords_MinimumLength(t *testing.T) {
	// Words with 2 or fewer characters should be filtered (except in file paths)
	input := "Fix a bug in db or ui"
	result := ExtractKeywords(input)

	resultMap := make(map[string]bool)
	for _, kw := range result {
		resultMap[kw] = true
	}

	// Single and two-letter words should be filtered
	assert.False(t, resultMap["a"], "Single letter 'a' should be filtered")
	assert.False(t, resultMap["or"], "Two letter 'or' should be filtered")
	assert.False(t, resultMap["in"], "Two letter 'in' should be filtered")

	// Three-letter words should be kept
	assert.True(t, resultMap["fix"], "Three letter 'fix' should be kept")
	assert.True(t, resultMap["bug"], "Three letter 'bug' should be kept")
}

// Benchmark tests
func BenchmarkExtractKeywords(b *testing.B) {
	text := "PROJ-123: Fix authentication bug in internal/orchestrator/coordinator.go when processing tickets with empty descriptions. The getUserData function throws nil pointer exception."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractKeywords(text)
	}
}

func BenchmarkExtractFilePaths(b *testing.B) {
	text := "Update internal/auth/service.go and internal/repository/github.go and cmd/agent/main.go"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractFilePaths(text)
	}
}

// Helper function to sort keywords for easier comparison in tests
func sortedKeywords(keywords []string) []string {
	result := make([]string, len(keywords))
	copy(result, keywords)
	sort.Strings(result)
	return result
}
