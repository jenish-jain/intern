package jiraraw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"intern/internal/ticketing"
)

// issueFields is the field list requested whenever issues are read.
const issueFields = "id,key,summary,description,status,priority,assignee,reporter,issuelinks"

type transitionsResponse struct {
	Transitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		To   struct {
			Name string `json:"name"`
		} `json:"to"`
	} `json:"transitions"`
}

// findTransitionID returns the ID of the transition available on the
// ticket that leads to status. It matches the target status name first,
// then the transition's own name, both case-insensitively.
func (c *client) findTransitionID(ctx context.Context, ticketKey, status string) (string, error) {
	endpoint := fmt.Sprintf("/rest/api/3/issue/%s/transitions", url.PathEscape(ticketKey))
	resp, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("list transitions for %s: %w", ticketKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.handleErrorResponse(resp)
	}

	var tr transitionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode transitions for %s: %w", ticketKey, err)
	}
	for _, t := range tr.Transitions {
		if strings.EqualFold(t.To.Name, status) {
			return t.ID, nil
		}
	}
	available := make([]string, 0, len(tr.Transitions))
	for _, t := range tr.Transitions {
		if strings.EqualFold(t.Name, status) {
			return t.ID, nil
		}
		available = append(available, t.To.Name)
	}
	return "", fmt.Errorf("no transition to status %q available for %s (available: %s)",
		status, ticketKey, strings.Join(available, ", "))
}

// GetTicket fetches a single issue by key.
func (c *client) GetTicket(ctx context.Context, key string) (ticketing.Ticket, error) {
	endpoint := fmt.Sprintf("/rest/api/3/issue/%s?fields=%s", url.PathEscape(key), issueFields)
	resp, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return ticketing.Ticket{}, fmt.Errorf("get issue %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ticketing.Ticket{}, c.handleErrorResponse(resp)
	}
	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return ticketing.Ticket{}, fmt.Errorf("decode issue %s: %w", key, err)
	}
	return issue.ToTicket(), nil
}

// AddComment posts a plain-text comment on the issue.
func (c *client) AddComment(ctx context.Context, key, body string) error {
	endpoint := fmt.Sprintf("/rest/api/3/issue/%s/comment", url.PathEscape(key))
	resp, err := c.makeRequest(ctx, "POST", endpoint, map[string]any{"body": textToADF(body)})
	if err != nil {
		return fmt.Errorf("comment on %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return c.handleErrorResponse(resp)
	}
	return nil
}

// CreateTicket creates an issue and returns its key.
func (c *client) CreateTicket(ctx context.Context, t ticketing.NewTicket) (string, error) {
	issueType := t.IssueType
	if issueType == "" {
		issueType = "Task"
	}
	fields := map[string]any{
		"project":     map[string]string{"key": t.Project},
		"summary":     t.Summary,
		"description": textToADF(t.Description),
		"issuetype":   map[string]string{"name": issueType},
	}
	if len(t.Labels) > 0 {
		fields["labels"] = t.Labels
	}
	if t.AssigneeID != "" {
		fields["assignee"] = map[string]string{"accountId": t.AssigneeID}
	}
	resp, err := c.makeRequest(ctx, "POST", "/rest/api/3/issue", map[string]any{"fields": fields})
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", c.handleErrorResponse(resp)
	}
	var created struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode created issue: %w", err)
	}
	return created.Key, nil
}

// LinkBlocks creates a "Blocks" link so that blockerKey blocks blockedKey.
// In JIRA's issueLink API the outward issue carries the outward
// description ("blocks") and the inward issue the inward one ("is blocked
// by").
func (c *client) LinkBlocks(ctx context.Context, blockerKey, blockedKey string) error {
	body := map[string]any{
		"type":         map[string]string{"name": "Blocks"},
		"outwardIssue": map[string]string{"key": blockerKey},
		"inwardIssue":  map[string]string{"key": blockedKey},
	}
	resp, err := c.makeRequest(ctx, "POST", "/rest/api/3/issueLink", body)
	if err != nil {
		return fmt.Errorf("link %s blocks %s: %w", blockerKey, blockedKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return c.handleErrorResponse(resp)
	}
	return nil
}

// textToADF converts plain text into an Atlassian Document Format doc, as
// required by the v3 API for descriptions and comments. Blank lines split
// paragraphs; single newlines become hard breaks.
func textToADF(text string) map[string]any {
	var paragraphs []any
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.Trim(para, "\n")
		if para == "" {
			continue
		}
		var content []any
		for i, line := range strings.Split(para, "\n") {
			if i > 0 {
				content = append(content, map[string]any{"type": "hardBreak"})
			}
			if line != "" {
				content = append(content, map[string]any{"type": "text", "text": line})
			}
		}
		paragraphs = append(paragraphs, map[string]any{"type": "paragraph", "content": content})
	}
	if paragraphs == nil {
		paragraphs = []any{}
	}
	return map[string]any{"type": "doc", "version": 1, "content": paragraphs}
}

// FindUserID returns the accountId of the single user matching query (a
// display name, email or accountId).
func (c *client) FindUserID(ctx context.Context, query string) (string, error) {
	endpoint := "/rest/api/3/user/search?query=" + url.QueryEscape(query)
	resp, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("search users: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.handleErrorResponse(resp)
	}
	var users []struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", fmt.Errorf("decode users: %w", err)
	}
	for _, u := range users {
		if u.AccountID == query || strings.EqualFold(u.DisplayName, query) || strings.EqualFold(u.EmailAddress, query) {
			return u.AccountID, nil
		}
	}
	if len(users) == 1 {
		return users[0].AccountID, nil
	}
	return "", fmt.Errorf("no single JIRA user matches %q (%d candidates)", query, len(users))
}
