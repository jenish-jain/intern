package jiraraw

import (
	"fmt"
	"intern/internal/ticketing"
)

// NewRawClient creates a new JIRA raw client with the same interface as the original client
// This function provides a drop-in replacement for the go-jira library client
func NewRawClient(jiraURL, email, apiToken string) (ticketing.Client, error) {
	config := ClientConfig{
		BaseURL:  jiraURL,
		Email:    email,
		APIToken: apiToken,
	}

	client, err := NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create JIRA raw client: %w", err)
	}

	return client, nil
}
