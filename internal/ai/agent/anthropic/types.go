package anthropic

// codeGenRequest represents a request to the Anthropic API for code generation.
// It contains the model to use, token limits, and conversation messages.
type codeGenRequest struct {
	Model     string        `json:"model"`      // Model identifier (e.g., "claude-3-5-sonnet-20241022")
	MaxTokens int           `json:"max_tokens"` // Maximum tokens to generate in response
	Messages  []messagePart `json:"messages"`   // Conversation history and user prompts
}

// messagePart represents a single message in the conversation with the AI.
// Each message has a role (user/assistant) and text content.
type messagePart struct {
	Role    string `json:"role"`    // Role of the message sender ("user" or "assistant")
	Content string `json:"content"` // Text content of the message
}

// codeGenResponse represents the response from the Anthropic API.
// It contains the generated text and metadata about why the model stopped generating.
type codeGenResponse struct {
	Content []struct {
		Text string `json:"text"` // Generated text content
	} `json:"content"`
	StopReason string `json:"stop_reason"` // Why the model stopped (e.g., "end_turn", "max_tokens")
}
