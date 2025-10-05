# JIRA Raw Client

This is a raw HTTP client implementation for JIRA API that provides an alternative to the `github.com/andygrunwald/go-jira` library. It implements the same interface as the original JIRA client but uses direct HTTP calls to the JIRA REST API.

## Features

- **Drop-in replacement** for the existing JIRA client
- **No external dependencies** on deprecated or slow-to-update libraries
- **Full JIRA REST API v3 support**
- **Proper error handling** with detailed error messages
- **Context support** for request cancellation and timeouts
- **Flexible authentication** (Basic Auth with email/token)

## Usage

### Basic Usage

```go
import (
    "context"
    "intern/internal/ticketing/jira-raw"
)

// Create a new raw client
client, err := jiraraw.NewRawClient(
    "https://your-domain.atlassian.net",
    "your-email@example.com",
    "your-api-token",
)
if err != nil {
    log.Fatal(err)
}

// Use with ticketing service
service := ticketing.NewTicketingService(client)

// Health check
ctx := context.Background()
if err := client.HealthCheck(ctx); err != nil {
    log.Fatal("Health check failed:", err)
}

// Get tickets
tickets, err := service.GetTickets(ctx, "assignee@example.com", "PROJECT")
if err != nil {
    log.Fatal("Failed to get tickets:", err)
}

// Update ticket status
transitions := map[string]string{
    "In Progress": "11",
    "Done": "21",
}
err = service.UpdateTicketStatus(ctx, "PROJECT-123", "In Progress", transitions)
if err != nil {
    log.Fatal("Failed to update ticket:", err)
}
```

### Advanced Configuration

```go
import "intern/internal/ticketing/jira-raw"

config := jiraraw.ClientConfig{
    BaseURL:  "https://your-domain.atlassian.net",
    Email:    "your-email@example.com",
    APIToken: "your-api-token",
    Timeout:  60 * time.Second, // Custom timeout
}

client, err := jiraraw.NewClient(config)
if err != nil {
    log.Fatal(err)
}
```

## API Methods

### HealthCheck(ctx context.Context) error

Verifies the connection to JIRA by fetching current user information from `/rest/api/3/myself`.

### GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error)

Retrieves tickets using JQL search via GET request to `/rest/api/3/search/jql` with query parameters. Searches for:

- Assigned to the specified user
- In the specified project  
- With "To Do" status category
- Ordered by priority

### UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error

Transitions a ticket to a new status using `/rest/api/3/issue/{key}/transitions`.

## Migration from go-jira

To migrate from the existing `github.com/andygrunwald/go-jira` client:

1. **Replace import:**

   ```go
   // Before
   import "github.com/andygrunwald/go-jira"
   
   // After  
   import "intern/internal/ticketing/jira-raw"
   ```

2. **Replace client creation:**

   ```go
   // Before
   client, err := jira.NewClient(tp.Client(), jiraURL)
   
   // After
   client, err := jiraraw.NewRawClient(jiraURL, email, apiToken)
   ```

3. **No other changes needed** - the interface is identical!

## Error Handling

The client provides detailed error messages for common JIRA API errors:

- **Authentication errors**: Invalid credentials or expired tokens
- **Permission errors**: Insufficient permissions for the operation
- **Validation errors**: Invalid JQL queries or transition IDs
- **Rate limiting**: Automatic handling of rate limit responses
- **Network errors**: Connection timeouts and network issues

## JIRA API Version

This client targets **JIRA REST API v3** for maximum compatibility and feature support.

### API Migration Notes

- **Search Endpoint**: Uses GET `/rest/api/3/search/jql` with query parameters (updated from deprecated POST `/rest/api/3/search`)
- **HTTP Method**: GET request with JQL in query parameters instead of POST with JSON body
- **Authentication**: Uses Basic Auth with email and API token
- **Error Handling**: Provides detailed error messages for deprecated API usage

## Dependencies

- `encoding/json` - JSON marshaling/unmarshaling
- `net/http` - HTTP client
- `intern/internal/ticketing` - Ticketing types and interfaces
- `github.com/jenish-jain/logger` - Logging (optional)
