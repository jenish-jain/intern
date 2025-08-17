package ai

import (
	"fmt"
)

func BuildCodeGenPrompt(taskDescription, context string) string {
	return fmt.Sprintf("You are an expert Go developer. Task: %s\nContext: %s\nGenerate idiomatic, production-ready Go code.", taskDescription, context)
}

func ParseAIResponse(response string) string {
	// Placeholder: in real use, parse/clean up AI output
	return response
}
