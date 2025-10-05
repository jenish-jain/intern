package jiraraw

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"intern/internal/ticketing"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jenish-jain/logger"
)

// Client interface matches the existing JIRA client interface
type Client interface {
	HealthCheck(ctx context.Context) error
	GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error)
	UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error
}

// client implements the raw JIRA HTTP client
type client struct {
	baseURL    string
	httpClient *http.Client
	authHeader string
}

// ClientConfig holds configuration for the JIRA raw client
type ClientConfig struct {
	BaseURL  string
	Email    string
	APIToken string
	Timeout  time.Duration
}

// NewClient creates a new JIRA raw client
func NewClient(config ClientConfig) (Client, error) {
	if config.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if config.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if config.APIToken == "" {
		return nil, fmt.Errorf("API token is required")
	}

	// Ensure base URL doesn't end with slash
	baseURL := strings.TrimSuffix(config.BaseURL, "/")

	// Create auth header for basic authentication
	auth := base64.StdEncoding.EncodeToString([]byte(config.Email + ":" + config.APIToken))

	// Set default timeout if not provided
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		authHeader: "Basic " + auth,
	}, nil
}

// makeRequest performs HTTP requests to JIRA API
func (c *client) makeRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	logger.Debug("making JIRA API request", "method", method, "url", url)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// handleErrorResponse processes error responses from JIRA API
func (c *client) handleErrorResponse(resp *http.Response) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read error response body: %w", err)
	}

	var errorResp ErrorResponse
	if err := json.Unmarshal(body, &errorResp); err != nil {
		// If we can't parse as JSON error, return the raw response
		return fmt.Errorf("JIRA API error (status %d): %s", resp.StatusCode, string(body))
	}

	var errorMsg strings.Builder
	if len(errorResp.ErrorMessages) > 0 {
		errorMsg.WriteString(strings.Join(errorResp.ErrorMessages, "; "))
	}
	if len(errorResp.Errors) > 0 {
		if errorMsg.Len() > 0 {
			errorMsg.WriteString("; ")
		}
		for key, value := range errorResp.Errors {
			errorMsg.WriteString(fmt.Sprintf("%s: %s", key, value))
		}
	}

	if errorMsg.Len() == 0 {
		errorMsg.WriteString(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}

	return fmt.Errorf("JIRA API error: %s", errorMsg.String())
}

// HealthCheck verifies the connection to JIRA by getting current user info
func (c *client) HealthCheck(ctx context.Context) error {
	resp, err := c.makeRequest(ctx, "GET", "/rest/api/3/myself", nil)
	if err != nil {
		return fmt.Errorf("JIRA health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleErrorResponse(resp)
	}

	var myself MyselfResponse
	if err := json.NewDecoder(resp.Body).Decode(&myself); err != nil {
		return fmt.Errorf("failed to decode user info: %w", err)
	}

	if myself.EmailAddress == "" {
		return fmt.Errorf("JIRA health check: user email address missing")
	}

	logger.Debug("JIRA health check successful", "user", myself.EmailAddress)
	return nil
}

// GetTickets retrieves tickets from JIRA using JQL
func (c *client) GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error) {
	// Escape assignee and project for JQL
	assigneeEscaped := strings.ReplaceAll(assignee, "'", "\\'")
	projectEscaped := strings.ReplaceAll(project, "'", "\\'")

	jql := fmt.Sprintf("assignee = '%s' AND project = '%s' AND statusCategory = 'To Do' ORDER BY priority ASC",
		assigneeEscaped, projectEscaped)

	logger.Debug("fetching tickets from JIRA", "query", jql)

	// Build query parameters for GET request
	params := url.Values{}
	params.Set("jql", jql)
	params.Set("maxResults", "100")
	params.Set("fields", "id,key,summary,description,status,priority,assignee,reporter")
	params.Set("expand", "schema,names")

	endpoint := "/rest/api/3/search/jql?" + params.Encode()

	resp, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to search JIRA issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	var tickets []ticketing.Ticket
	for _, issue := range searchResp.Issues {
		tickets = append(tickets, issue.ToTicket())
		fmt.Println("Ticket", issue.ToTicket())
	}

	logger.Debug("fetched tickets from JIRA", "count", len(tickets))
	return tickets, nil
}

// UpdateTicketStatus transitions a ticket to a new status
func (c *client) UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error {
	transitionID, ok := transitions[status]
	if !ok {
		return fmt.Errorf("no transition ID found for status: %s", status)
	}

	// URL encode the ticket key to handle special characters
	ticketKeyEscaped := url.PathEscape(ticketKey)

	transitionReq := TransitionRequest{
		Transition: struct {
			ID string `json:"id"`
		}{
			ID: transitionID,
		},
	}

	endpoint := fmt.Sprintf("/rest/api/3/issue/%s/transitions", ticketKeyEscaped)
	resp, err := c.makeRequest(ctx, "POST", endpoint, transitionReq)
	if err != nil {
		return fmt.Errorf("failed to transition ticket %s: %w", ticketKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return c.handleErrorResponse(resp)
	}

	logger.Debug("successfully transitioned ticket", "ticket", ticketKey, "status", status)
	return nil
}
