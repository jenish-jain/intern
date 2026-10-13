package setup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"intern/internal/config"
)

// fakeServices answers like GitHub, JIRA, Slack and Anthropic. Fields
// switch individual answers to failures.
type fakeServices struct {
	githubStatus   int
	branchMissing  bool
	statuses       []string
	slackOK        bool
	anthropicCode  int
	agentUserFound bool
}

func (f *fakeServices) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/todo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp" {
			t.Errorf("GitHub auth header = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(f.githubStatus)
		_, _ = w.Write([]byte(`{"permissions":{"push":true}}`))
	})
	mux.HandleFunc("/repos/acme/todo/branches/main", func(w http.ResponseWriter, r *http.Request) {
		if f.branchMissing {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "bot@acme.com" || p != "jt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/rest/api/3/project/TODO/statuses", func(w http.ResponseWriter, r *http.Request) {
		var parts []string
		for _, s := range f.statuses {
			parts = append(parts, `{"name":"`+s+`"}`)
		}
		_, _ = w.Write([]byte(`[{"name":"Task","statuses":[` + strings.Join(parts, ",") + `]}]`))
	})
	mux.HandleFunc("/rest/api/3/user/search", func(w http.ResponseWriter, r *http.Request) {
		if f.agentUserFound {
			_, _ = w.Write([]byte(`[{"accountId":"abc"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/slack/auth.test", func(w http.ResponseWriter, r *http.Request) {
		if f.slackOK {
			_, _ = w.Write([]byte(`{"ok":true,"user":"intern","team":"Acme"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk" {
			t.Errorf("Anthropic key header = %q", r.Header.Get("x-api-key"))
		}
		w.WriteHeader(f.anthropicCode)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	return mux
}

func runDoctor(t *testing.T, f *fakeServices, mutate func(*config.Config), tools map[string]bool) map[string]Result {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		TicketingMode: "jira", JiraURL: srv.URL, JiraEmail: "bot@acme.com", JiraAPIToken: "jt", JiraProject: "TODO",
		AgentUsername: "ai-intern", GitHubToken: "ghp", GitHubOwner: "acme", GitHubRepo: "todo", BaseBranch: "main",
		SlackBotToken: "xoxb", AIProvider: "anthropic", AnthropicAPIKey: "sk", AIRunner: "claude-code",
	}
	if mutate != nil {
		mutate(cfg)
	}
	d := &Doctor{
		Cfg: cfg, GitHubAPI: srv.URL, AnthropicAPI: srv.URL, SlackAPI: srv.URL + "/slack",
		LookPath: func(name string) (string, error) {
			if tools[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("2.1.0 (Claude Code)"), nil
		},
	}
	out := map[string]Result{}
	for _, r := range d.Run(context.Background()) {
		out[r.Name] = r
	}
	return out
}

func healthy() *fakeServices {
	return &fakeServices{githubStatus: 200, statuses: RequiredStatuses, slackOK: true, anthropicCode: 200, agentUserFound: true}
}

func TestDoctor_AllHealthy(t *testing.T) {
	res := runDoctor(t, healthy(), nil, map[string]bool{"git": true, "claude": true})
	for _, name := range []string{"git", "Claude Code CLI", "GitHub", "Base branch", "JIRA", "JIRA project", "JIRA workflow", "JIRA agent user", "Slack", "Anthropic"} {
		r, ok := res[name]
		if !ok {
			t.Errorf("missing check %q", name)
			continue
		}
		if r.Status != StatusOK {
			t.Errorf("%s: %v %s", name, r.Status, r.Detail)
		}
	}
}

func TestDoctor_ReportsEachProblemWithAFix(t *testing.T) {
	f := healthy()
	f.branchMissing = true
	f.statuses = []string{"To Do", "In Progress", "Done"}
	f.slackOK = false
	f.anthropicCode = 401
	f.agentUserFound = false
	res := runDoctor(t, f, func(c *config.Config) { c.JiraAPIToken = "wrong" }, map[string]bool{"git": true})

	cases := map[string]Status{
		"Claude Code CLI": StatusFail,
		"Base branch":     StatusFail,
		"JIRA":            StatusFail,
		"Slack":           StatusFail,
		"Anthropic":       StatusFail,
	}
	for name, want := range cases {
		if r := res[name]; r.Status != want || r.Fix == "" {
			t.Errorf("%s: status %v fix %q, want status %v with a fix", name, r.Status, r.Fix, want)
		}
	}
	if _, ok := res["JIRA project"]; ok {
		t.Error("project checks should be skipped when JIRA sign-in fails")
	}

	res = runDoctor(t, f, nil, map[string]bool{"git": true, "claude": true})
	if r := res["JIRA workflow"]; r.Status != StatusWarn || !strings.Contains(r.Detail, "In Review") {
		t.Errorf("JIRA workflow should warn about In Review, got %v %q", r.Status, r.Detail)
	}
	if r := res["JIRA agent user"]; r.Status != StatusWarn {
		t.Errorf("unknown agent user should warn, got %v", r.Status)
	}
}

func TestDoctor_SkipsWhatTheModeDoesntUse(t *testing.T) {
	f := healthy()
	f.githubStatus = 404
	res := runDoctor(t, f, func(c *config.Config) {
		c.TicketingMode = "slack"
		c.AIRunner = "legacy"
	}, map[string]bool{"git": true})
	for _, name := range []string{"JIRA", "Claude Code CLI"} {
		if _, ok := res[name]; ok {
			t.Errorf("%s shouldn't be checked in slack mode with the legacy runner", name)
		}
	}
	if r := res["GitHub"]; r.Status != StatusFail || !strings.Contains(r.Detail, "not found") {
		t.Errorf("missing repo should fail, got %v %q", r.Status, r.Detail)
	}
	if _, ok := res["Base branch"]; ok {
		t.Error("branch check should be skipped when the repo isn't reachable")
	}
}
