package errors

import (
	"fmt"
	"runtime"
	"time"
)

// ErrorCategory represents the category/domain of an error
type ErrorCategory string

const (
	CategoryRepository ErrorCategory = "REPOSITORY" // Git/GitHub operations
	CategoryTicketing  ErrorCategory = "TICKETING"  // JIRA operations
	CategoryAI         ErrorCategory = "AI"         // AI provider operations
	CategoryState      ErrorCategory = "STATE"      // State file operations
	CategoryValidation ErrorCategory = "VALIDATION" // Validation failures
	CategoryConfig     ErrorCategory = "CONFIG"     // Configuration errors
	CategoryContext    ErrorCategory = "CONTEXT"    // Context building errors
	CategoryQuality    ErrorCategory = "QUALITY"    // Quality gate failures
	CategorySelfHeal   ErrorCategory = "SELFHEAL"   // Self-healing errors
	CategoryInternal   ErrorCategory = "INTERNAL"   // Internal/unknown errors
)

// ErrorSeverity represents how critical an error is
type ErrorSeverity string

const (
	SeverityCritical ErrorSeverity = "CRITICAL" // Service cannot continue
	SeverityHigh     ErrorSeverity = "HIGH"     // Major functionality broken
	SeverityMedium   ErrorSeverity = "MEDIUM"   // Degraded functionality
	SeverityLow      ErrorSeverity = "LOW"      // Minor issue, workaround exists
)

// ErrorCode is a unique identifier for specific error types
type ErrorCode string

// Repository error codes
const (
	ErrCodeRepoClone       ErrorCode = "REP001" // Failed to clone repository
	ErrCodeRepoSync        ErrorCode = "REP002" // Failed to sync with remote
	ErrCodeRepoBranch      ErrorCode = "REP003" // Failed to create/switch branch
	ErrCodeRepoCommit      ErrorCode = "REP004" // Failed to commit changes
	ErrCodeRepoPush        ErrorCode = "REP005" // Failed to push changes
	ErrCodeRepoPR          ErrorCode = "REP006" // Failed to create pull request
	ErrCodeRepoStatus      ErrorCode = "REP007" // Failed to get repo status
	ErrCodeRepoPath        ErrorCode = "REP008" // Invalid repository path
)

// State error codes
const (
	ErrCodeStateLoad       ErrorCode = "STATE001" // Failed to load state file
	ErrCodeStateSave       ErrorCode = "STATE002" // Failed to save state file
	ErrCodeStateCorrupt    ErrorCode = "STATE003" // State file corrupted
	ErrCodeStatePermission ErrorCode = "STATE004" // Permission denied on state file
)

// AI error codes
const (
	ErrCodeAIPlan        ErrorCode = "AI001" // AI planning failed
	ErrCodeAIFix         ErrorCode = "AI002" // AI fix generation failed
	ErrCodeAITimeout     ErrorCode = "AI003" // AI request timed out
	ErrCodeAIRateLimit   ErrorCode = "AI004" // AI rate limit hit
	ErrCodeAIInvalidResp ErrorCode = "AI005" // AI returned invalid response
	ErrCodeAIAuth        ErrorCode = "AI006" // AI authentication failed
)

// Ticketing error codes
const (
	ErrCodeTicketFetch  ErrorCode = "TICKET001" // Failed to fetch tickets
	ErrCodeTicketUpdate ErrorCode = "TICKET002" // Failed to update ticket status
	ErrCodeTicketAuth   ErrorCode = "TICKET003" // JIRA authentication failed
)

// Validation error codes
const (
	ErrCodeValidatePath      ErrorCode = "VAL001" // Invalid file path
	ErrCodeValidateTraversal ErrorCode = "VAL002" // Path traversal detected
	ErrCodeValidateEmpty     ErrorCode = "VAL003" // Empty/invalid content
	ErrCodeValidateMaxFiles  ErrorCode = "VAL004" // Too many files
	ErrCodeValidateDisallowed ErrorCode = "VAL005" // Disallowed directory
)

// Config error codes
const (
	ErrCodeConfigMissing  ErrorCode = "CFG001" // Required config missing
	ErrCodeConfigInvalid  ErrorCode = "CFG002" // Invalid config value
	ErrCodeConfigConflict ErrorCode = "CFG003" // Conflicting config values
)

// Quality gate error codes
const (
	ErrCodeQualityVet   ErrorCode = "QA001" // go vet failed
	ErrCodeQualityTest  ErrorCode = "QA002" // go test failed
	ErrCodeQualityBuild ErrorCode = "QA003" // go build failed
)

// Context error codes
const (
	ErrCodeContextBuild ErrorCode = "CTX001" // Failed to build context
	ErrCodeContextIndex ErrorCode = "CTX002" // Failed to build/load index
	ErrCodeContextSize  ErrorCode = "CTX003" // Context too large
)

// AgentError is a structured error with rich metadata
type AgentError struct {
	Code      ErrorCode     // Unique error code
	Category  ErrorCategory // Error category/domain
	Severity  ErrorSeverity // How critical is this error
	Message   string        // Human-readable error message
	Cause     error         // Underlying error (if any)
	Timestamp time.Time     // When the error occurred
	Context   map[string]interface{} // Additional context for debugging
	StackFile string        // File where error was created
	StackLine int           // Line where error was created
	Retryable bool          // Can this operation be retried?
}

// Error implements the error interface
func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Category, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Category, e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support
func (e *AgentError) Unwrap() error {
	return e.Cause
}

// WithContext adds additional context to the error
func (e *AgentError) WithContext(key string, value interface{}) *AgentError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithRetryable marks the error as retryable or not
func (e *AgentError) WithRetryable(retryable bool) *AgentError {
	e.Retryable = retryable
	return e
}

// IsRetryable returns whether this error can be retried
func (e *AgentError) IsRetryable() bool {
	return e.Retryable
}

// LogFields returns structured log fields for this error
func (e *AgentError) LogFields() map[string]interface{} {
	fields := map[string]interface{}{
		"error_code":     string(e.Code),
		"error_category": string(e.Category),
		"error_severity": string(e.Severity),
		"error_message":  e.Message,
		"retryable":      e.Retryable,
		"timestamp":      e.Timestamp,
	}

	if e.StackFile != "" {
		fields["error_location"] = fmt.Sprintf("%s:%d", e.StackFile, e.StackLine)
	}

	if e.Cause != nil {
		fields["underlying_error"] = e.Cause.Error()
	}

	// Merge additional context
	for k, v := range e.Context {
		fields[k] = v
	}

	return fields
}

// New creates a new AgentError with the given parameters
func New(code ErrorCode, category ErrorCategory, severity ErrorSeverity, message string) *AgentError {
	_, file, line, _ := runtime.Caller(1)
	return &AgentError{
		Code:      code,
		Category:  category,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now(),
		StackFile: file,
		StackLine: line,
		Context:   make(map[string]interface{}),
	}
}

// Wrap wraps an existing error with AgentError metadata
func Wrap(code ErrorCode, category ErrorCategory, severity ErrorSeverity, message string, cause error) *AgentError {
	_, file, line, _ := runtime.Caller(1)
	return &AgentError{
		Code:      code,
		Category:  category,
		Severity:  severity,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
		StackFile: file,
		StackLine: line,
		Context:   make(map[string]interface{}),
	}
}

// Helper functions for common error patterns

// Repository errors
func NewRepoCloneError(err error, repoURL string) *AgentError {
	return Wrap(ErrCodeRepoClone, CategoryRepository, SeverityCritical,
		"failed to clone repository", err).
		WithContext("repo_url", repoURL).
		WithRetryable(true)
}

func NewRepoBranchError(err error, branchName string, operation string) *AgentError {
	return Wrap(ErrCodeRepoBranch, CategoryRepository, SeverityHigh,
		fmt.Sprintf("failed to %s branch", operation), err).
		WithContext("branch", branchName).
		WithContext("operation", operation).
		WithRetryable(false)
}

func NewRepoCommitError(err error) *AgentError {
	return Wrap(ErrCodeRepoCommit, CategoryRepository, SeverityHigh,
		"failed to commit changes", err).
		WithRetryable(true)
}

func NewRepoPushError(err error, branchName string) *AgentError {
	return Wrap(ErrCodeRepoPush, CategoryRepository, SeverityHigh,
		"failed to push changes", err).
		WithContext("branch", branchName).
		WithRetryable(true)
}

func NewRepoPRError(err error, baseBranch, headBranch string) *AgentError {
	return Wrap(ErrCodeRepoPR, CategoryRepository, SeverityHigh,
		"failed to create pull request", err).
		WithContext("base_branch", baseBranch).
		WithContext("head_branch", headBranch).
		WithRetryable(true)
}

// State errors
func NewStateLoadError(err error, filePath string) *AgentError {
	return Wrap(ErrCodeStateLoad, CategoryState, SeverityCritical,
		"failed to load state file", err).
		WithContext("file_path", filePath).
		WithRetryable(false)
}

func NewStateSaveError(err error, filePath string) *AgentError {
	return Wrap(ErrCodeStateSave, CategoryState, SeverityHigh,
		"failed to save state file", err).
		WithContext("file_path", filePath).
		WithRetryable(true)
}

func NewStateCorruptError(err error, filePath string) *AgentError {
	return Wrap(ErrCodeStateCorrupt, CategoryState, SeverityCritical,
		"state file is corrupted", err).
		WithContext("file_path", filePath).
		WithRetryable(false)
}

// AI errors
func NewAIPlanError(err error, ticketKey string) *AgentError {
	return Wrap(ErrCodeAIPlan, CategoryAI, SeverityHigh,
		"AI code generation failed", err).
		WithContext("ticket_key", ticketKey).
		WithRetryable(true)
}

func NewAIFixError(err error, ticketKey string, errorType string) *AgentError {
	return Wrap(ErrCodeAIFix, CategoryAI, SeverityHigh,
		"AI error fixing failed", err).
		WithContext("ticket_key", ticketKey).
		WithContext("error_type", errorType).
		WithRetryable(true)
}

func NewAITimeoutError(ticketKey string, duration time.Duration) *AgentError {
	return New(ErrCodeAITimeout, CategoryAI, SeverityMedium,
		"AI request timed out").
		WithContext("ticket_key", ticketKey).
		WithContext("timeout_duration", duration).
		WithRetryable(true)
}

func NewAIInvalidResponseError(err error, response string) *AgentError {
	preview := response
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return Wrap(ErrCodeAIInvalidResp, CategoryAI, SeverityHigh,
		"AI returned invalid response", err).
		WithContext("response_preview", preview).
		WithRetryable(true)
}

// Validation errors
func NewValidationPathTraversalError(path string) *AgentError {
	return New(ErrCodeValidateTraversal, CategoryValidation, SeverityCritical,
		"path traversal attempt detected").
		WithContext("path", path).
		WithRetryable(false)
}

func NewValidationDisallowedDirError(path string, allowedDirs []string) *AgentError {
	return New(ErrCodeValidateDisallowed, CategoryValidation, SeverityMedium,
		"file path not in allowed directories").
		WithContext("path", path).
		WithContext("allowed_dirs", allowedDirs).
		WithRetryable(false)
}

func NewValidationMaxFilesError(count, max int) *AgentError {
	return New(ErrCodeValidateMaxFiles, CategoryValidation, SeverityMedium,
		"too many files in change set").
		WithContext("file_count", count).
		WithContext("max_allowed", max).
		WithRetryable(false)
}

// Ticketing errors
func NewTicketFetchError(err error, project, assignee string) *AgentError {
	return Wrap(ErrCodeTicketFetch, CategoryTicketing, SeverityHigh,
		"failed to fetch tickets", err).
		WithContext("project", project).
		WithContext("assignee", assignee).
		WithRetryable(true)
}

func NewTicketUpdateError(err error, ticketKey, status string) *AgentError {
	return Wrap(ErrCodeTicketUpdate, CategoryTicketing, SeverityMedium,
		"failed to update ticket status", err).
		WithContext("ticket_key", ticketKey).
		WithContext("target_status", status).
		WithRetryable(true)
}

// Config errors
func NewConfigMissingError(field string) *AgentError {
	return New(ErrCodeConfigMissing, CategoryConfig, SeverityCritical,
		"required configuration missing").
		WithContext("field", field).
		WithRetryable(false)
}

func NewConfigInvalidError(field string, value interface{}, reason string) *AgentError {
	return New(ErrCodeConfigInvalid, CategoryConfig, SeverityCritical,
		"invalid configuration value").
		WithContext("field", field).
		WithContext("value", value).
		WithContext("reason", reason).
		WithRetryable(false)
}

func NewConfigConflictError(fields []string, reason string) *AgentError {
	return New(ErrCodeConfigConflict, CategoryConfig, SeverityCritical,
		"conflicting configuration").
		WithContext("conflicting_fields", fields).
		WithContext("reason", reason).
		WithRetryable(false)
}

// Quality gate errors
func NewQualityVetError(output string) *AgentError {
	return New(ErrCodeQualityVet, CategoryQuality, SeverityMedium,
		"go vet failed").
		WithContext("vet_output", output).
		WithRetryable(true) // Self-healing can fix
}

func NewQualityTestError(output string) *AgentError {
	return New(ErrCodeQualityTest, CategoryQuality, SeverityMedium,
		"go test failed").
		WithContext("test_output", output).
		WithRetryable(true) // Self-healing can fix
}

func NewQualityBuildError(output string) *AgentError {
	return New(ErrCodeQualityBuild, CategoryQuality, SeverityHigh,
		"go build failed").
		WithContext("build_output", output).
		WithRetryable(true) // Self-healing can fix
}

// Context errors
func NewContextBuildError(err error, strategy string) *AgentError {
	return Wrap(ErrCodeContextBuild, CategoryContext, SeverityMedium,
		"failed to build repository context", err).
		WithContext("strategy", strategy).
		WithRetryable(true)
}

func NewContextIndexError(err error, repoPath string) *AgentError {
	return Wrap(ErrCodeContextIndex, CategoryContext, SeverityMedium,
		"failed to build file index", err).
		WithContext("repo_path", repoPath).
		WithRetryable(true)
}
