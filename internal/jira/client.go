package jira

import (
	"context"
	"fmt"

	"github.com/andygrunwald/go-jira"
)

type Client interface {
	HealthCheck(ctx context.Context) error
	Raw() *jira.Client
}

type client struct {
	jiraClient *jira.Client
}

func NewClient(jiraURL, email, apiToken string) (Client, error) {
	tp := jira.BasicAuthTransport{
		Username: email,
		Password: apiToken,
	}
	c, err := jira.NewClient(tp.Client(), jiraURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create JIRA client: %w", err)
	}
	return &client{jiraClient: c}, nil
}

func (c *client) HealthCheck(ctx context.Context) error {
	me, _, err := c.jiraClient.User.GetSelfWithContext(ctx)
	if err != nil {
		return fmt.Errorf("JIRA health check failed: %w", err)
	}
	if me == nil || me.EmailAddress == "" {
		return fmt.Errorf("JIRA health check: user info missing")
	}
	return nil
}

func (c *client) Raw() *jira.Client {
	return c.jiraClient
}
