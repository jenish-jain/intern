package ai

import (
	"testing"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name         string
		inputTokens  int
		outputTokens int
		pricing      *PricingModel
		expectedCost float64
	}{
		{
			name:         "typical ticket with default pricing",
			inputTokens:  5000,
			outputTokens: 2000,
			pricing:      nil, // Use default
			expectedCost: 0.045, // (5000/1M * $3) + (2000/1M * $15) = $0.015 + $0.030 = $0.045
		},
		{
			name:         "zero tokens",
			inputTokens:  0,
			outputTokens: 0,
			pricing:      nil,
			expectedCost: 0.0,
		},
		{
			name:         "only input tokens",
			inputTokens:  10000,
			outputTokens: 0,
			pricing:      nil,
			expectedCost: 0.030, // 10000/1M * $3 = $0.030
		},
		{
			name:         "only output tokens",
			inputTokens:  0,
			outputTokens: 5000,
			pricing:      nil,
			expectedCost: 0.075, // 5000/1M * $15 = $0.075
		},
		{
			name:         "large usage",
			inputTokens:  125000,
			outputTokens: 45000,
			pricing:      nil,
			expectedCost: 1.050, // (125000/1M * $3) + (45000/1M * $15) = $0.375 + $0.675 = $1.050
		},
		{
			name:         "custom pricing model",
			inputTokens:  10000,
			outputTokens: 5000,
			pricing: &PricingModel{
				Name:                  "custom-model",
				InputPricePerMillion:  2.0,
				OutputPricePerMillion: 10.0,
			},
			expectedCost: 0.070, // (10000/1M * $2) + (5000/1M * $10) = $0.020 + $0.050 = $0.070
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.inputTokens, tt.outputTokens, tt.pricing)
			// Allow small floating point differences
			if diff := got - tt.expectedCost; diff < -0.0001 || diff > 0.0001 {
				t.Errorf("CalculateCost(%d, %d) = %.6f, expected %.6f",
					tt.inputTokens, tt.outputTokens, got, tt.expectedCost)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name     string
		cost     float64
		expected string
	}{
		{
			name:     "small cost",
			cost:     0.045,
			expected: "$0.045",
		},
		{
			name:     "zero cost",
			cost:     0.0,
			expected: "$0.000",
		},
		{
			name:     "medium cost",
			cost:     1.234,
			expected: "$1.234",
		},
		{
			name:     "large cost",
			cost:     12.567,
			expected: "$12.567",
		},
		{
			name:     "very small cost",
			cost:     0.001,
			expected: "$0.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCost(tt.cost)
			if got != tt.expected {
				t.Errorf("FormatCost(%.3f) = %q, expected %q", tt.cost, got, tt.expected)
			}
		})
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   int
		expected string
	}{
		{
			name:     "small number",
			tokens:   5,
			expected: "5",
		},
		{
			name:     "hundreds",
			tokens:   123,
			expected: "123",
		},
		{
			name:     "thousands",
			tokens:   1234,
			expected: "1,234",
		},
		{
			name:     "ten thousands",
			tokens:   12345,
			expected: "12,345",
		},
		{
			name:     "hundred thousands",
			tokens:   125430,
			expected: "125,430",
		},
		{
			name:     "millions",
			tokens:   1234567,
			expected: "1,234,567",
		},
		{
			name:     "exact thousand",
			tokens:   1000,
			expected: "1,000",
		},
		{
			name:     "zero",
			tokens:   0,
			expected: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTokens(tt.tokens)
			if got != tt.expected {
				t.Errorf("FormatTokens(%d) = %q, expected %q", tt.tokens, got, tt.expected)
			}
		})
	}
}

func TestEstimateTokensFromText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello",
			expected: 1, // 5 chars / 4 = 1.25 -> 1
		},
		{
			name:     "medium text",
			text:     "This is a test sentence.",
			expected: 6, // 24 chars / 4 = 6
		},
		{
			name:     "typical code snippet",
			text:     "func main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			expected: 11, // 45 chars / 4 = 11.25 -> 11
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokensFromText(tt.text)
			if got != tt.expected {
				t.Errorf("EstimateTokensFromText(%q) = %d, expected %d (text length: %d)",
					tt.text, got, tt.expected, len(tt.text))
			}
		})
	}
}

func TestPricingModels(t *testing.T) {
	t.Run("ClaudeSonnet4 has correct values", func(t *testing.T) {
		if ClaudeSonnet4.Name != "claude-sonnet-4-20250514" {
			t.Errorf("ClaudeSonnet4.Name = %q, expected %q",
				ClaudeSonnet4.Name, "claude-sonnet-4-20250514")
		}
		if ClaudeSonnet4.InputPricePerMillion != 3.00 {
			t.Errorf("ClaudeSonnet4.InputPricePerMillion = %.2f, expected 3.00",
				ClaudeSonnet4.InputPricePerMillion)
		}
		if ClaudeSonnet4.OutputPricePerMillion != 15.00 {
			t.Errorf("ClaudeSonnet4.OutputPricePerMillion = %.2f, expected 15.00",
				ClaudeSonnet4.OutputPricePerMillion)
		}
	})

	t.Run("DefaultPricing is ClaudeSonnet4", func(t *testing.T) {
		if DefaultPricing.Name != ClaudeSonnet4.Name {
			t.Errorf("DefaultPricing should be ClaudeSonnet4")
		}
	})
}
