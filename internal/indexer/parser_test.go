package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMinimalContext_GoFile(t *testing.T) {
	// Create a test Go file with various declarations
	testCode := `package testpkg

import (
	"fmt"
	"strings"
)

// User represents a user in the system
type User struct {
	ID   int
	Name string
}

// UserService handles user operations
type UserService interface {
	GetUser(id int) (*User, error)
	CreateUser(name string) (*User, error)
}

const (
	MaxUsers = 100
	MinAge   = 18
)

var (
	DefaultUser = &User{ID: 0, Name: "default"}
)

// GetUserByID retrieves a user by their ID
// Returns the user and any error encountered
func GetUserByID(id int) (*User, error) {
	// This is implementation detail
	if id < 0 {
		return nil, fmt.Errorf("invalid id")
	}
	return &User{ID: id, Name: "Test"}, nil
}

// (u *User) String returns string representation
func (u *User) String() string {
	// Implementation details
	return fmt.Sprintf("User{ID: %d, Name: %s}", u.ID, u.Name)
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Verify package declaration
	assert.Contains(t, result, "package testpkg")

	// Verify imports are included
	assert.Contains(t, result, `"fmt"`)
	assert.Contains(t, result, `"strings"`)

	// Verify type definitions are included
	assert.Contains(t, result, "type User struct")
	assert.Contains(t, result, "ID int")
	assert.Contains(t, result, "Name string")

	// Verify interface is included
	assert.Contains(t, result, "type UserService interface")
	assert.Regexp(t, `GetUser\s*\(id int\) \(\*User, error\)`, result)

	// Verify constants are included
	assert.Contains(t, result, "const")
	assert.Contains(t, result, "MaxUsers")

	// Verify variables are included (but not full values)
	assert.Contains(t, result, "var")
	assert.Contains(t, result, "DefaultUser")

	// Verify function signatures are included
	assert.Contains(t, result, "func GetUserByID(id int) (*User, error)")

	// Verify method signatures are included
	assert.Contains(t, result, "func (u *User) String() string")

	// Verify comments are included
	assert.Contains(t, result, "User represents a user")
	assert.Contains(t, result, "GetUserByID retrieves a user")

	// IMPORTANT: Verify implementation details are NOT included
	assert.NotContains(t, result, "if id < 0")
	assert.NotContains(t, result, `fmt.Sprintf("User{ID:`)
	assert.NotContains(t, result, "This is implementation detail")
}

func TestExtractMinimalContext_InterfaceWithMethods(t *testing.T) {
	testCode := `package test

// Repository defines data access methods
type Repository interface {
	// Find retrieves an entity by ID
	Find(id string) (interface{}, error)
	// Save persists an entity
	Save(entity interface{}) error
	// Delete removes an entity
	Delete(id string) error
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	assert.Contains(t, result, "type Repository interface")
	assert.Regexp(t, `Find\s*\(id string\) \(interface\{\}, error\)`, result)
	assert.Regexp(t, `Save\s*\(entity interface\{\}\) error`, result)
	assert.Regexp(t, `Delete\s*\(id string\) error`, result)
	assert.Contains(t, result, "Repository defines data access")
}

func TestExtractMinimalContext_ComplexTypes(t *testing.T) {
	testCode := `package test

type Config struct {
	Values  map[string]interface{}
	Handler func(string) error
	Channel chan int
	Slice   []string
}

func ProcessData(input chan<- string, output <-chan int) error {
	// Implementation
	return nil
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Verify complex types are preserved
	assert.Regexp(t, `map\[string\]interface\{\}`, result)
	assert.Regexp(t, `func\s*\(string\) error`, result)
	assert.Contains(t, result, "chan int")
	assert.Contains(t, result, "[]string")

	// Verify channel directions
	assert.Contains(t, result, "chan<- string")
	assert.Contains(t, result, "<-chan int")

	// Implementation should not be included
	assert.NotContains(t, result, "return nil")
}

func TestExtractMinimalContext_NonGoFile(t *testing.T) {
	testContent := `# README

This is a test file.
It has multiple lines.
Line 3
Line 4
Line 5
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "README.md")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Should include file path header
	assert.Contains(t, result, "# File:")
	assert.Contains(t, result, "README.md")

	// Should include content
	assert.Contains(t, result, "This is a test file")
	assert.Contains(t, result, "Line 3")
}

func TestExtractMinimalContext_SmallNonGoFileNotTruncated(t *testing.T) {
	// A file with many short lines but well under the full-content size
	// threshold should be returned in full, not truncated to a prefix -
	// declarative config files (e.g. Terraform) have no "head" that's more
	// relevant than the rest, so blindly cutting them hides real content
	// from the model instead of summarizing it.
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("Line ")
		sb.WriteString(string(rune('0' + i)))
		sb.WriteString("\n")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "small.txt")
	err := os.WriteFile(testFile, []byte(sb.String()), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	assert.NotContains(t, result, "... (truncated)")
	assert.Contains(t, result, "Line ") // last line (100th)
}

func TestExtractMinimalContext_LargeNonGoFile(t *testing.T) {
	// Create a file that exceeds the full-content size threshold.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString(fmt.Sprintf("Line %d of filler content to exceed the threshold\n", i))
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.txt")
	err := os.WriteFile(testFile, []byte(sb.String()), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Should be truncated
	assert.Contains(t, result, "... (truncated)")

	// Should not include all 2000 lines
	lines := strings.Split(result, "\n")
	assert.Less(t, len(lines), 2000, "Should truncate large files")
}

func TestExtractMinimalContext_InvalidGoFile(t *testing.T) {
	testCode := `package test

// This is invalid Go code
func broken syntax here {
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	// Should fall back to non-Go extraction
	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Should still return something (fallback behavior)
	assert.NotEmpty(t, result)
}

func TestExtractMinimalContext_FileNotFound(t *testing.T) {
	_, err := ExtractMinimalContext("/nonexistent/file.go")
	assert.Error(t, err)
}

func TestExtractMinimalContext_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.go")
	err := os.WriteFile(testFile, []byte(""), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	// Should handle empty files gracefully by falling back to non-Go extraction
	require.NoError(t, err)
	// Should at least have the file path header
	assert.Contains(t, result, "# File:")
}

func TestExtractMinimalContext_MethodsWithMultipleReceivers(t *testing.T) {
	testCode := `package test

type Service struct {
	name string
}

func (s *Service) GetName() string {
	return s.name
}

func (s Service) IsValid() bool {
	return true
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Both pointer and value receivers should be included
	assert.Contains(t, result, "func (s *Service) GetName() string")
	assert.Contains(t, result, "func (s Service) IsValid() bool")

	// Implementation should not be included
	assert.NotContains(t, result, "return s.name")
	assert.NotContains(t, result, "return true")
}

func TestExtractMinimalContext_VariadicFunctions(t *testing.T) {
	testCode := `package test

func Printf(format string, args ...interface{}) error {
	return nil
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Variadic parameters should be preserved
	assert.Regexp(t, `func Printf\s*\(format string, args \.\.\.interface\{\}\) error`, result)

	// Implementation should not be included
	assert.NotContains(t, result, "return nil")
}

func TestExtractMinimalContext_MultipleReturnValues(t *testing.T) {
	testCode := `package test

func GetUserData(id int) (name string, age int, err error) {
	return "test", 25, nil
}

func GetConfig() (map[string]string, error) {
	return nil, nil
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Named return values
	assert.Contains(t, result, "func GetUserData(id int) (name string, age int, err error)")

	// Anonymous return values
	assert.Contains(t, result, "func GetConfig() (map[string]string, error)")

	// Implementation should not be included
	assert.NotContains(t, result, `return "test", 25, nil`)
}

func TestExtractMinimalContext_StructTags(t *testing.T) {
	testCode := `package test

type User struct {
	ID   int    ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\" db:\"user_name\"`" + `
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	result, err := ExtractMinimalContext(testFile)
	require.NoError(t, err)

	// Struct tags should be preserved
	assert.Contains(t, result, "`json:\"id\"`")
	assert.Contains(t, result, "`json:\"name\"")
}

// Benchmark tests
func BenchmarkExtractMinimalContext_SmallFile(b *testing.B) {
	testCode := `package test

type User struct {
	ID   int
	Name string
}

func GetUser(id int) (*User, error) {
	return &User{ID: id}, nil
}
`

	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractMinimalContext(testFile)
	}
}

func BenchmarkExtractMinimalContext_LargeFile(b *testing.B) {
	// Create a realistic large file
	var sb strings.Builder
	sb.WriteString("package test\n\n")

	// Add 50 type definitions
	for i := 0; i < 50; i++ {
		sb.WriteString("type Type")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString(" struct { Field int }\n\n")
	}

	// Add 50 functions
	for i := 0; i < 50; i++ {
		sb.WriteString("func Function")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString("() error { return nil }\n\n")
	}

	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "large.go")
	err := os.WriteFile(testFile, []byte(sb.String()), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractMinimalContext(testFile)
	}
}
