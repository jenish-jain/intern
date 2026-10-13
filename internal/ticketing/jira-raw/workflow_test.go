package jiraraw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"intern/internal/ticketing"

	logger "github.com/jenish-jain/logger"
)

func TestMain(m *testing.M) {
	logger.Init("error")
	os.Exit(m.Run())
}

type recorded struct {
	method, path string
	body         map[string]any
}

// newTestClient starts a fake JIRA server. handler returns status and
// response body for each request; every request is recorded.
func newTestClient(t *testing.T, handler func(r *http.Request) (int, string)) (Client, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{method: r.Method, path: r.URL.Path}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			_ = json.Unmarshal(data, &rec.body)
		}
		reqs = append(reqs, rec)
		status, body := handler(r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientConfig{BaseURL: srv.URL, Email: "a@b.c", APIToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &reqs
}

const transitionsJSON = `{"transitions":[
	{"id":"11","name":"Reopen","to":{"name":"To Do"}},
	{"id":"41","name":"Send to review","to":{"name":"In Review"}},
	{"id":"31","name":"Finish","to":{"name":"Done"}}]}`

func TestUpdateTicketStatus_ResolvesTransitionByName(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) {
		if r.Method == "GET" {
			return 200, transitionsJSON
		}
		return 204, ""
	})
	if err := c.UpdateTicketStatus(context.Background(), "P-1", "in review", map[string]string{}); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("expected GET + POST, got %d requests", len(*reqs))
	}
	post := (*reqs)[1]
	if post.method != "POST" || post.path != "/rest/api/3/issue/P-1/transitions" {
		t.Fatalf("unexpected request %s %s", post.method, post.path)
	}
	if id := post.body["transition"].(map[string]any)["id"]; id != "41" {
		t.Errorf("transition id = %v, want 41", id)
	}
}

func TestUpdateTicketStatus_ConfiguredIDSkipsLookup(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) { return 204, "" })
	err := c.UpdateTicketStatus(context.Background(), "P-1", "Done", map[string]string{"Done": "99"})
	if err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != "POST" {
		t.Fatalf("expected a single POST, got %+v", *reqs)
	}
	if id := (*reqs)[0].body["transition"].(map[string]any)["id"]; id != "99" {
		t.Errorf("transition id = %v, want 99", id)
	}
}

func TestUpdateTicketStatus_UnknownStatusListsAvailable(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) { return 200, transitionsJSON })
	err := c.UpdateTicketStatus(context.Background(), "P-1", "Testing", nil)
	if err == nil || !strings.Contains(err.Error(), "In Review") {
		t.Fatalf("expected error listing available statuses, got %v", err)
	}
}

func TestGetTicket_MapsStatusCategory(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, `{"id":"1","key":"P-7","fields":{"summary":"S","status":{"name":"In Review","statusCategory":{"key":"indeterminate"}}}}`
	})
	tk, err := c.GetTicket(context.Background(), "P-7")
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if tk.Key != "P-7" || tk.Status != "In Review" || tk.StatusCategory != "indeterminate" {
		t.Errorf("unexpected ticket %+v", tk)
	}
	if (*reqs)[0].path != "/rest/api/3/issue/P-7" {
		t.Errorf("path = %s", (*reqs)[0].path)
	}
}

func TestAddComment_SendsADF(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) { return 201, `{}` })
	if err := c.AddComment(context.Background(), "P-1", "line one\nline two\n\nsecond para"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	doc := (*reqs)[0].body["body"].(map[string]any)
	paras := doc["content"].([]any)
	if doc["type"] != "doc" || len(paras) != 2 {
		t.Fatalf("expected doc with 2 paragraphs, got %+v", doc)
	}
	first := paras[0].(map[string]any)["content"].([]any)
	if len(first) != 3 || first[1].(map[string]any)["type"] != "hardBreak" {
		t.Errorf("expected text, hardBreak, text in first paragraph, got %+v", first)
	}
}

func TestCreateTicket_ReturnsKey(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) { return 201, `{"id":"10","key":"P-42"}` })
	key, err := c.CreateTicket(context.Background(), ticketing.NewTicket{
		Project: "P", Summary: "Add login", Description: "Users can log in.", Labels: []string{"epic-auth"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if key != "P-42" {
		t.Errorf("key = %s, want P-42", key)
	}
	fields := (*reqs)[0].body["fields"].(map[string]any)
	if fields["issuetype"].(map[string]any)["name"] != "Task" {
		t.Errorf("default issue type not Task: %+v", fields["issuetype"])
	}
	if fields["project"].(map[string]any)["key"] != "P" {
		t.Errorf("project not set: %+v", fields["project"])
	}
}

func TestLinkBlocks_Direction(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) { return 201, `` })
	if err := c.LinkBlocks(context.Background(), "P-1", "P-2"); err != nil {
		t.Fatalf("LinkBlocks: %v", err)
	}
	body := (*reqs)[0].body
	// P-1 blocks P-2: JIRA's create API takes the blocker as inwardIssue.
	if body["inwardIssue"].(map[string]any)["key"] != "P-1" || body["outwardIssue"].(map[string]any)["key"] != "P-2" {
		t.Errorf("P-1 must be the inwardIssue to block P-2, got %+v", body)
	}
}

func TestErrorResponseSurfacesMessage(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 400, `{"errorMessages":["Issue does not exist"]}`
	})
	_, err := c.GetTicket(context.Background(), "P-404")
	if err == nil || !strings.Contains(err.Error(), "Issue does not exist") {
		t.Fatalf("expected JIRA error message, got %v", err)
	}
}

func TestGetTickets_ParsesBlockers(t *testing.T) {
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, `{"issues":[{"key":"P-3","fields":{"summary":"S","status":{"name":"To Do"},"issuelinks":[
			{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},
			 "inwardIssue":{"key":"P-1","fields":{"status":{"name":"Done","statusCategory":{"key":"done"}}}}},
			{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},
			 "inwardIssue":{"key":"P-2","fields":{"status":{"name":"In Review","statusCategory":{"key":"indeterminate"}}}}},
			{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},
			 "outwardIssue":{"key":"P-9","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}},
			{"type":{"name":"Relates","inward":"relates to","outward":"relates to"},
			 "inwardIssue":{"key":"P-8","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}}
		]}}]}`
	})
	tickets, err := c.GetTickets(context.Background(), "bot", "P")
	if err != nil {
		t.Fatalf("GetTickets: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("got %d tickets", len(tickets))
	}
	got := tickets[0].Blockers
	if len(got) != 2 || got[0] != (ticketing.Blocker{Key: "P-1", Done: true}) || got[1] != (ticketing.Blocker{Key: "P-2", Done: false}) {
		t.Errorf("blockers = %+v, want P-1 done and P-2 open (P-9 is blocked by us, P-8 is not a Blocks link)", got)
	}
	if open := tickets[0].OpenBlockers(); len(open) != 1 || open[0] != "P-2" {
		t.Errorf("OpenBlockers = %v, want [P-2]", open)
	}
}

func TestFindUserID(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) {
		return 200, `[{"accountId":"a1","displayName":"AI Intern"},{"accountId":"a2","displayName":"AI Intern Bot"}]`
	})
	id, err := c.FindUserID(context.Background(), "ai intern")
	if err != nil || id != "a1" {
		t.Fatalf("FindUserID = %q, %v", id, err)
	}
	if !strings.HasPrefix((*reqs)[0].path, "/rest/api/3/user/search") {
		t.Errorf("path = %s", (*reqs)[0].path)
	}
	if _, err := c.FindUserID(context.Background(), "nobody"); err == nil {
		t.Error("ambiguous match should fail")
	}
}

func TestCreateTicket_Assignee(t *testing.T) {
	c, reqs := newTestClient(t, func(r *http.Request) (int, string) { return 201, `{"key":"P-1"}` })
	if _, err := c.CreateTicket(context.Background(), ticketing.NewTicket{Project: "P", Summary: "s", AssigneeID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[0].body["fields"].(map[string]any)["assignee"].(map[string]any)["accountId"]; got != "a1" {
		t.Errorf("assignee = %v", got)
	}
}

// Tickets left In Progress by a run that was interrupted (a restart or
// deploy) must be fetched again, or they're stranded forever.
func TestGetTickets_IncludesInterruptedInProgress(t *testing.T) {
	var jql string
	c, _ := newTestClient(t, func(r *http.Request) (int, string) {
		jql = r.URL.Query().Get("jql")
		return 200, `{"issues":[]}`
	})
	if _, err := c.GetTickets(context.Background(), "intern", "IP"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"statusCategory = 'To Do'", "status = 'In Progress'", "assignee = 'intern'", "project = 'IP'"} {
		if !strings.Contains(jql, want) {
			t.Errorf("JQL %q missing %q", jql, want)
		}
	}
	if strings.Contains(jql, "In Review") {
		t.Errorf("In Review tickets belong to their PR flow, not the ticket poll: %q", jql)
	}
}
