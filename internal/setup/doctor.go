package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"intern/internal/config"
)

// Status is the outcome of one check.
type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
)

// Result is one doctor check's outcome, with a fix when it isn't OK.
type Result struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// Doctor checks that a configuration can actually reach everything it
// names. The URL and exec fields default to the real services and tools;
// tests point them elsewhere.
type Doctor struct {
	Cfg          *config.Config
	HTTP         *http.Client
	GitHubAPI    string
	AnthropicAPI string
	SlackAPI     string
	LookPath     func(string) (string, error)
	Exec         func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RequiredStatuses are the JIRA statuses the ticket lifecycle moves through.
var RequiredStatuses = []string{"To Do", "In Progress", "In Review", "Done"}

func (d *Doctor) defaults() {
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if d.GitHubAPI == "" {
		d.GitHubAPI = "https://api.github.com"
	}
	if d.AnthropicAPI == "" {
		d.AnthropicAPI = "https://api.anthropic.com"
	}
	if d.SlackAPI == "" {
		d.SlackAPI = "https://slack.com/api"
	}
	if d.LookPath == nil {
		d.LookPath = exec.LookPath
	}
	if d.Exec == nil {
		d.Exec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
}

// Run performs every check that applies to the configuration.
func (d *Doctor) Run(ctx context.Context) []Result {
	d.defaults()
	var out []Result
	out = append(out, d.tools(ctx)...)
	out = append(out, d.github(ctx)...)
	if d.Cfg.TicketingMode == "jira" || d.Cfg.TicketingMode == "" {
		out = append(out, d.jira(ctx)...)
	}
	if d.Cfg.SlackBotToken != "" {
		out = append(out, d.slack(ctx))
	}
	if d.Cfg.AIProvider == "anthropic" || d.Cfg.AIRunner == "claude-code" {
		out = append(out, d.anthropic(ctx))
	}
	return out
}

func (d *Doctor) tools(ctx context.Context) []Result {
	var out []Result
	if _, err := d.LookPath("git"); err != nil {
		out = append(out, Result{Name: "git", Status: StatusFail, Detail: "git is not installed", Fix: "Install git."})
	} else {
		out = append(out, Result{Name: "git", Status: StatusOK, Detail: "installed"})
	}
	if d.Cfg.AIRunner == "claude-code" {
		if _, err := d.LookPath("claude"); err != nil {
			out = append(out, Result{Name: "Claude Code CLI", Status: StatusFail, Detail: "claude is not on PATH",
				Fix: "Install it with `npm install -g @anthropic-ai/claude-code`, or use the intern Docker image, which includes it."})
		} else {
			v, _ := d.Exec(ctx, "claude", "--version")
			out = append(out, Result{Name: "Claude Code CLI", Status: StatusOK, Detail: strings.TrimSpace(string(v))})
		}
		if d.Cfg.Sandbox == "docker" {
			if _, err := d.Exec(ctx, "docker", "image", "inspect", d.Cfg.SandboxImage); err != nil {
				out = append(out, Result{Name: "Docker sandbox", Status: StatusFail, Detail: "image " + d.Cfg.SandboxImage + " not found or Docker not running",
					Fix: "Build it with `docker build -t " + d.Cfg.SandboxImage + " deploy/sandbox` and make sure Docker is running."})
			} else {
				out = append(out, Result{Name: "Docker sandbox", Status: StatusOK, Detail: "image " + d.Cfg.SandboxImage})
			}
		}
	}
	return out
}

func (d *Doctor) github(ctx context.Context) []Result {
	repo := d.Cfg.GitHubOwner + "/" + d.Cfg.GitHubRepo
	auth := map[string]string{"Authorization": "Bearer " + d.Cfg.GitHubToken, "Accept": "application/vnd.github+json"}
	var info struct {
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	code, err := d.getJSON(ctx, d.GitHubAPI+"/repos/"+repo, auth, &info)
	switch {
	case err != nil:
		return []Result{{Name: "GitHub", Status: StatusFail, Detail: err.Error(), Fix: "Check your network connection."}}
	case code == http.StatusUnauthorized:
		return []Result{{Name: "GitHub", Status: StatusFail, Detail: "GITHUB_TOKEN was rejected", Fix: "Create a new token; the current one is invalid or expired."}}
	case code == http.StatusNotFound || code == http.StatusForbidden:
		return []Result{{Name: "GitHub", Status: StatusFail, Detail: repo + " not found with this token",
			Fix: "Check GITHUB_OWNER and GITHUB_REPO, and give the token access to the repo."}}
	case code != http.StatusOK:
		return []Result{{Name: "GitHub", Status: StatusFail, Detail: fmt.Sprintf("GitHub returned %d", code)}}
	}
	out := []Result{{Name: "GitHub", Status: StatusOK, Detail: "can read " + repo}}
	if !info.Permissions.Push {
		out[0] = Result{Name: "GitHub", Status: StatusWarn, Detail: "the token's user can't push to " + repo,
			Fix: "Intern pushes branches and opens PRs: give the token Contents, Pull requests and Issues write access."}
	}

	base := d.Cfg.BaseBranch
	code, err = d.getJSON(ctx, d.GitHubAPI+"/repos/"+repo+"/branches/"+url.PathEscape(base), auth, nil)
	switch {
	case err != nil:
	case code == http.StatusNotFound:
		out = append(out, Result{Name: "Base branch", Status: StatusFail, Detail: "branch " + base + " doesn't exist",
			Fix: "Set BASE_BRANCH to the repo's default branch, or push a first commit to " + base + "."})
	case code == http.StatusOK:
		out = append(out, Result{Name: "Base branch", Status: StatusOK, Detail: base})
	}
	return out
}

func (d *Doctor) jira(ctx context.Context) []Result {
	base := strings.TrimRight(d.Cfg.JiraURL, "/")
	auth := map[string]string{"Accept": "application/json"}
	req := func(path string, v any) (int, error) {
		return d.getJSONBasic(ctx, base+path, d.Cfg.JiraEmail, d.Cfg.JiraAPIToken, auth, v)
	}

	code, err := req("/rest/api/3/myself", nil)
	switch {
	case err != nil:
		return []Result{{Name: "JIRA", Status: StatusFail, Detail: err.Error(), Fix: "Check JIRA_URL."}}
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return []Result{{Name: "JIRA", Status: StatusFail, Detail: "JIRA rejected JIRA_EMAIL and JIRA_API_TOKEN",
			Fix: "Check the email, and create a new token at https://id.atlassian.com/manage-profile/security/api-tokens."}}
	case code != http.StatusOK:
		return []Result{{Name: "JIRA", Status: StatusFail, Detail: fmt.Sprintf("JIRA returned %d", code), Fix: "Check JIRA_URL."}}
	}
	out := []Result{{Name: "JIRA", Status: StatusOK, Detail: "signed in as " + d.Cfg.JiraEmail}}

	key := d.Cfg.JiraProject
	var types []struct {
		Name     string `json:"name"`
		Statuses []struct {
			Name string `json:"name"`
		} `json:"statuses"`
	}
	code, err = req("/rest/api/3/project/"+url.PathEscape(key)+"/statuses", &types)
	switch {
	case err != nil:
		return out
	case code == http.StatusNotFound:
		return append(out, Result{Name: "JIRA project", Status: StatusFail, Detail: "project " + key + " not found",
			Fix: "Check JIRA_PROJECT_KEY, and that the JIRA user can see the project."})
	case code != http.StatusOK:
		return append(out, Result{Name: "JIRA project", Status: StatusFail, Detail: fmt.Sprintf("JIRA returned %d for project %s", code, key)})
	}
	out = append(out, Result{Name: "JIRA project", Status: StatusOK, Detail: key})

	issueType := d.Cfg.JiraIssueType
	if issueType == "" {
		issueType = "Task"
	}
	have := map[string]bool{}
	found := false
	for _, t := range types {
		if strings.EqualFold(t.Name, issueType) {
			found = true
			for _, s := range t.Statuses {
				have[strings.ToLower(s.Name)] = true
			}
		}
	}
	if !found {
		out = append(out, Result{Name: "JIRA workflow", Status: StatusWarn, Detail: "issue type " + issueType + " isn't in project " + key,
			Fix: "Set JIRA_ISSUE_TYPE to an issue type the project has."})
	} else {
		var missing []string
		for _, s := range RequiredStatuses {
			if !have[strings.ToLower(s)] {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			out = append(out, Result{Name: "JIRA workflow", Status: StatusWarn, Detail: "missing statuses: " + strings.Join(missing, ", "),
				Fix: "Add them to the project's workflow, or set the matching JIRA_TRANSITION_* IDs."})
		} else {
			out = append(out, Result{Name: "JIRA workflow", Status: StatusOK, Detail: strings.Join(RequiredStatuses, " → ")})
		}
	}

	var users []struct {
		AccountID string `json:"accountId"`
	}
	code, err = req("/rest/api/3/user/search?query="+url.QueryEscape(d.Cfg.AgentUsername), &users)
	if err == nil && code == http.StatusOK {
		if len(users) == 0 {
			out = append(out, Result{Name: "JIRA agent user", Status: StatusWarn, Detail: "no JIRA user matches " + d.Cfg.AgentUsername,
				Fix: "Set AGENT_USERNAME to the agent's display name or email, or set JIRA_AGENT_ACCOUNT_ID."})
		} else {
			out = append(out, Result{Name: "JIRA agent user", Status: StatusOK, Detail: d.Cfg.AgentUsername})
		}
	}
	return out
}

func (d *Doctor) slack(ctx context.Context) Result {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Team  string `json:"team"`
		User  string `json:"user"`
	}
	code, err := d.getJSON(ctx, d.SlackAPI+"/auth.test", map[string]string{"Authorization": "Bearer " + d.Cfg.SlackBotToken}, &resp)
	switch {
	case err != nil:
		return Result{Name: "Slack", Status: StatusFail, Detail: err.Error(), Fix: "Check your network connection."}
	case code != http.StatusOK || !resp.OK:
		return Result{Name: "Slack", Status: StatusFail, Detail: "SLACK_BOT_TOKEN was rejected: " + resp.Error,
			Fix: "Copy the Bot User OAuth Token (xoxb-...) from the app's OAuth & Permissions page."}
	}
	detail := fmt.Sprintf("bot @%s in %s", resp.User, resp.Team)
	if d.Cfg.SlackArchitectChannel != "" || d.Cfg.SlackNotifyChannel != "" {
		detail += "; invite the bot to the architect and notify channels"
	}
	return Result{Name: "Slack", Status: StatusOK, Detail: detail}
}

func (d *Doctor) anthropic(ctx context.Context) Result {
	headers := map[string]string{"x-api-key": d.Cfg.AnthropicAPIKey, "anthropic-version": "2023-06-01"}
	code, err := d.getJSON(ctx, d.AnthropicAPI+"/v1/models?limit=1", headers, nil)
	switch {
	case err != nil:
		return Result{Name: "Anthropic", Status: StatusFail, Detail: err.Error(), Fix: "Check your network connection."}
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return Result{Name: "Anthropic", Status: StatusFail, Detail: "ANTHROPIC_API_KEY was rejected",
			Fix: "Create a key at https://console.anthropic.com/settings/keys."}
	case code != http.StatusOK:
		return Result{Name: "Anthropic", Status: StatusWarn, Detail: fmt.Sprintf("Anthropic API returned %d", code)}
	}
	return Result{Name: "Anthropic", Status: StatusOK, Detail: "API key works"}
}

func (d *Doctor) getJSON(ctx context.Context, u string, headers map[string]string, v any) (int, error) {
	return d.do(ctx, u, func(r *http.Request) {
		for k, val := range headers {
			r.Header.Set(k, val)
		}
	}, v)
}

func (d *Doctor) getJSONBasic(ctx context.Context, u, user, pass string, headers map[string]string, v any) (int, error) {
	return d.do(ctx, u, func(r *http.Request) {
		r.SetBasicAuth(user, pass)
		for k, val := range headers {
			r.Header.Set(k, val)
		}
	}, v)
}

func (d *Doctor) do(ctx context.Context, u string, prep func(*http.Request), v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	prep(req)
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if v != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, v); err != nil {
			return resp.StatusCode, fmt.Errorf("unexpected response from %s: %w", req.URL.Host, err)
		}
	}
	return resp.StatusCode, nil
}
