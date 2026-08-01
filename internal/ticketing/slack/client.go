package slack

import (
	"context"
	"fmt"
	"sync"

	"intern/internal/ticketing"

	slackapi "github.com/slack-go/slack"
)

// Client implements ticketing.Client backed by Slack. Tickets here are
// ad-hoc "asks" created from incoming Slack messages rather than pulled
// from a backlog, so GetTickets is a no-op — each Slack event is processed
// directly via Coordinator.ProcessTicket by Handler (see handler.go)
// instead of going through Run()'s polling loop.
type Client struct {
	api *slackapi.Client

	mu      sync.Mutex
	threads map[string]ThreadRef // ticket key -> originating Slack thread
}

// New creates a Slack-backed ticketing client using a bot token
// (xoxb-...) with chat:write and app_mentions:read/im:history scopes.
func New(botToken string) *Client {
	return &Client{
		api:     slackapi.New(botToken),
		threads: make(map[string]ThreadRef),
	}
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if _, err := c.api.AuthTestContext(ctx); err != nil {
		return fmt.Errorf("slack auth test failed: %w", err)
	}
	return nil
}

// GetTickets always returns an empty list: Slack pushes work via webhook
// events rather than exposing a pollable backlog.
func (c *Client) GetTickets(ctx context.Context, assignee, project string) ([]ticketing.Ticket, error) {
	return nil, nil
}

// UpdateTicketStatus posts a short status line into the Slack thread that
// originated the ticket, if one is registered. It's a no-op otherwise
// (e.g. called for a ticket that wasn't created via Slack).
func (c *Client) UpdateTicketStatus(ctx context.Context, ticketKey, status string, transitions map[string]string) error {
	ref, ok := c.ThreadFor(ticketKey)
	if !ok {
		return nil
	}
	_, _, err := c.api.PostMessageContext(ctx, ref.Channel,
		slackapi.MsgOptionText(fmt.Sprintf("Status: *%s*", status), false),
		slackapi.MsgOptionTS(ref.ThreadTS),
	)
	return err
}

// RegisterThread associates a ticket key with the Slack thread that
// created it, so later status/result replies land in the right place.
func (c *Client) RegisterThread(ticketKey, channel, threadTS string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.threads[ticketKey] = ThreadRef{Channel: channel, ThreadTS: threadTS}
}

func (c *Client) ThreadFor(ticketKey string) (ThreadRef, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref, ok := c.threads[ticketKey]
	return ref, ok
}

// PostReply sends a plain message into the thread for a ticket. Used by
// Handler for the immediate ack and final result (PR URL / error), which
// carry information UpdateTicketStatus's signature has no room for.
func (c *Client) PostReply(ctx context.Context, ticketKey, text string) error {
	ref, ok := c.ThreadFor(ticketKey)
	if !ok {
		return fmt.Errorf("no registered thread for ticket %s", ticketKey)
	}
	_, _, err := c.api.PostMessageContext(ctx, ref.Channel,
		slackapi.MsgOptionText(text, false),
		slackapi.MsgOptionTS(ref.ThreadTS),
	)
	return err
}
