# Multi-Provider AI Support Implementation Plan

## Overview
Extend the AI agent to support multiple AI providers, specifically adding Ollama support for local LLMs (Qwen2.5-coder, DeepSeek Coder) while maintaining the existing Anthropic Claude integration.

## Goals
- **Pluggable Architecture**: Easy to add new providers via interface
- **Config-Driven**: Switch providers through environment variables
- **Local LLM Support**: Run models locally via Ollama
- **Cost Tracking**: Proper metrics for both paid and free providers
- **Backwards Compatible**: Existing Anthropic configs should continue working

## Architecture Principles
- Use existing `agent.Agent` interface - no changes needed
- Factory pattern for provider initialization
- Provider-specific implementations in separate packages
- Centralized configuration validation

---

## Phase 1: Configuration & Architecture Updates

### 1.1 Extend Configuration (`internal/config/config.go`)
**Changes:**
- Add `AIProvider` string field (values: "anthropic", "ollama")
- Add `OllamaBaseURL` string field (default: "http://localhost:11434")
- Add `OllamaModel` string field (e.g., "qwen2.5-coder:7b")
- Make `AnthropicAPIKey` optional (only validate when provider = "anthropic")
- Update `Validate()` method for provider-specific validation

**New Environment Variables:**
```bash
AI_PROVIDER=ollama                          # "anthropic" or "ollama"
OLLAMA_BASE_URL=http://localhost:11434      # Ollama API endpoint
OLLAMA_MODEL=qwen2.5-coder:7b               # Model name
```

**Validation Rules:**
- If `AI_PROVIDER=anthropic`: require `ANTHROPIC_API_KEY`
- If `AI_PROVIDER=ollama`: require `OLLAMA_MODEL`
- Default provider: "anthropic" (backwards compatibility)

### 1.2 Create Provider Factory (`internal/ai/agent/factory.go`)
**Purpose:** Centralize provider instantiation logic

**Function Signature:**
```go
func NewAgent(cfg *config.Config) (Agent, error)
```

**Logic:**
- Switch on `cfg.AIProvider`
- Return appropriate client implementation
- Validate provider-specific requirements
- Return error if provider unknown or misconfigured

---

## Phase 2: Ollama Client Implementation

### 2.1 Package Structure
```
internal/ai/agent/ollama/
├── client.go          # Main Ollama client
├── types.go           # Request/response types
└── client_test.go     # Unit tests
```

### 2.2 Ollama Client Implementation

**File: `internal/ai/agent/ollama/client.go`**

**Client Structure:**
```go
type Client struct {
    BaseURL string       // Ollama server URL
    Model   string       // Model name (e.g., "qwen2.5-coder:7b")
    HTTP    *http.Client // HTTP client with timeout
}
```

**Key Methods:**
- `NewClient(baseURL, model string) *Client`
- `PlanChanges(ctx, ticketKey, summary, description, context) ([]CodeChange, *UsageMetrics, error)`
- `buildRequest()` - Construct Ollama API request
- `parseResponse()` - Parse JSON response to CodeChange array

**Ollama API Endpoints:**
- Primary: `POST /api/generate` (completion endpoint)
- Alternative: `POST /api/chat` (chat endpoint, might work better)

**Error Handling:**
- Connection errors (Ollama not running)
- Model not found errors
- JSON parsing errors
- Timeout handling

### 2.3 Ollama Types (`internal/ai/agent/ollama/types.go`)

**Request Type:**
```go
type GenerateRequest struct {
    Model  string `json:"model"`
    Prompt string `json:"prompt"`
    Stream bool   `json:"stream"`
    Options map[string]interface{} `json:"options,omitempty"`
}
```

**Response Type:**
```go
type GenerateResponse struct {
    Model     string `json:"model"`
    Response  string `json:"response"`
    Done      bool   `json:"done"`
    Context   []int  `json:"context,omitempty"`
    TotalDuration int64 `json:"total_duration,omitempty"`
    // ... other fields
}
```

### 2.4 Cost Calculation Updates

**File: `internal/ai/cost.go`**

**Add Local Model Pricing:**
```go
var OllamaLocal = ModelPricing{
    Name:               "ollama-local",
    InputPricePerMToken:  0.0,
    OutputPricePerMToken: 0.0,
}
```

**Update Client to Use:**
- Ollama client uses `OllamaLocal` pricing (zero cost)
- Still track tokens for metrics purposes
- Indicate "local" in metrics reports

---

## Phase 3: Integration & Wiring

### 3.1 Update Main Application (`cmd/agent/main.go`)

**Changes:**
```go
// Before:
agent := anthropic.NewClient(cfg.AnthropicAPIKey)

// After:
agent, err := agent.NewAgent(cfg)
if err != nil {
    logger.Error("Failed to initialize AI agent: %v", err)
    os.Exit(1)
}
```

### 3.2 Update Sample Config (`.env.example`)

**Add Ollama Section:**
```bash
# AI Provider Configuration
AI_PROVIDER=anthropic                       # Options: anthropic, ollama

# Anthropic Configuration (required if AI_PROVIDER=anthropic)
ANTHROPIC_API_KEY="your-anthropic-api-key"

# Ollama Configuration (required if AI_PROVIDER=ollama)
OLLAMA_BASE_URL="http://localhost:11434"    # Ollama API endpoint
OLLAMA_MODEL="qwen2.5-coder:7b"             # Model name
```

### 3.3 Update Metrics Reporting

**Track Provider in Metrics:**
- Add `Provider` field to metrics
- Show provider in final reports
- Track provider-specific performance

---

## Phase 4: Documentation

### 4.1 Ollama Setup Guide (`docs/OLLAMA_SETUP.md`)

**Sections:**
1. **What is Ollama?**
2. **Installation** (macOS, Linux, Windows, Docker)
3. **Starting Ollama Server**
4. **Pulling Models**
5. **Recommended Models**
6. **Testing Ollama**
7. **Configuration**
8. **Troubleshooting**

### 4.2 Update Main README

**Add Section: "AI Provider Configuration"**
- Supported providers table
- Quick start for each provider
- Links to detailed setup guides

### 4.3 Update CLAUDE.md

**Architecture Section:**
- Document factory pattern
- List supported providers
- Configuration guidelines

---

## Phase 5: Testing Strategy

### 5.1 Unit Tests
- ✅ Config validation for each provider
- ✅ Factory pattern returns correct implementation
- ✅ Ollama client request building
- ✅ Response parsing
- ✅ Cost calculation

### 5.2 Integration Tests
- ✅ End-to-end with mock Ollama server
- ✅ Error handling (server down, model missing)
- ✅ Timeout scenarios

### 5.3 Manual Testing
- ✅ Real Ollama with Qwen2.5-coder
- ✅ Real Ollama with DeepSeek Coder
- ✅ Compare output quality with Anthropic
- ✅ Test various ticket types

---

## Phase 6: Optional Enhancements (Future)

### 6.1 Health Check
- Ping Ollama on startup
- List available models
- Warn if configured model not present

### 6.2 Auto-Pull Models
- `OLLAMA_AUTO_PULL=true` option
- Automatically download model if missing
- Show download progress

### 6.3 Fallback Strategy
- Primary provider + fallback provider
- Automatic failover on errors
- Track fallback usage in metrics

### 6.4 Multi-Model Support
- Different models for different tasks
- Planning vs implementation models
- Cost optimization

---

## Implementation Order

1. ✅ **Config Changes** (30 min)
   - Update `config.go`
   - Update `.env.example`
   - Add validation

2. ✅ **Factory Pattern** (30 min)
   - Create `factory.go`
   - Implement provider switching
   - Add tests

3. ✅ **Ollama Client** (2-3 hours)
   - Create package structure
   - Implement client
   - Add types
   - Write tests

4. ✅ **Cost Updates** (15 min)
   - Add local model pricing
   - Update metrics

5. ✅ **Integration** (30 min)
   - Update `main.go`
   - Test provider switching
   - Validate metrics

6. ✅ **Documentation** (1 hour)
   - Create Ollama setup guide
   - Update README
   - Update CLAUDE.md

7. ✅ **Testing** (1 hour)
   - Manual testing with Ollama
   - Validate code generation
   - Compare quality

**Total Estimate:** 5-6 hours

---

## Recommended Models

### For Code Generation

| Model | Size | Speed | Quality | RAM Required |
|-------|------|-------|---------|--------------|
| **qwen2.5-coder:7b** | 4.7GB | Fast | Good | 8GB |
| qwen2.5-coder:14b | 9GB | Medium | Better | 16GB |
| qwen2.5-coder:32b | 20GB | Slow | Best | 32GB |
| **deepseek-coder:6.7b** | 3.8GB | Fast | Good | 8GB |
| deepseek-coder:33b | 19GB | Slow | Excellent | 32GB |
| codellama:13b | 7.4GB | Medium | Good | 16GB |

**Recommended Starter:** `qwen2.5-coder:7b` - Best balance of speed and quality

---

## Decisions & Assumptions

### Design Decisions
- **Default Provider:** Anthropic (backwards compatibility)
- **Factory Location:** `internal/ai/agent/factory.go` (co-located with interface)
- **Ollama Endpoint:** Use `/api/generate` (simpler than chat)
- **Error Strategy:** Fail fast, no automatic fallback (explicit is better)

### Assumptions
- Ollama server runs on localhost (can be overridden)
- Users will manually pull models before running
- JSON output format works across all models (may need prompt tuning)
- No streaming required initially (can add later)

### Open Questions
1. Should we validate model exists on startup?
2. Should we provide model aliases (e.g., "qwen-small" → "qwen2.5-coder:7b")?
3. Should we support OpenAI-compatible endpoints (LM Studio, etc.)?
4. Should we add request timeouts per provider?

---

## Success Criteria

- ✅ Can switch between Anthropic and Ollama via config
- ✅ Ollama generates valid code changes
- ✅ Cost tracking works correctly (zero for local)
- ✅ Existing Anthropic configs still work
- ✅ Clear setup instructions for Ollama
- ✅ Tests pass for both providers
- ✅ No breaking changes to existing code

---

## File Changes Summary

### New Files
- `internal/ai/agent/factory.go`
- `internal/ai/agent/ollama/client.go`
- `internal/ai/agent/ollama/types.go`
- `internal/ai/agent/ollama/client_test.go`
- `docs/OLLAMA_SETUP.md`

### Modified Files
- `internal/config/config.go`
- `internal/ai/cost.go`
- `cmd/agent/main.go`
- `.env.example`
- `README.md`
- `CLAUDE.md`

---

*Document Version: 1.0*
*Last Updated: 2025-11-12*
*Status: Ready for Implementation*
