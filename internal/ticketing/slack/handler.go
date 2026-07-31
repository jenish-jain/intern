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

	"intern/internal/orchestrator"

	logger "github.com/jenish-jain/logger"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// eventDedupeWindow bounds how long a Slack event_id is remembered for
// retry de-duplication; Slack stops retrying well before this expires.
const eventDedupeWindow = 10 * time.Minute

var mentionPrefix = regexp.MustCompile(`^\s*<@[A-Z0-9]+>\s*`)

// Handler serves Slack's Events API webhook (POST /slack/events) and turns
// incoming messages into single-ticket runs on the coordinator. This is the
// request-driven entrypoint the Cloud Run deployment triggers on.
type Handler struct {
	client        *Client
	signingSecret string
	coordinator   *orchestrator.Coordinator

	mu   sync.Mutex
	seen map[string]time.Time // event_id -> received time, dedupes Slack retries
}

func NewHandler(client *Client, signingSecret string, coordinator *orchestrator.Coordinator) *Handler {
	return &Handler{
		client:        client,
		signingSecret: signingSecret,
		coordinator:   coordinator,
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

func (h *Handler) handleCallback(ctx context.Context, event slackevents.EventsAPIEvent) {
	var channel, threadTS, text string

	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		channel = ev.Channel
		threadTS = firstNonEmpty(ev.ThreadTimeStamp, ev.TimeStamp)
		text = ev.Text
	case *slackevents.MessageEvent:
		// Only handle plain direct messages to the bot; ignore channel
		// chatter, the bot's own messages, and edit/delete subtypes.
		if ev.ChannelType != "im" || ev.BotID != "" || ev.SubType != "" {
			return
		}
		channel = ev.Channel
		threadTS = firstNonEmpty(ev.ThreadTimeStamp, ev.TimeStamp)
		text = ev.Text
	default:
		return
	}

	ask := strings.TrimSpace(mentionPrefix.ReplaceAllString(text, ""))
	key := ticketKeyFor(channel, threadTS)
	h.client.RegisterThread(key, channel, threadTS)

	if ask == "" {
		_ = h.client.PostReply(ctx, key, "I need a description of what to do — try `@intern <describe the task>`.")
		return
	}

	summary := summarize(ask)
	_ = h.client.PostReply(ctx, key, fmt.Sprintf("On it — working on: %s", summary))

	if err := h.coordinator.PrepareRepository(ctx); err != nil {
		logger.Error("Slack: repository preparation failed", "error", err)
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Couldn't prepare the repository: %v", err))
		return
	}

	if err := h.coordinator.ProcessTicket(ctx, key, summary, ask); err != nil {
		logger.Error("Slack: ticket processing failed", "key", key, "error", err)
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Failed: %v", err))
		return
	}

	if entry, ok := h.coordinator.Journal.Find(key); ok && entry.PRURL != "" {
		_ = h.client.PostReply(ctx, key, fmt.Sprintf("Done — opened %s", entry.PRURL))
	} else {
		_ = h.client.PostReply(ctx, key, "Done.")
	}
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
