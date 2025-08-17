package orchestrator

import (
	"regexp"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9\-]+`)

func buildBranchName(prefix, ticketKey string) string {
	base := strings.ToLower(ticketKey)
	slug := strings.ReplaceAll(base, " ", "-")
	slug = nonAlnum.ReplaceAllString(slug, "")
	if len(slug) > 30 {
		slug = slug[:30]
	}
	return prefix + "-" + strings.Trim(slug, "-")
}
