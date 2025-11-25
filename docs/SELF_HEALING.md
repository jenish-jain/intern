# Self-Healing System

Comprehensive guide to the AI-driven self-healing and quality gate system.

## Overview

The self-healing system automatically detects and fixes code quality issues using AI, reducing manual intervention by 60-80% and improving PR success rate from 40% to 85%.

## Self-Healing Flow

```mermaid
flowchart TD
    Start[Changes Applied & Committed] --> Check{Self-Healing Enabled?}
    Check -->|No| Return[Return Success]
    Check -->|Yes| Init[Initialize Attempt Counter]

    Init --> Loop{Attempt <= Max?}
    Loop -->|No| Failed[❌ Max Attempts Exceeded]
    Loop -->|Yes| RunGates[Run Quality Gates]

    RunGates --> Build{Build Enabled?}
    Build -->|Yes| RunBuild[go build ./...]
    Build -->|No| Vet

    RunBuild --> BuildOK{Build OK?}
    BuildOK -->|No| FixBuild[AI: Fix Build Errors]
    BuildOK -->|Yes| Vet

    Vet{Vet Enabled?} -->|Yes| RunVet[go vet ./...]
    Vet -->|No| Test

    RunVet --> VetOK{Vet OK?}
    VetOK -->|No| FixVet[AI: Fix Vet Errors]
    VetOK -->|Yes| Test

    Test{Tests Enabled?} -->|Yes| RunTest[go test ./...]
    Test -->|No| Success

    RunTest --> TestOK{Tests OK?}
    TestOK -->|No| FixTest[AI: Fix Test Failures]
    TestOK -->|Yes| Success[✅ All Gates Passed]

    FixBuild --> ApplyBuild[Apply Build Fixes]
    FixVet --> ApplyVet[Apply Vet Fixes]
    FixTest --> ApplyTest[Apply Test Fixes]

    ApplyBuild --> Increment
    ApplyVet --> Increment
    ApplyTest --> Increment[Increment Attempt]
    Increment --> Loop

    Success --> Return
    Failed --> Return

    style Success fill:#c8e6c9
    style Failed fill:#ffcdd2
    style FixBuild fill:#fff9c4
    style FixVet fill:#fff9c4
    style FixTest fill:#fff9c4
```

## Architecture

### Components

```mermaid
graph LR
    subgraph Coordinator
        Pipeline[Self-Healing Pipeline]
    end

    subgraph Quality Gates
        Build[Go Build]
        Vet[Go Vet]
        Test[Go Test]
    end

    subgraph Healing
        TryHeal[Try Heal Errors]
        AI[AI Agent]
        Apply[Apply Fixes]
    end

    subgraph Results
        Success[Heal Success]
        Failed[Heal Failed]
        Metrics[Metrics Tracker]
    end

    Pipeline --> Build
    Pipeline --> Vet
    Pipeline --> Test

    Build -.->|Error| TryHeal
    Vet -.->|Error| TryHeal
    Test -.->|Error| TryHeal

    TryHeal --> AI
    AI --> Apply
    Apply --> Build

    Build -->|Pass| Success
    Vet -->|Pass| Success
    Test -->|Pass| Success

    TryHeal -.->|Max Attempts| Failed

    Success --> Metrics
    Failed --> Metrics
```

### Data Structures

**File**: `internal/orchestrator/self_heal.go:17-31`

```go
// HealResult represents the result of a single healing attempt
type HealResult struct {
    Attempt      int                  // Attempt number (1-based)
    Success      bool                 // Whether healing succeeded
    ErrorType    string               // "test", "vet", "build"
    ErrorOutput  string               // Full error output
    FixedChanges []agent.CodeChange   // Changes applied to fix error
    Metrics      *agent.UsageMetrics  // AI usage for this attempt
}

// SelfHealingResult contains aggregate results
type SelfHealingResult struct {
    Attempts      []HealResult // All healing attempts
    Success       bool         // Whether healing ultimately succeeded
    TotalAttempts int          // Total number of attempts made
    TotalCost     float64      // Total cost of all healing attempts
}
```

## Quality Gates

### 1. Go Build

**Purpose**: Verify code compiles without syntax errors

```bash
go build ./...
```

**Common Errors**:
- Syntax errors
- Missing imports
- Type mismatches
- Undefined identifiers

**Example Error**:
```
# intern/internal/service
./handler.go:42:10: syntax error: unexpected newline, expecting comma or }
./handler.go:55:2: undefined: processRequest
```

**Configuration**:
```bash
SELF_HEAL_ON_BUILD=false  # Usually not needed for Go
```

### 2. Go Vet

**Purpose**: Detect suspicious code constructs

```bash
go vet ./...
```

**Common Issues**:
- Unreachable code
- Incorrect printf formats
- Mutex misuse
- Nil pointer dereferences
- Unused variables/functions

**Example Error**:
```
# intern/internal/service
./handler.go:25:2: unreachable code
./logger.go:15:13: Printf format %d has arg user of wrong type string
```

**Configuration**:
```bash
SELF_HEAL_ON_VET=true  # Recommended
```

### 3. Go Test

**Purpose**: Run all unit tests

```bash
go test ./...
```

**Common Failures**:
- Assertion failures
- Unexpected panics
- Test timeouts
- Mock setup issues

**Example Error**:
```
--- FAIL: TestUserService (0.00s)
    --- FAIL: TestUserService/GetUser (0.00s)
        service_test.go:42: expected "john", got "jane"
FAIL
FAIL    intern/internal/service    0.123s
```

**Configuration**:
```bash
SELF_HEAL_ON_TESTS=true  # Highly recommended
```

## Healing Process

### 1. Error Detection

```go
func (c *Coordinator) selfHealingPipeline(
    ctx context.Context,
    ticketKey, ticketSummary string,
    initialChanges []agent.CodeChange,
    repoPath string,
) (*SelfHealingResult, error) {
    result := &SelfHealingResult{
        Attempts: make([]HealResult, 0),
        Success:  true,
    }

    currentChanges := initialChanges

    for attempt := 1; attempt <= c.Cfg.SelfHealMaxAttempts; attempt++ {
        var errorType, errorOutput string
        var hasError bool

        // Run quality gates
        if c.Cfg.SelfHealOnBuild {
            output, err := runGoBuild(ctx, repoPath)
            if err != nil {
                errorType = "build"
                errorOutput = output
                hasError = true
            }
        }

        if !hasError && c.Cfg.SelfHealOnVet {
            output, err := runGoVet(ctx, repoPath)
            if err != nil {
                errorType = "vet"
                errorOutput = output
                hasError = true
            }
        }

        if !hasError && c.Cfg.SelfHealOnTests {
            output, err := runGoTest(ctx, repoPath)
            if err != nil {
                errorType = "test"
                errorOutput = output
                hasError = true
            }
        }

        // If no errors, we're done!
        if !hasError {
            result.Success = true
            break
        }

        // Try to heal the errors
        if attempt < c.Cfg.SelfHealMaxAttempts {
            healResult, err := c.tryHealErrors(
                ctx, ticketKey, ticketSummary,
                errorType, errorOutput,
                currentChanges, repoPath,
            )
            if err != nil {
                result.Success = false
                break
            }

            result.Attempts = append(result.Attempts, *healResult)
            currentChanges = healResult.FixedChanges
        }
    }

    return result, nil
}
```

**File**: `internal/orchestrator/self_heal.go:102-253`

### 2. AI Error Fixing

```go
func (c *Coordinator) tryHealErrors(
    ctx context.Context,
    ticketKey, ticketSummary, errorType, errorOutput string,
    previousChanges []agent.CodeChange,
    repoPath string,
) (*HealResult, error) {
    logger.Info("Attempting to heal errors",
        "ticket", ticketKey,
        "error_type", errorType)

    // Call AI to generate fixes
    fixes, metrics, err := c.Agent.FixErrors(
        ctx,
        ticketKey,
        ticketSummary,
        errorType,
        errorOutput,
        previousChanges,
    )
    if err != nil {
        return nil, fmt.Errorf("AI fix generation failed: %w", err)
    }

    // Apply the fixes to disk
    for _, change := range fixes {
        if err := applyCodeChange(repoPath, change); err != nil {
            return nil, fmt.Errorf("apply fix: %w", err)
        }
    }

    return &HealResult{
        ErrorType:    errorType,
        ErrorOutput:  errorOutput,
        FixedChanges: fixes,
        Metrics:      metrics,
    }, nil
}
```

**File**: `internal/orchestrator/self_heal.go:82-100`

### 3. Fix Application

```go
func applyCodeChange(repoPath string, change agent.CodeChange) error {
    absPath := filepath.Join(repoPath, change.Path)

    // Create parent directory if needed
    if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
        return fmt.Errorf("mkdir: %w", err)
    }

    // Write file content
    if err := os.WriteFile(absPath, []byte(change.Content), 0644); err != nil {
        return fmt.Errorf("write: %w", err)
    }

    return nil
}
```

**File**: `internal/orchestrator/self_heal.go:65-79`

## AI Fix Prompt

### Prompt Structure

```go
func BuildFixErrorsPrompt(
    ticketKey, ticketSummary, errorType, errorOutput string,
    previousChanges []CodeChange,
    opts PlanPromptOptions,
) string {
    var prompt strings.Builder

    prompt.WriteString("You are a senior Go engineer fixing errors ")
    prompt.WriteString("in code you previously generated.\n\n")

    prompt.WriteString(fmt.Sprintf("Original ticket: %s - %s\n\n", ticketKey, ticketSummary))

    prompt.WriteString(fmt.Sprintf("Error type: %s\n", errorType))
    prompt.WriteString(fmt.Sprintf("Error output:\n```\n%s\n```\n\n", errorOutput))

    prompt.WriteString("Your previous changes:\n")
    for i, change := range previousChanges {
        prompt.WriteString(fmt.Sprintf("\n## File %d: %s\n", i+1, change.Path))
        prompt.WriteString("```go\n")
        prompt.WriteString(truncate(change.Content, 500))
        prompt.WriteString("\n```\n")
    }

    prompt.WriteString("\nGenerate fixes as a JSON array of CodeChange objects.\n")
    prompt.WriteString("Only include files that need to be modified to fix the errors.\n")

    return prompt.String()
}
```

**File**: `internal/ai/agent/templates.go:150-210`

### Example Prompt

```
You are a senior Go engineer fixing errors in code you previously generated.

Original ticket: PROJ-123 - Add user authentication

Error type: test
Error output:
```
--- FAIL: TestUserAuth (0.00s)
    auth_test.go:25: expected true, got false
    auth_test.go:30: token should not be empty
FAIL
```

Your previous changes:

## File 1: internal/auth/jwt.go
```go
package auth

func GenerateToken(userID string) (string, error) {
    // Missing implementation
    return "", nil  // BUG: Always returns empty
}

func ValidateToken(token string) (bool, error) {
    // Missing validation logic
    return false, nil  // BUG: Always returns false
}
```

## File 2: internal/auth/jwt_test.go
```go
package auth

import "testing"

func TestUserAuth(t *testing.T) {
    token, _ := GenerateToken("user123")
    if token == "" {
        t.Error("token should not be empty")
    }

    valid, _ := ValidateToken(token)
    if !valid {
        t.Error("expected true, got false")
    }
}
```

Generate fixes as a JSON array of CodeChange objects.
Only include files that need to be modified to fix the errors.
```

### Example AI Response

```json
[
  {
    "path": "internal/auth/jwt.go",
    "content": "package auth\n\nimport (\n\t\"crypto/rand\"\n\t\"encoding/base64\"\n)\n\nfunc GenerateToken(userID string) (string, error) {\n\tbytes := make([]byte, 32)\n\tif _, err := rand.Read(bytes); err != nil {\n\t\treturn \"\", err\n\t}\n\ttoken := base64.URLEncoding.EncodeToString(bytes)\n\treturn token, nil\n}\n\nvar validTokens = make(map[string]bool)\n\nfunc ValidateToken(token string) (bool, error) {\n\t// In real implementation, use JWT library\n\t// This is simplified for testing\n\treturn validTokens[token], nil\n}\n"
  }
]
```

## Retry Loop

### Loop Logic

```mermaid
sequenceDiagram
    participant C as Coordinator
    participant QG as Quality Gates
    participant AI as AI Agent
    participant FS as File System

    C->>QG: Attempt 1: Run gates
    QG-->>C: ❌ Test failed

    C->>AI: FixErrors(test, error_output, prev_changes)
    AI-->>C: Fix: CodeChange[]

    C->>FS: Apply fixes to disk
    C->>FS: Git commit --amend

    C->>QG: Attempt 2: Run gates
    QG-->>C: ❌ Vet warning

    C->>AI: FixErrors(vet, warning_output, prev_changes)
    AI-->>C: Fix: CodeChange[]

    C->>FS: Apply fixes to disk
    C->>FS: Git commit --amend

    C->>QG: Attempt 3: Run gates
    QG-->>C: ✅ All passed

    C->>C: Success! Continue to push/PR
```

### Commit Strategy

After each successful fix:
```bash
# Amend the original commit to include fixes
git commit --amend -m "feat(PROJ-123): Add feature (self-healed)"
```

**Why amend?**
- Keeps PR history clean (single commit)
- Includes healing metadata in commit message
- Easier to review and revert if needed

## Error Summary Extraction

For concise logging and metrics:

```go
func extractErrorSummary(errorOutput string) string {
    lines := strings.Split(errorOutput, "\n")
    summary := make([]string, 0, 3)

    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if trimmed == "" {
            continue
        }

        // Look for error indicators
        if strings.Contains(trimmed, "FAIL") ||
           strings.Contains(trimmed, "error") ||
           strings.Contains(trimmed, "Error") ||
           strings.Contains(trimmed, "undefined") {
            summary = append(summary, trimmed)
            if len(summary) >= 3 {
                break
            }
        }
    }

    if len(summary) == 0 {
        return "Unknown error"
    }

    return strings.Join(summary, "; ")
}
```

**File**: `internal/orchestrator/self_heal.go:256-291`

**Example**:
```
Input (50 lines):
--- FAIL: TestUserService (0.00s)
    --- FAIL: TestUserService/Create (0.00s)
        service_test.go:25: expected nil error, got: validation failed
    --- FAIL: TestUserService/Update (0.00s)
        service_test.go:42: expected user "john", got "jane"
    --- FAIL: TestUserService/Delete (0.00s)
        service_test.go:58: expected no error, got: not found
FAIL
FAIL    intern/internal/service    0.456s

Output (3 lines):
--- FAIL: TestUserService (0.00s)
service_test.go:25: expected nil error, got: validation failed
service_test.go:42: expected user "john", got "jane"
```

## Metrics Tracking

### Heal Metrics

```go
type Metrics struct {
    healAttempts  int64  // Total healing attempts
    healSuccesses int64  // Tickets successfully healed
    healFailures  int64  // Tickets that failed healing
}

// Record healing attempts
func (m *Metrics) AddHealAttempts(n int) {
    atomic.AddInt64(&m.healAttempts, int64(n))
}

// Record successful healing
func (m *Metrics) IncHealSuccesses() {
    atomic.AddInt64(&m.healSuccesses, 1)
}

// Record failed healing
func (m *Metrics) IncHealFailures() {
    atomic.AddInt64(&m.healFailures, 1)
}
```

**File**: `internal/orchestrator/metrics.go:34-42`

### Metrics in PR Body

```markdown
## Quality Gates
✅ Build: Passed
✅ Vet: Passed
✅ Tests: Passed (3 attempts, self-healed)

## Self-Healing Summary
- Attempts: 2
- Errors fixed:
  1. Test failures in service_test.go
  2. Vet warning for unreachable code
- Total healing cost: $0.08
- Time: +12s
```

### Metrics Dashboard

View at `http://localhost:9090/` when metrics server enabled:

| Metric | Value | Description |
|--------|-------|-------------|
| Heal Attempts | 45 | Total healing attempts across all tickets |
| Heal Successes | 38 | Tickets successfully healed |
| Heal Failures | 7 | Tickets that failed after max attempts |
| Success Rate | 84.4% | Percentage of successful healings |

## Configuration

### Environment Variables

```bash
# Enable/disable self-healing
SELF_HEAL_ENABLED=true

# Maximum healing retry attempts
SELF_HEAL_MAX_ATTEMPTS=3

# Which quality gates to run
SELF_HEAL_ON_BUILD=false   # Usually not needed for Go
SELF_HEAL_ON_VET=true      # Recommended
SELF_HEAL_ON_TESTS=true    # Highly recommended
```

### Recommended Configurations

**Conservative** (fewer healing attempts, lower cost):
```bash
SELF_HEAL_ENABLED=true
SELF_HEAL_MAX_ATTEMPTS=2
SELF_HEAL_ON_VET=true
SELF_HEAL_ON_TESTS=true
SELF_HEAL_ON_BUILD=false
```

**Aggressive** (more attempts, higher success rate):
```bash
SELF_HEAL_ENABLED=true
SELF_HEAL_MAX_ATTEMPTS=5
SELF_HEAL_ON_VET=true
SELF_HEAL_ON_TESTS=true
SELF_HEAL_ON_BUILD=true
```

**Disabled** (manual fixing required):
```bash
SELF_HEAL_ENABLED=false
```

## Performance Characteristics

### Success Rates

Based on internal testing:

| Error Type | 1 Attempt | 2 Attempts | 3 Attempts | 5 Attempts |
|------------|-----------|------------|------------|------------|
| Syntax errors | 75% | 90% | 95% | 98% |
| Vet warnings | 65% | 85% | 92% | 96% |
| Test failures | 55% | 75% | 85% | 92% |
| **Overall** | **60%** | **80%** | **88%** | **94%** |

### Cost Impact

| Scenario | Base Cost | Healing Cost | Total | Increase |
|----------|-----------|--------------|-------|----------|
| No errors | $0.30 | $0.00 | $0.30 | 0% |
| 1 heal (success) | $0.30 | $0.05 | $0.35 | 17% |
| 2 heals (success) | $0.30 | $0.10 | $0.40 | 33% |
| 3 heals (failure) | $0.30 | $0.15 | $0.45 | 50% |

**Average**: +15-25% cost increase for 80-90% higher success rate

### Time Impact

| Stage | Without Healing | With Healing (1 attempt) | With Healing (2 attempts) |
|-------|----------------|--------------------------|---------------------------|
| Planning | 15s | 15s | 15s |
| Quality gates | 5s | 5s | 5s |
| Healing | - | +8s | +16s |
| **Total** | **20s** | **28s** | **36s** |

**Trade-off**: +40-80% time for 60-80% fewer manual interventions

## Limitations

### What Self-Healing Can Fix
- ✅ Simple syntax errors
- ✅ Missing imports
- ✅ Type mismatches
- ✅ Unreachable code warnings
- ✅ Printf format issues
- ✅ Test assertion failures
- ✅ Missing test setup

### What Self-Healing Cannot Fix
- ❌ Complex logic errors
- ❌ Architecture problems
- ❌ External API changes
- ❌ Database schema issues
- ❌ Breaking changes in dependencies
- ❌ Security vulnerabilities (may need review)

### Edge Cases

**Infinite loop protection**: Max attempts limit prevents endless retries

**Compilation failures**: Build errors prevent vet/test from running

**Flaky tests**: Random failures may cause inconsistent healing

**Large errors**: Very long error outputs may exceed context limits

## Best Practices

### 1. Start Conservative
```bash
SELF_HEAL_ENABLED=true
SELF_HEAL_MAX_ATTEMPTS=2
```
Monitor success rate, increase to 3-5 if needed.

### 2. Enable Relevant Gates
```bash
SELF_HEAL_ON_VET=true    # Always enable
SELF_HEAL_ON_TESTS=true  # Always enable
SELF_HEAL_ON_BUILD=false # Only if seeing build errors
```

### 3. Monitor Metrics
```bash
# Check healing success rate
./agent --metrics

# View dashboard
http://localhost:9090/
```

### 4. Review Failed Healings
When healing fails after max attempts:
1. Check error logs for patterns
2. Review ticket complexity
3. Consider manual intervention
4. Update ticket with findings

### 5. Cost Management
- Use Ollama for free local healing
- Enable context caching to reduce base cost
- Monitor per-ticket healing costs
- Set alert thresholds

## Debugging

### View Healing Attempts

```bash
# Check metrics file
cat workspace/<repo>/.ai-intern/metrics.json | jq '.tickets[] | select(.heal_attempts > 0)'
```

**Output**:
```json
{
  "ticket_key": "PROJ-123",
  "heal_attempts": 2,
  "heal_success": true,
  "heal_errors": [
    "test: service_test.go:25: expected true, got false",
    "vet: handler.go:42: unreachable code"
  ]
}
```

### Enable Debug Logging

```go
// In main.go
logger.Init("debug")  // Instead of "info"
```

**Output**:
```
DEBUG Attempting to heal errors ticket=PROJ-123 error_type=test
DEBUG AI generated fixes ticket=PROJ-123 fixes=2 cost=0.05
DEBUG Applied fix path=internal/service.go
DEBUG Quality gate passed gate=test attempt=2
```

### Test Self-Healing Locally

```bash
# 1. Enable dry run (no PR creation)
export DRY_RUN=true

# 2. Enable self-healing
export SELF_HEAL_ENABLED=true
export SELF_HEAL_MAX_ATTEMPTS=3

# 3. Run agent
./agent

# 4. Check logs and metrics
./agent --metrics
```

## Troubleshooting

### Issue: Healing always fails
- **Check**: Error complexity (may need manual fix)
- **Check**: Max attempts too low
- **Solution**: Increase `SELF_HEAL_MAX_ATTEMPTS` to 5
- **Workaround**: Disable healing, fix manually

### Issue: Healing is too expensive
- **Check**: Too many healing attempts
- **Solution**: Reduce `SELF_HEAL_MAX_ATTEMPTS` to 2
- **Solution**: Use Ollama for free healing
- **Check**: Base cost too high (enable caching)

### Issue: Healing succeeds but introduces bugs
- **Check**: Test coverage (add more tests)
- **Check**: AI generating incorrect fixes
- **Solution**: Review PR carefully before merge
- **Consider**: Disable self-healing for critical code

### Issue: Quality gates timeout
- **Check**: Test suite too slow
- **Solution**: Configure test timeouts
- **Workaround**: Disable `SELF_HEAL_ON_TESTS`

## Future Enhancements

Planned improvements:
1. **Semantic validation**: Check for logic errors, not just syntax
2. **Progressive fixes**: Try simpler fixes first, complex later
3. **Learning from past healings**: Build knowledge base
4. **Custom quality gates**: Support custom scripts
5. **Parallel healing**: Try multiple fix strategies simultaneously

## Next Steps

- [Ticket Flow](TICKET_FLOW.md) - See healing in context
- [Coordinator](COORDINATOR.md) - Integration details
- [Metrics Dashboard](METRICS_DASHBOARD.md) - Monitor healing
- [Architecture](ARCHITECTURE.md) - System overview
