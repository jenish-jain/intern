package runner

import "testing"

func TestUsageAdd(t *testing.T) {
	got := Usage{InputTokens: 1, OutputTokens: 2, CostUSD: 0.5}.Add(Usage{InputTokens: 10, OutputTokens: 20, CostUSD: 1})
	if got != (Usage{InputTokens: 11, OutputTokens: 22, CostUSD: 1.5}) {
		t.Errorf("Add = %+v", got)
	}
}
