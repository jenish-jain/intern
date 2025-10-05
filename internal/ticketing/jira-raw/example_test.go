package jiraraw_test

import (
	"context"
	"fmt"
	"intern/internal/ticketing"
	jiraraw "intern/internal/ticketing/jira-raw"
	"os"
	"time"
)

// Example demonstrates how to use the JIRA raw client as a drop-in replacement
func ExampleNewRawClient() {
	// Example configuration - replace with your actual JIRA credentials
	jiraURL := "https://your-domain.atlassian.net"
	email := "your-email@example.com"
	apiToken := "your-api-token"

	// Create the raw client (same interface as the original go-jira client)
	client, err := jiraraw.NewRawClient(jiraURL, email, apiToken)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	// Use with the existing ticketing service (no changes needed!)
	service := ticketing.NewTicketingService(client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Health check
	if err := client.HealthCheck(ctx); err != nil {
		fmt.Printf("Health check failed: %v\n", err)
		return
	}
	fmt.Println("Health check passed!")

	// Get tickets
	tickets, err := service.GetTickets(ctx, "assignee@example.com", "PROJECT")
	if err != nil {
		fmt.Printf("Failed to get tickets: %v\n", err)
		return
	}
	fmt.Printf("Found %d tickets\n", len(tickets))

	// Update ticket status (example transitions - replace with actual IDs)
	transitions := map[string]string{
		"In Progress": "11", // Replace with actual transition ID
		"Done":        "21", // Replace with actual transition ID
	}

	if len(tickets) > 0 {
		ticketKey := tickets[0].Key
		err = service.UpdateTicketStatus(ctx, ticketKey, "In Progress", transitions)
		if err != nil {
			fmt.Printf("Failed to update ticket %s: %v\n", ticketKey, err)
			return
		}
		fmt.Printf("Successfully updated ticket %s to In Progress\n", ticketKey)
	}
}

// ExampleWithConfig demonstrates advanced configuration
func ExampleWithConfig() {
	config := jiraraw.ClientConfig{
		BaseURL:  "https://your-domain.atlassian.net",
		Email:    "your-email@example.com",
		APIToken: "your-api-token",
		Timeout:  60 * time.Second, // Custom timeout
	}

	client, err := jiraraw.NewClient(config)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	ctx := context.Background()
	if err := client.HealthCheck(ctx); err != nil {
		fmt.Printf("Health check failed: %v\n", err)
		return
	}

	fmt.Println("Client created successfully with custom configuration!")
}

// ExampleMigration shows how to migrate from the original go-jira client
func ExampleMigration() {
	// Before (with go-jira library):
	// tp := jira.BasicAuthTransport{
	//     Username: "your-email@example.com",
	//     Password: "your-api-token",
	// }
	// client, err := jira.NewClient(tp.Client(), "https://your-domain.atlassian.net")

	// After (with jira-raw):
	jiraURL := "https://your-domain.atlassian.net"
	email := "your-email@example.com"
	apiToken := "your-api-token"

	client, err := jiraraw.NewRawClient(jiraURL, email, apiToken)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	// The rest of your code remains exactly the same!
	service := ticketing.NewTicketingService(client)

	ctx := context.Background()
	tickets, err := service.GetTickets(ctx, "assignee@example.com", "PROJECT")
	if err != nil {
		fmt.Printf("Failed to get tickets: %v\n", err)
		return
	}

	fmt.Printf("Migration successful! Found %d tickets\n", len(tickets))
}

// ExampleRealWorldUsage demonstrates real-world usage patterns
func ExampleRealWorldUsage() {
	// Get credentials from environment variables (recommended)
	jiraURL := os.Getenv("JIRA_URL")
	email := os.Getenv("JIRA_EMAIL")
	apiToken := os.Getenv("JIRA_API_TOKEN")

	if jiraURL == "" || email == "" || apiToken == "" {
		fmt.Println("Please set JIRA_URL, JIRA_EMAIL, and JIRA_API_TOKEN environment variables")
		return
	}

	client, err := jiraraw.NewRawClient(jiraURL, email, apiToken)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	service := ticketing.NewTicketingService(client)
	ctx := context.Background()

	// Health check first
	if err := client.HealthCheck(ctx); err != nil {
		fmt.Printf("JIRA connection failed: %v\n", err)
		return
	}

	// Get tickets for a specific user and project
	assignee := "user@example.com"
	project := "PROJ"

	tickets, err := service.GetTickets(ctx, assignee, project)
	if err != nil {
		fmt.Printf("Failed to get tickets: %v\n", err)
		return
	}

	fmt.Printf("Found %d tickets for %s in project %s\n", len(tickets), assignee, project)

	// Process each ticket
	for _, ticket := range tickets {
		fmt.Printf("Ticket %s: %s (Status: %s, Priority: %s)\n",
			ticket.Key, ticket.Summary, ticket.Status, ticket.Priority)
	}

	// Example: Move first ticket to "In Progress" if we have transitions configured
	// Note: You need to get actual transition IDs from your JIRA instance
	transitions := map[string]string{
		"In Progress": "11", // Replace with actual transition ID
		"Done":        "21", // Replace with actual transition ID
	}

	if len(tickets) > 0 {
		ticketKey := tickets[0].Key
		err = service.UpdateTicketStatus(ctx, ticketKey, "In Progress", transitions)
		if err != nil {
			fmt.Printf("Failed to update ticket %s: %v\n", ticketKey, err)
		} else {
			fmt.Printf("Successfully moved ticket %s to In Progress\n", ticketKey)
		}
	}
}
