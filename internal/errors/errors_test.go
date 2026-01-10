package errors

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAgentError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *AgentError
		expected string
	}{
		{
			name: "error without cause",
			err: &AgentError{
				Code:     ErrCodeRepoClone,
				Category: CategoryRepository,
				Message:  "failed to clone",
			},
			expected: "[REPOSITORY:REP001] failed to clone",
		},
		{
			name: "error with cause",
			err: &AgentError{
				Code:     ErrCodeRepoClone,
				Category: CategoryRepository,
				Message:  "failed to clone",
				Cause:    errors.New("network error"),
			},
			expected: "[REPOSITORY:REP001] failed to clone: network error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAgentError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(ErrCodeAIPlan, CategoryAI, SeverityHigh, "AI failed", cause)

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is() should work with wrapped errors")
	}
}

func TestAgentError_WithContext(t *testing.T) {
	err := New(ErrCodeRepoClone, CategoryRepository, SeverityCritical, "clone failed").
		WithContext("repo_url", "https://github.com/test/repo").
		WithContext("branch", "main")

	if err.Context["repo_url"] != "https://github.com/test/repo" {
		t.Error("Context not set correctly")
	}
	if err.Context["branch"] != "main" {
		t.Error("Context not set correctly")
	}
}

func TestAgentError_WithRetryable(t *testing.T) {
	err := New(ErrCodeAIPlan, CategoryAI, SeverityHigh, "AI failed").
		WithRetryable(true)

	if !err.IsRetryable() {
		t.Error("Expected error to be retryable")
	}

	err.WithRetryable(false)
	if err.IsRetryable() {
		t.Error("Expected error to not be retryable")
	}
}

func TestAgentError_LogFields(t *testing.T) {
	err := Wrap(ErrCodeStateLoad, CategoryState, SeverityCritical,
		"failed to load state", errors.New("file not found")).
		WithContext("file_path", "/tmp/state.json").
		WithRetryable(false)

	fields := err.LogFields()

	// Check required fields
	if fields["error_code"] != string(ErrCodeStateLoad) {
		t.Errorf("error_code = %v, want %v", fields["error_code"], ErrCodeStateLoad)
	}
	if fields["error_category"] != string(CategoryState) {
		t.Errorf("error_category = %v, want %v", fields["error_category"], CategoryState)
	}
	if fields["error_severity"] != string(SeverityCritical) {
		t.Errorf("error_severity = %v, want %v", fields["error_severity"], SeverityCritical)
	}
	if fields["retryable"] != false {
		t.Error("retryable should be false")
	}

	// Check context fields
	if fields["file_path"] != "/tmp/state.json" {
		t.Error("Context fields not included in log fields")
	}

	// Check underlying error
	if !strings.Contains(fields["underlying_error"].(string), "file not found") {
		t.Error("Underlying error not included in log fields")
	}

	// Check location
	if _, ok := fields["error_location"]; !ok {
		t.Error("error_location should be present")
	}
}

func TestNew(t *testing.T) {
	err := New(ErrCodeConfigMissing, CategoryConfig, SeverityCritical, "config missing")

	if err.Code != ErrCodeConfigMissing {
		t.Error("Code not set correctly")
	}
	if err.Category != CategoryConfig {
		t.Error("Category not set correctly")
	}
	if err.Severity != SeverityCritical {
		t.Error("Severity not set correctly")
	}
	if err.Message != "config missing" {
		t.Error("Message not set correctly")
	}
	if err.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
	if err.StackFile == "" {
		t.Error("StackFile should be set")
	}
	if err.StackLine == 0 {
		t.Error("StackLine should be set")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(ErrCodeAIPlan, CategoryAI, SeverityHigh, "AI failed", cause)

	if err.Cause != cause {
		t.Error("Cause not set correctly")
	}
	if err.Code != ErrCodeAIPlan {
		t.Error("Code not set correctly")
	}
}

// Test helper functions

func TestNewRepoCloneError(t *testing.T) {
	err := NewRepoCloneError(errors.New("network error"), "https://github.com/test/repo")

	if err.Code != ErrCodeRepoClone {
		t.Error("Wrong error code")
	}
	if err.Category != CategoryRepository {
		t.Error("Wrong category")
	}
	if !err.IsRetryable() {
		t.Error("Clone errors should be retryable")
	}
	if err.Context["repo_url"] != "https://github.com/test/repo" {
		t.Error("repo_url not set in context")
	}
}

func TestNewRepoBranchError(t *testing.T) {
	err := NewRepoBranchError(errors.New("branch exists"), "feature/test", "create")

	if err.Code != ErrCodeRepoBranch {
		t.Error("Wrong error code")
	}
	if err.Context["branch"] != "feature/test" {
		t.Error("branch not set in context")
	}
	if err.Context["operation"] != "create" {
		t.Error("operation not set in context")
	}
	if err.IsRetryable() {
		t.Error("Branch errors should not be retryable by default")
	}
}

func TestNewStateLoadError(t *testing.T) {
	err := NewStateLoadError(errors.New("file corrupted"), "/tmp/state.json")

	if err.Code != ErrCodeStateLoad {
		t.Error("Wrong error code")
	}
	if err.Category != CategoryState {
		t.Error("Wrong category")
	}
	if err.Severity != SeverityCritical {
		t.Error("State load should be critical")
	}
	if err.Context["file_path"] != "/tmp/state.json" {
		t.Error("file_path not set in context")
	}
}

func TestNewAIPlanError(t *testing.T) {
	err := NewAIPlanError(errors.New("timeout"), "PROJ-123")

	if err.Code != ErrCodeAIPlan {
		t.Error("Wrong error code")
	}
	if err.Category != CategoryAI {
		t.Error("Wrong category")
	}
	if !err.IsRetryable() {
		t.Error("AI plan errors should be retryable")
	}
	if err.Context["ticket_key"] != "PROJ-123" {
		t.Error("ticket_key not set in context")
	}
}

func TestNewAITimeoutError(t *testing.T) {
	duration := 30 * time.Second
	err := NewAITimeoutError("PROJ-123", duration)

	if err.Code != ErrCodeAITimeout {
		t.Error("Wrong error code")
	}
	if err.Context["timeout_duration"] != duration {
		t.Error("timeout_duration not set in context")
	}
	if !err.IsRetryable() {
		t.Error("Timeout errors should be retryable")
	}
}

func TestNewAIInvalidResponseError(t *testing.T) {
	longResponse := strings.Repeat("x", 300)
	err := NewAIInvalidResponseError(errors.New("invalid JSON"), longResponse)

	if err.Code != ErrCodeAIInvalidResp {
		t.Error("Wrong error code")
	}

	// Should truncate long responses
	preview := err.Context["response_preview"].(string)
	if len(preview) > 205 { // 200 + "..."
		t.Errorf("Response preview too long: %d chars", len(preview))
	}
	if !strings.HasSuffix(preview, "...") {
		t.Error("Long response should be truncated with ...")
	}
}

func TestNewValidationPathTraversalError(t *testing.T) {
	err := NewValidationPathTraversalError("../../etc/passwd")

	if err.Code != ErrCodeValidateTraversal {
		t.Error("Wrong error code")
	}
	if err.Severity != SeverityCritical {
		t.Error("Path traversal should be critical")
	}
	if err.IsRetryable() {
		t.Error("Path traversal errors should not be retryable")
	}
}

func TestNewValidationDisallowedDirError(t *testing.T) {
	allowedDirs := []string{"internal", "cmd", "pkg"}
	err := NewValidationDisallowedDirError("malicious/file.go", allowedDirs)

	if err.Code != ErrCodeValidateDisallowed {
		t.Error("Wrong error code")
	}
	if err.Context["path"] != "malicious/file.go" {
		t.Error("path not set in context")
	}
}

func TestNewQualityVetError(t *testing.T) {
	output := "vet: some warning"
	err := NewQualityVetError(output)

	if err.Code != ErrCodeQualityVet {
		t.Error("Wrong error code")
	}
	if err.Category != CategoryQuality {
		t.Error("Wrong category")
	}
	if !err.IsRetryable() {
		t.Error("Quality gate errors should be retryable (self-healing)")
	}
	if err.Context["vet_output"] != output {
		t.Error("vet_output not set in context")
	}
}

func TestNewConfigMissingError(t *testing.T) {
	err := NewConfigMissingError("GITHUB_TOKEN")

	if err.Code != ErrCodeConfigMissing {
		t.Error("Wrong error code")
	}
	if err.Severity != SeverityCritical {
		t.Error("Missing config should be critical")
	}
	if err.IsRetryable() {
		t.Error("Config errors should not be retryable")
	}
}

func TestNewConfigConflictError(t *testing.T) {
	fields := []string{"SELF_HEAL_ENABLED", "SELF_HEAL_ON_TESTS"}
	err := NewConfigConflictError(fields, "healing enabled but no gates configured")

	if err.Code != ErrCodeConfigConflict {
		t.Error("Wrong error code")
	}
	if err.Context["conflicting_fields"] == nil {
		t.Error("conflicting_fields not set in context")
	}
}

func TestErrorCodes_Uniqueness(t *testing.T) {
	// Ensure all error codes are unique
	codes := []ErrorCode{
		// Repository
		ErrCodeRepoClone, ErrCodeRepoSync, ErrCodeRepoBranch,
		ErrCodeRepoCommit, ErrCodeRepoPush, ErrCodeRepoPR,
		ErrCodeRepoStatus, ErrCodeRepoPath,
		// State
		ErrCodeStateLoad, ErrCodeStateSave, ErrCodeStateCorrupt,
		ErrCodeStatePermission,
		// AI
		ErrCodeAIPlan, ErrCodeAIFix, ErrCodeAITimeout,
		ErrCodeAIRateLimit, ErrCodeAIInvalidResp, ErrCodeAIAuth,
		// Ticketing
		ErrCodeTicketFetch, ErrCodeTicketUpdate, ErrCodeTicketAuth,
		// Validation
		ErrCodeValidatePath, ErrCodeValidateTraversal, ErrCodeValidateEmpty,
		ErrCodeValidateMaxFiles, ErrCodeValidateDisallowed,
		// Config
		ErrCodeConfigMissing, ErrCodeConfigInvalid, ErrCodeConfigConflict,
		// Quality
		ErrCodeQualityVet, ErrCodeQualityTest, ErrCodeQualityBuild,
		// Context
		ErrCodeContextBuild, ErrCodeContextIndex, ErrCodeContextSize,
	}

	seen := make(map[ErrorCode]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("Duplicate error code: %s", code)
		}
		seen[code] = true
	}
}

func TestErrorCategories(t *testing.T) {
	categories := []ErrorCategory{
		CategoryRepository, CategoryTicketing, CategoryAI, CategoryState,
		CategoryValidation, CategoryConfig, CategoryContext, CategoryQuality,
		CategorySelfHeal, CategoryInternal,
	}

	for _, cat := range categories {
		if cat == "" {
			t.Error("Empty category found")
		}
	}
}

func TestErrorSeverities(t *testing.T) {
	severities := []ErrorSeverity{
		SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow,
	}

	for _, sev := range severities {
		if sev == "" {
			t.Error("Empty severity found")
		}
	}
}
