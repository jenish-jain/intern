package orchestrator

import (
	"regexp"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9\-]+`)

func buildBranchName(prefix, ticketKey string) string {
	// Lowercase rather than strip: the old strip-only regex silently deleted
	// every uppercase char (most ticket keys, e.g. Slack's "SLACK-...", are
	// mostly uppercase), which could collapse distinct tickets into the same
	// slug. Collapse to "-" rather than "" for the same reason, and to avoid
	// merging adjacent tokens together.
	slug := strings.ToLower(ticketKey)
	slug = nonAlnum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 30 {
		slug = strings.Trim(slug[:30], "-")
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return slug
	}
	return prefix + "/" + slug
}
