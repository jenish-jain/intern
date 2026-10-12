package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"intern/internal/ai"
	"intern/internal/orchestrator"

	logger "github.com/jenish-jain/logger"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// eventDedupeWindow bounds how long a Slack event_id is remembered for
// retry de-duplication; Slack stops retrying well before this expires.
const eventDedupeWindow = 10 * time.Minute

var mentionPrefix = regexp.MustCompile(`^\s*<@[A-Z0-9]+>\s*`)

// Backend is what the handler needs from the orchestrator.
type Backend interface {
	PrepareRepository(ctx context.Context) error
	ProcessTicket(ctx context.Context, key, summary, description string) error
	LastTicketMetrics(key string) (*orchestrator.TicketMetrics, bool)
	PRURLFor(key string) string
	Converse(ctx context.Context, convID, message string) (string, error)
	HasConversation(convID string) bool
}

// HandlerOptions configure routing.
type HandlerOptions struct {
	// ArchitectChannel makes every thread in that channel an architect
	// conversation.
	ArchitectChannel string
}

// planPrefix starts an architect conversation from any channel or DM.
var planPrefix = regexp.MustCompile(`(?i)^\s*plan\s*:\s*`)

// maxSlackText keeps replies under Slack's message size limit.
const maxSlackText = 39000

// Handler serves Slack's Events API webhook (POST /slack/events). Messages
// become either architect conversations (planning with the product owner)
// or single-ticket developer runs.
type Handler struct {
	client        *Client
	signingSecret string
	backend       Backend
	opts          HandlerOptions

	mu   sync.Mutex
	seen map[string]time.Time // event_id -> received time, dedupes Slack retries
}

func NewHandler(client *Client, signingSecret string, backend Backend, opts HandlerOptions) *Handler {
	return &Handler{
		client:        client,
		signingSecret: signingSecret,
		backend:       backend,
		opts:          opts,
		seen:          make(map[string]time.Time),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	sv, err := slackapi.NewSecretsVerifier(r.Header, h.signingSecret)
	if err != nil {
		logger.Warn("Slack signature verifier setup failed", "error", err)
		http.Error(w, "invalid request", http.StatusUnauthorized)
		return
	}
	if _, err := sv.Write(body); err != nil {
		http.Error(w, "invalid request", http.StatusUnauthorized)
		return
	}
	if err := sv.Ensure(); err != nil {
		logger.Warn("Slack signature verification failed", "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		logger.Warn("Failed to parse Slack event", "error", err)
		http.Error(w, "bad event payload", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case slackevents.URLVerification:
		var ve slackevents.EventsAPIURLVerificationEvent
		if err := json.Unmarshal(body, &ve); err != nil {
			http.Error(w, "bad challenge payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(ve.Challenge))

	case slackevents.CallbackEvent:
		// Slack requires an ack within ~3s and retries on timeout/non-2xx.
		// Respond immediately and do the (multi-minute) ticket processing
		// in the background — this is why the Cloud Run deployment must
		// run with CPU always allocated (see docs/CLOUD_RUN_DEPLOY.md).
		w.WriteHeader(http.StatusOK)

		eventID := ""
		if cb, ok := event.Data.(*slackevents.EventsAPICallbackEvent); ok {
			eventID = cb.EventID
		}
		if !h.markSeen(eventID) {
			return // Slack redelivered an event we've already started processing
		}
		go h.handleCallback(context.Background(), event)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

// markSeen de-dupes on Slack's event_id, since Slack retries webhook
// delivery on slow acks or non-2xx responses.
func (h *Handler) markSeen(id string) bool {
	if id == "" {
		return true // no ID to dedupe on (shouldn't happen); process it
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for k, t := range h.seen {
		if now.Sub(t) > eventDedupeWindow {
			delete(h.seen, k)
		}
	}
	if _, ok := h.seen[id]; ok {
		return false
	}
	h.seen[id] = now
	return true
}

// incoming is a message addressed to the bot.
type incoming struct {
	channel, threadTS, text string
	mention                 bool // came as an app_mention
	threadReply             bool // a reply inside an existing thread
}

func (h *Handler) parse(ctx context.Context, event slackevents.EventsAPIEvent) (incoming, bool) {
	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		return incoming{channel: ev.Channel, threadTS: firstNonEmpty(ev.ThreadTimeStamp, ev.TimeStamp), text: ev.Text, mention: true, threadReply: ev.ThreadTimeStamp != ""}, true
	case *slackevents.MessageEvent:
		if ev.BotID != "" || ev.SubType != "" {
			return incoming{}, false
		}
		if ev.ChannelType == "im" {
			return incoming{channel: ev.Channel, threadTS: firstNonEmpty(ev.ThreadTimeStamp, ev.TimeStamp), text: ev.Text, threadReply: ev.ThreadTimeStamp != ""}, true
		}
		// Plain replies in a channel thread only matter when that thread is
		// an architect conversation, and mentions arrive as app_mention.
		if ev.ThreadTimeStamp == "" {
			return incoming{}, false
		}
		if bot := h.client.BotUserID(ctx); bot != "" && strings.Contains(ev.Text, "<@"+bot+">") {
			return incoming{}, false
		}
		if !h.backend.HasConversation(conversationID(ev.Channel, ev.ThreadTimeStamp)) {
			return incoming{}, false
		}
		return incoming{channel: ev.Channel, threadTS: ev.ThreadTimeStamp, text: ev.Text, threadReply: true}, true
	}
	return incoming{}, false
}

func (h *Handler) handleCallback(ctx context.Context, event slackevents.EventsAPIEvent) {
	in, ok := h.parse(ctx, event)
	if !ok {
		return
	}
	text := strings.TrimSpace(mentionPrefix.ReplaceAllString(in.text, ""))
	convID := conversationID(in.channel, in.threadTS)

	isPlan := planPrefix.MatchString(text)
	switch {
	case h.backend.HasConversation(convID):
		h.converse(ctx, in, convID, text)
	case isPlan || (h.opts.ArchitectChannel != "" && in.channel == h.opts.ArchitectChannel && !in.threadReply):
		h.converse(ctx, in, convID, planPrefix.ReplaceAllString(text, ""))
	default:
		h.developerAsk(ctx, in, text)
	}
}

// converse sends a message to the architect and posts the reply.
func (h *Handler) converse(ctx context.Context, in incoming, convID, text string) {
	h.client.RegisterThread(convID, in.channel, in.threadTS)
	if text == "" {
		_ = h.client.PostReply(ctx, convID, "Send me your product requirements (paste the PRD or describe the product) and I'll plan it.")
		return
	}
	_ = h.client.PostReply(ctx, convID, "Thinking about it…")
	reply, err := h.backend.Converse(ctx, convID, text)
	if err != nil {
		logger.Error("Slack: architect conversation failed", "conversation", convID, "error", err)
		reply = fmt.Sprintf("Something went wrong while planning: %v", err)
	}
	if len(reply) > maxSlackText {
		reply = reply[:maxSlackText] + "…"
	}
	_ = h.client.PostReply(ctx, convID, reply)
}

// developerAsk runs a one-off developer ticket from a Slack message.
func (h *Handler) developerAsk(ctx context.Context, in incoming, ask string) {
	key := ticketKeyFor(in.channel, in.threadTS)
	h.client.RegisterThread(key, in.channel, in.threadTS)

	if ask == "" {
		_ = h.client.PostReply(ctx, key, "I need a description of what to do — try `@intern <describe the task>`, or `@intern plan: <your PRD>` to plan a project.")
		return
	}

	summary := summarize(ask)
	_ = h.client.PostReply(ctx, key, fmt.Sprintf("On it — working on: %s", summary))

	if err := h.backend.PrepareRepository(ctx); err != nil {
		logger.Error("Slack: repository preparation failed", "error", err)
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Couldn't prepare the repository: %v", err))
		return
	}

	if err := h.backend.ProcessTicket(ctx, key, summary, ask); err != nil {
		logger.Error("Slack: ticket processing failed", "key", key, "error", err)
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Failed: %v%s", err, h.costSuffix(key)))
		return
	}

	if url := h.backend.PRURLFor(key); url != "" {
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Done — opened %s%s", url, h.costSuffix(key)))
	} else {
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Done.%s", h.costSuffix(key)))
	}
}

// costSuffix renders a "\n\n_cost: ..._" line from the ticket's recorded
// metrics (actual cost incurred), or "" if no metrics were recorded yet
// (e.g. processing failed before any AI call was made).
func (h *Handler) costSuffix(key string) string {
	tm, ok := h.backend.LastTicketMetrics(key)
	if !ok || (tm.InputTokens == 0 && tm.OutputTokens == 0 && tm.Cost == 0) {
		return ""
	}
	return fmt.Sprintf("\n\n_cost: %s (%s in / %s out tokens)_",
		ai.FormatCost(tm.Cost), ai.FormatTokens(tm.InputTokens), ai.FormatTokens(tm.OutputTokens))
}

// conversationID identifies an architect conversation by its thread.
func conversationID(channel, threadTS string) string {
	return "conv-" + channel + "-" + strings.ReplaceAll(threadTS, ".", "")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ticketKeyFor derives a stable, branch-name-safe ticket key from the
// originating Slack conversation so repeated events in the same thread map
// to the same ticket (and so orchestrator.State can dedupe reprocessing).
func ticketKeyFor(channel, threadTS string) string {
	sanitized := strings.NewReplacer(".", "", ":", "").Replace(threadTS)
	return fmt.Sprintf("SLACK-%s-%s", channel, sanitized)
}

func summarize(ask string) string {
	line := strings.SplitN(ask, "\n", 2)[0]
	const maxLen = 80
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen-1] + "…"
}
