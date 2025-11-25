package ollama

// GenerateRequest represents a request to the Ollama /api/generate endpoint.
// This endpoint is used for completion-style requests where we provide a prompt
// and the model generates a response.
type GenerateRequest struct {
	Model  string                 `json:"model"`            // Model name (e.g., "qwen2.5-coder:7b")
	Prompt string                 `json:"prompt"`           // The prompt to send to the model
	Stream bool                   `json:"stream"`           // Whether to stream the response
	Format string                 `json:"format,omitempty"` // Response format, "json" for structured output
	Options map[string]interface{} `json:"options,omitempty"` // Model-specific options (temperature, etc.)
}

// GenerateResponse represents the response from the Ollama /api/generate endpoint.
// When stream=false, we get a single response with the complete generated text.
type GenerateResponse struct {
	Model     string `json:"model"`      // Model that generated the response
	CreatedAt string `json:"created_at"` // Timestamp of response creation
	Response  string `json:"response"`   // The generated text
	Done      bool   `json:"done"`       // Whether generation is complete

	// Token usage information (only present when done=true)
	Context          []int `json:"context,omitempty"`           // Context for continuing generation
	TotalDuration    int64 `json:"total_duration,omitempty"`    // Total duration in nanoseconds
	LoadDuration     int64 `json:"load_duration,omitempty"`     // Model load duration in nanoseconds
	PromptEvalCount  int   `json:"prompt_eval_count,omitempty"` // Number of tokens in prompt
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"` // Prompt evaluation duration in nanoseconds
	EvalCount        int   `json:"eval_count,omitempty"`        // Number of tokens generated
	EvalDuration     int64 `json:"eval_duration,omitempty"`     // Generation duration in nanoseconds
}

// ErrorResponse represents an error response from the Ollama API.
type ErrorResponse struct {
	Error string `json:"error"`
}
