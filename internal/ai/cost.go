package ai

import "fmt"

// PricingModel defines the cost structure for an AI model.
// Different providers and models have different pricing.
type PricingModel struct {
	Name              string  // Model name (e.g., "claude-sonnet-4-20250514")
	InputPricePerMillion  float64 // Cost per million input tokens in USD
	OutputPricePerMillion float64 // Cost per million output tokens in USD
}

// Pricing constants for supported models
var (
	// ClaudeSonnet4 pricing as of January 2025
	// Source: https://www.anthropic.com/pricing
	ClaudeSonnet4 = PricingModel{
		Name:                  "claude-sonnet-4-20250514",
		InputPricePerMillion:  3.00,  // $3.00 per million input tokens
		OutputPricePerMillion: 15.00, // $15.00 per million output tokens
	}

	// DefaultPricing is used when model-specific pricing is not available
	DefaultPricing = ClaudeSonnet4
)

// CalculateCost computes the total cost in USD for given token usage.
// Uses the specified pricing model, or DefaultPricing if nil.
func CalculateCost(inputTokens, outputTokens int, pricing *PricingModel) float64 {
	if pricing == nil {
		pricing = &DefaultPricing
	}

	inputCost := (float64(inputTokens) / 1_000_000) * pricing.InputPricePerMillion
	outputCost := (float64(outputTokens) / 1_000_000) * pricing.OutputPricePerMillion

	return inputCost + outputCost
}

// FormatCost formats a cost value as a USD string.
// Examples: "$0.045", "$1.23", "$0.001"
func FormatCost(cost float64) string {
	return fmt.Sprintf("$%.3f", cost)
}

// FormatTokens formats a token count with thousand separators.
// Examples: "1,234", "125,430", "5"
func FormatTokens(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}

	// Add thousand separators
	str := fmt.Sprintf("%d", tokens)
	n := len(str)
	result := ""

	for i, ch := range str {
		if i > 0 && (n-i)%3 == 0 {
			result += ","
		}
		result += string(ch)
	}

	return result
}

// EstimateTokensFromText provides a rough estimate of token count from text.
// Uses the common approximation of ~4 characters per token.
// This is a rough estimate and actual tokenization may vary.
func EstimateTokensFromText(text string) int {
	const charsPerToken = 4
	return len(text) / charsPerToken
}
