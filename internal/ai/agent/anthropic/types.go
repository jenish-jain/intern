package anthropic

type codeGenRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []messagePart `json:"messages"`
}

type messagePart struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type codeGenResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"` // Why the model stopped (e.g., "end_turn", "max_tokens")
}
