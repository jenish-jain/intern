package agent

import (
	"regexp"
	"strings"
)

func SanitizeResponse(s string) string {
	// sanitizeResponse tries to strip code fences and extract the JSON array
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	// Extract first JSON array if extra text present
	re := regexp.MustCompile(`(?s)\[.*\]`)
	if m := re.FindString(s); m != "" {
		return m
	}
	return s
}
