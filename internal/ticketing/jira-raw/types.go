package jiraraw

import (
	"intern/internal/ticketing"
	"strings"
)

// JIRA API Response Types

// MyselfResponse represents the response from /rest/api/3/myself
type MyselfResponse struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Self         string `json:"self"`
}

// SearchRequest represents the request body for issue search
type SearchRequest struct {
	JQL        string   `json:"jql"`
	StartAt    int      `json:"startAt"`
	MaxResults int      `json:"maxResults"`
	Fields     []string `json:"fields"`
}

// SearchResponse represents the response from /rest/api/3/search
type SearchResponse struct {
	Expand     string  `json:"expand"`
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// Issue represents a JIRA issue
type Issue struct {
	Expand string `json:"expand"`
	ID     string `json:"id"`
	Self   string `json:"self"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string      `json:"summary"`
		Description interface{} `json:"description"` // Can be string or Atlassian Document Format object
		Status      struct {
			Self           string `json:"self"`
			Description    string `json:"description"`
			IconURL        string `json:"iconUrl"`
			Name           string `json:"name"`
			ID             string `json:"id"`
			StatusCategory struct {
				Self      string `json:"self"`
				ID        int    `json:"id"`
				Key       string `json:"key"`
				ColorName string `json:"colorName"`
				Name      string `json:"name"`
			} `json:"statusCategory"`
		} `json:"status"`
		Priority struct {
			Self    string `json:"self"`
			IconURL string `json:"iconUrl"`
			Name    string `json:"name"`
			ID      string `json:"id"`
		} `json:"priority"`
		Assignee *User `json:"assignee"`
		Reporter *User `json:"reporter"`
	} `json:"fields"`
}

// User represents a JIRA user
type User struct {
	Self         string `json:"self"`
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Name         string `json:"name,omitempty"`
	Key          string `json:"key,omitempty"`
}

// Atlassian Document Format types for description parsing
type Document struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	Content []Content `json:"content"`
}

type Content struct {
	Type    string      `json:"type"`
	Content []TextBlock `json:"content,omitempty"`
	Text    string      `json:"text,omitempty"`
}

type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TransitionRequest represents the request body for issue transition
type TransitionRequest struct {
	Transition struct {
		ID string `json:"id"`
	} `json:"transition"`
}

// TransitionResponse represents the response from transition endpoint
type TransitionResponse struct {
	// Empty response for successful transitions
}

// ErrorResponse represents JIRA API error response
type ErrorResponse struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors"`
}

// extractTextFromDescription extracts plain text from JIRA description field
// which can be a string or Atlassian Document Format object
func extractTextFromDescription(desc interface{}) string {
	if desc == nil {
		return ""
	}

	// Handle string description
	if descStr, ok := desc.(string); ok {
		return descStr
	}

	// Handle Atlassian Document Format
	if descMap, ok := desc.(map[string]interface{}); ok {
		return extractTextFromDocument(descMap)
	}

	// Log unexpected description format for debugging
	// This will help identify if there are other description formats we need to handle
	return ""
}

// extractTextFromDocument recursively extracts text from Atlassian Document Format
func extractTextFromDocument(doc map[string]interface{}) string {
	var result strings.Builder

	// Check if this is a text node
	if text, ok := doc["text"].(string); ok {
		result.WriteString(text)
	}

	// Process content array
	if content, ok := doc["content"].([]interface{}); ok {
		for _, item := range content {
			if itemMap, ok := item.(map[string]interface{}); ok {
				result.WriteString(extractTextFromDocument(itemMap))
			}
		}
	}

	return result.String()
}

// ToTicket converts a JIRA Issue to ticketing.Ticket
func (i *Issue) ToTicket() ticketing.Ticket {
	ticket := ticketing.Ticket{
		ID:          i.ID,
		Key:         i.Key,
		Summary:     i.Fields.Summary,
		Description: extractTextFromDescription(i.Fields.Description),
		Status:      i.Fields.Status.Name,
		Priority:    i.Fields.Priority.Name,
		URL:         i.Self,
	}

	// Handle assignee
	if i.Fields.Assignee != nil {
		ticket.Assignee = getUserName(i.Fields.Assignee)
	}

	// Handle reporter
	if i.Fields.Reporter != nil {
		ticket.Reporter = getUserName(i.Fields.Reporter)
	}

	return ticket
}

// getUserName extracts display name from user, falling back to name or key
func getUserName(user *User) string {
	if user == nil {
		return ""
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Name != "" {
		return user.Name
	}
	return user.Key
}
