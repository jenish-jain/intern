package jira

import (
	"context"
	"fmt"

	"github.com/andygrunwald/go-jira"
	logger "github.com/jenish-jain/logger"
)

func GetAssignedTickets(ctx context.Context, c Client, assignee, project string) ([]Ticket, error) {
	jql := fmt.Sprintf("assignee = '%s' AND project = '%s' AND statusCategory = 'To Do' ORDER BY priority ASC", assignee, project)
	logger.Info("jql: %s", jql)
	issues, _, err := c.Raw().Issue.SearchWithContext(ctx, jql, nil)
	logger.Info("issues: %v", issues)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JIRA tickets: %w", err)
	}
	var tickets []Ticket
	for _, issue := range issues {
		tickets = append(tickets, parseJiraIssue(issue))
	}
	return tickets, nil
}

// UpdateTicketStatus updates the status of a JIRA ticket using a config-based transition mapping.
func UpdateTicketStatus(ctx context.Context, c Client, ticketKey, status string, transitions map[string]string) error {
	transitionID, ok := transitions[status]
	if !ok {
		return fmt.Errorf("no transition ID found for status: %s", status)
	}
	jiraClient := c.Raw()
	_, err := jiraClient.Issue.DoTransitionWithContext(ctx, ticketKey, transitionID)
	if err != nil {
		return fmt.Errorf("failed to transition ticket %s to %s: %w", ticketKey, status, err)
	}
	return nil
}

func parseJiraIssue(issue jira.Issue) Ticket {
	return Ticket{
		ID:          issue.ID,
		Key:         issue.Key,
		Summary:     issue.Fields.Summary,
		Description: issue.Fields.Description,
		Status:      issue.Fields.Status.Name,
		Priority:    issue.Fields.Priority.Name,
		Assignee:    getUserName(issue.Fields.Assignee),
		Reporter:    getUserName(issue.Fields.Reporter),
		URL:         issue.Self,
	}
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
