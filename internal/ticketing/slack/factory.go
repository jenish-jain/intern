package slack

import "fmt"

// NewSlackClient mirrors the jira-raw factory pattern (see
// internal/ticketing/jira-raw/factory.go) so wiring stays consistent across
// ticketing backends.
func NewSlackClient(botToken string) (*Client, error) {
	if botToken == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	return New(botToken), nil
}
