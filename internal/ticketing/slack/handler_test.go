package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"intern/internal/orchestrator"

	logger "github.com/jenish-jain/logger"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func TestMain(m *testing.M) {
	logger.Init("error")
	os.Exit(m.Run())
}

type post struct{ channel, threadTS, text string }

// fakeSlack records chat.postMessage calls and answers auth.test.
type fakeSlack struct {
	mu    sync.Mutex
	posts []post
}

func (f *fakeSlack) handler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "auth.test"):
		_, _ = w.Write([]byte(`{"ok":true,"user_id":"UBOT"}`))
	case strings.HasSuffix(r.URL.Path, "chat.postMessage"):
		f.mu.Lock()
		f.posts = append(f.posts, post{r.Form.Get("channel"), r.Form.Get("thread_ts"), r.Form.Get("text")})
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C","ts":"1.1"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func (f *fakeSlack) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.posts {
		out = append(out, p.text)
	}
	return out
}

type fakeBackend struct {
	mu            sync.Mutex
	conversations map[string]bool
	conversed     []string // "convID: message"
	processed     []string
}

func (b *fakeBackend) PrepareRepository(context.Context) error { return nil }
func (b *fakeBackend) ProcessTicket(_ context.Context, key, summary, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.processed = append(b.processed, key+": "+summary)
	return nil
}
func (b *fakeBackend) LastTicketMetrics(string) (*orchestrator.TicketMetrics, bool) { return nil, false }
func (b *fakeBackend) PRURLFor(string) string                                       { return "https://github.com/o/r/pull/1" }
func (b *fakeBackend) Converse(_ context.Context, id, msg string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conversations[id] = true
	b.conversed = append(b.conversed, id+": "+msg)
	return "architect reply to: " + msg, nil
}
func (b *fakeBackend) HasConversation(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conversations[id]
}

func newTestHandler(t *testing.T, opts HandlerOptions) (*Handler, *fakeSlack, *fakeBackend) {
	t.Helper()
	fs := &fakeSlack{}
	srv := httptest.NewServer(http.HandlerFunc(fs.handler))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL + "/")
	client := New("xoxb-test", slackapi.OptionAPIURL(u.String()))
	be := &fakeBackend{conversations: map[string]bool{}}
	return NewHandler(client, "secret", be, opts), fs, be
}

func mention(channel, ts, threadTS, text string) slackevents.EventsAPIEvent {
	return slackevents.EventsAPIEvent{InnerEvent: slackevents.EventsAPIInnerEvent{
		Data: &slackevents.AppMentionEvent{Channel: channel, TimeStamp: ts, ThreadTimeStamp: threadTS, Text: text},
	}}
}

func message(channel, channelType, ts, threadTS, text string) slackevents.EventsAPIEvent {
	return slackevents.EventsAPIEvent{InnerEvent: slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{Channel: channel, ChannelType: channelType, TimeStamp: ts, ThreadTimeStamp: threadTS, Text: text},
	}}
}

func TestHandler_PlanPrefixStartsConversationAndRepliesContinueIt(t *testing.T) {
	h, fs, be := newTestHandler(t, HandlerOptions{})
	ctx := context.Background()

	h.handleCallback(ctx, mention("CGEN", "100.1", "", "<@UBOT> plan: a notes app"))
	if len(be.conversed) != 1 || be.conversed[0] != "conv-CGEN-1001: a notes app" {
		t.Fatalf("conversed = %v", be.conversed)
	}
	texts := fs.texts()
	if texts[0] != "Thinking about it…" || texts[1] != "architect reply to: a notes app" {
		t.Errorf("posts = %v", texts)
	}
	if fs.posts[1].threadTS != "100.1" {
		t.Errorf("reply not threaded: %+v", fs.posts[1])
	}

	// A plain reply in that thread (no mention) continues the conversation.
	h.handleCallback(ctx, message("CGEN", "channel", "101.1", "100.1", "No sharing."))
	if len(be.conversed) != 2 || be.conversed[1] != "conv-CGEN-1001: No sharing." {
		t.Errorf("thread reply not routed: %v", be.conversed)
	}
	// A reply that mentions the bot is handled by its app_mention event,
	// not twice.
	h.handleCallback(ctx, message("CGEN", "channel", "102.1", "100.1", "<@UBOT> approve"))
	if len(be.conversed) != 2 {
		t.Errorf("mention duplicated through message event: %v", be.conversed)
	}
	h.handleCallback(ctx, mention("CGEN", "102.1", "100.1", "<@UBOT> approve"))
	if be.conversed[2] != "conv-CGEN-1001: approve" {
		t.Errorf("mention in thread not routed: %v", be.conversed)
	}
	if len(be.processed) != 0 {
		t.Error("nothing should run as a developer ask")
	}
}

func TestHandler_ArchitectChannel(t *testing.T) {
	h, _, be := newTestHandler(t, HandlerOptions{ArchitectChannel: "CARCH"})
	h.handleCallback(context.Background(), mention("CARCH", "200.1", "", "<@UBOT> Build a CRM for dentists."))
	if len(be.conversed) != 1 || !strings.HasSuffix(be.conversed[0], "Build a CRM for dentists.") {
		t.Errorf("architect channel message not routed: %v", be.conversed)
	}
}

func TestHandler_OtherMessagesAreDeveloperAsks(t *testing.T) {
	h, fs, be := newTestHandler(t, HandlerOptions{ArchitectChannel: "CARCH"})
	ctx := context.Background()
	h.handleCallback(ctx, mention("CGEN", "300.1", "", "<@UBOT> fix the typo in README"))
	if len(be.processed) != 1 || !strings.HasSuffix(be.processed[0], "fix the typo in README") {
		t.Fatalf("processed = %v", be.processed)
	}
	if last := fs.texts()[len(fs.texts())-1]; !strings.Contains(last, "Done — opened https://github.com/o/r/pull/1") {
		t.Errorf("final reply = %q", last)
	}
	// Plain channel chatter outside architect threads is ignored.
	h.handleCallback(ctx, message("CGEN", "channel", "301.1", "300.1", "thanks!"))
	h.handleCallback(ctx, message("CGEN", "channel", "302.1", "", "unrelated"))
	if len(be.processed) != 1 || len(be.conversed) != 0 {
		t.Errorf("chatter should be ignored: processed %v conversed %v", be.processed, be.conversed)
	}
	// Bot messages are ignored.
	ev := message("D1", "im", "303.1", "", "hello")
	ev.InnerEvent.Data.(*slackevents.MessageEvent).BotID = "B1"
	h.handleCallback(ctx, ev)
	if len(be.processed) != 1 {
		t.Error("bot message processed")
	}
}

func TestChannelNotifier(t *testing.T) {
	h, fs, _ := newTestHandler(t, HandlerOptions{})
	n := &ChannelNotifier{Client: h.client, Channel: "CNOTIFY"}
	if err := n.Notify(context.Background(), "P-1 needs a human"); err != nil {
		t.Fatal(err)
	}
	if fs.posts[0].channel != "CNOTIFY" || fs.posts[0].threadTS != "" {
		t.Errorf("post = %+v", fs.posts[0])
	}
}
