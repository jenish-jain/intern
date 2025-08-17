package jira

import (
	"context"
	"fmt"
	"intern/internal/ticketing"

	"github.com/andygrunwald/go-jira"
	"github.com/jenish-jain/logger"
)

type Client interface {
	HealthCheck(ctx context.Context) error
	// TicketingClient methods
	GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error)
	UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error
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

func (c *client) GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error) {
	jql := fmt.Sprintf("assignee = '%s' AND project = '%s' AND statusCategory = 'To Do' ORDER BY priority ASC", assignee, project)
	logger.Debug("fetching tickets from JIRA", "query", jql)
	issues, _, err := c.jiraClient.Issue.SearchWithContext(ctx, jql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JIRA tickets: %w", err)
	}
	var tickets []ticketing.Ticket
	for _, issue := range issues {
		tickets = append(tickets, ticketing.Ticket{
			ID:          issue.ID,
			Key:         issue.Key,
			Summary:     issue.Fields.Summary,
			Description: issue.Fields.Description,
			Status:      issue.Fields.Status.Name,
			Priority:    issue.Fields.Priority.Name,
			Assignee:    getUserName(issue.Fields.Assignee),
			Reporter:    getUserName(issue.Fields.Reporter),
			URL:         issue.Self,
		})
	}
	logger.Debug("fetched tickets from JIRA", "tickets", tickets)
	return tickets, nil
}

func (c *client) UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error {
	transitionID, ok := transitions[status]
	if !ok {
		return fmt.Errorf("no transition ID found for status: %s", status)
	}
	_, err := c.jiraClient.Issue.DoTransitionWithContext(ctx, ticketKey, transitionID)
	if err != nil {
		return fmt.Errorf("failed to transition ticket %s to %s: %w", ticketKey, status, err)
	}
	return nil
}

func getUserName(user *jira.User) string {
	if user == nil {
		return ""
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return user.Name
}
