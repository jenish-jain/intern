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

// Result is one doctor check's outcome, with a fix when it isn't OK. Keys
// names the settings the check used, so a failure points at the value to
// change.
type Result struct {
	Name   string
	Status Status
	Detail string
	Fix    string
	Keys   []string
}

func ok(name, detail string) Result { return Result{Name: name, Status: StatusOK, Detail: detail} }

func fail(name, detail, fix string, keys ...string) Result {
	return Result{Name: name, Status: StatusFail, Detail: detail, Fix: fix, Keys: keys}
}

func warn(name, detail, fix string, keys ...string) Result {
	return Result{Name: name, Status: StatusWarn, Detail: detail, Fix: fix, Keys: keys}
}

// withReason appends the service's own explanation, when it gave one.
func withReason(detail string, code int, reason string) string {
	switch {
	case reason != "":
		return fmt.Sprintf("%s (%d: %s)", detail, code, reason)
	case code != 0:
		return fmt.Sprintf("%s (HTTP %d)", detail, code)
	}
	return detail
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
		out = append(out, fail("git", "git is not installed", "Install git."))
	} else {
		out = append(out, ok("git", "installed"))
	}
	if d.Cfg.AIRunner == "claude-code" {
		if _, err := d.LookPath("claude"); err != nil {
			out = append(out, fail("Claude Code CLI", "claude is not on PATH, and AI_RUNNER=claude-code needs it",
				"Install it with `npm install -g @anthropic-ai/claude-code`, or use the intern Docker image, which includes it.", "AI_RUNNER"))
		} else {
			v, _ := d.Exec(ctx, "claude", "--version")
			out = append(out, ok("Claude Code CLI", strings.TrimSpace(string(v))))
		}
		// Claude Code's Bash tool needs bash or zsh; with neither, every
		// command the developer runs fails with "No suitable shell found".
		_, bashErr := d.LookPath("bash")
		_, zshErr := d.LookPath("zsh")
		if bashErr != nil && zshErr != nil && d.Cfg.Sandbox != "docker" {
			out = append(out, fail("Shell", "neither bash nor zsh is installed, so Claude Code can't run commands",
				"Install bash and set SHELL=/bin/bash (the intern Docker image does both).", "AI_RUNNER"))
		}
		if d.Cfg.Sandbox == "docker" {
			if _, err := d.Exec(ctx, "docker", "image", "inspect", d.Cfg.SandboxImage); err != nil {
				out = append(out, fail("Docker sandbox", "image "+d.Cfg.SandboxImage+" not found or Docker not running",
					"Build it with `docker build -t "+d.Cfg.SandboxImage+" deploy/sandbox` and make sure Docker is running.", "SANDBOX_IMAGE"))
			} else {
				out = append(out, ok("Docker sandbox", "image "+d.Cfg.SandboxImage))
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
	code, reason, err := d.getJSON(ctx, d.GitHubAPI+"/repos/"+repo, auth, &info)
	switch {
	case err != nil:
		return []Result{fail("GitHub", err.Error(), "Check your network connection.")}
	case code == http.StatusUnauthorized:
		return []Result{fail("GitHub", withReason("GitHub rejected GITHUB_TOKEN", code, reason),
			"The token is invalid, expired or revoked. Create a new one and put it in GITHUB_TOKEN.", "GITHUB_TOKEN")}
	case code == http.StatusNotFound || code == http.StatusForbidden:
		return []Result{fail("GitHub", withReason(repo+" isn't visible to this token", code, reason),
			"Check GITHUB_OWNER and GITHUB_REPO, and that the token's repository access includes "+repo+".",
			"GITHUB_OWNER", "GITHUB_REPO", "GITHUB_TOKEN")}
	case code != http.StatusOK:
		return []Result{fail("GitHub", withReason("GitHub returned an error", code, reason), "", "GITHUB_TOKEN")}
	}
	out := []Result{ok("GitHub", "can read "+repo)}
	if !info.Permissions.Push {
		out[0] = warn("GitHub", "the token's user can't push to "+repo,
			"Intern pushes branches and opens PRs: give the token Contents, Pull requests and Issues write access.", "GITHUB_TOKEN")
	}

	base := d.Cfg.BaseBranch
	code, _, err = d.getJSON(ctx, d.GitHubAPI+"/repos/"+repo+"/branches/"+url.PathEscape(base), auth, nil)
	switch {
	case err != nil:
	case code == http.StatusNotFound:
		out = append(out, fail("Base branch", "branch "+base+" doesn't exist in "+repo,
			"Set BASE_BRANCH to the repo's default branch, or push a first commit to "+base+".", "BASE_BRANCH"))
	case code == http.StatusOK:
		out = append(out, ok("Base branch", base))
	}
	return out
}

func (d *Doctor) jira(ctx context.Context) []Result {
	base := strings.TrimRight(d.Cfg.JiraURL, "/")
	auth := map[string]string{"Accept": "application/json"}
	req := func(path string, v any) (int, string, error) {
		return d.getJSONBasic(ctx, base+path, d.Cfg.JiraEmail, d.Cfg.JiraAPIToken, auth, v)
	}

	code, reason, err := req("/rest/api/3/myself", nil)
	switch {
	case err != nil:
		return []Result{fail("JIRA", "can't reach JIRA: "+err.Error(), "Check JIRA_URL, e.g. https://your-company.atlassian.net.", "JIRA_URL")}
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return []Result{fail("JIRA", withReason("JIRA rejected the email and API token", code, reason),
			"The token must be created while signed in to id.atlassian.com as JIRA_EMAIL, and not be expired or revoked.",
			"JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_URL")}
	case code == http.StatusNotFound:
		return []Result{fail("JIRA", withReason(base+" isn't a JIRA Cloud site", code, reason), "Check JIRA_URL.", "JIRA_URL")}
	case code != http.StatusOK:
		return []Result{fail("JIRA", withReason("JIRA returned an error", code, reason), "Check JIRA_URL.", "JIRA_URL")}
	}
	out := []Result{ok("JIRA", "signed in as "+d.Cfg.JiraEmail)}

	key := d.Cfg.JiraProject
	var types []struct {
		Name     string `json:"name"`
		Statuses []struct {
			Name string `json:"name"`
		} `json:"statuses"`
	}
	code, reason, err = req("/rest/api/3/project/"+url.PathEscape(key)+"/statuses", &types)
	switch {
	case err != nil:
		return out
	case code == http.StatusNotFound:
		return append(out, fail("JIRA project", withReason("project "+key+" not found", code, reason),
			"Use the project's key (the prefix of its ticket numbers, e.g. PROJ in PROJ-12), and check JIRA_EMAIL can see it.", "JIRA_PROJECT_KEY"))
	case code != http.StatusOK:
		return append(out, fail("JIRA project", withReason("JIRA returned an error for project "+key, code, reason), "", "JIRA_PROJECT_KEY"))
	}
	out = append(out, ok("JIRA project", key))

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
		out = append(out, warn("JIRA workflow", "issue type "+issueType+" isn't in project "+key,
			"Set JIRA_ISSUE_TYPE to an issue type the project has.", "JIRA_ISSUE_TYPE"))
	} else {
		var missing []string
		for _, s := range RequiredStatuses {
			if !have[strings.ToLower(s)] {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			out = append(out, warn("JIRA workflow", "missing statuses: "+strings.Join(missing, ", "),
				"Add them to the project's workflow, or set the matching JIRA_TRANSITION_* IDs."))
		} else {
			out = append(out, ok("JIRA workflow", strings.Join(RequiredStatuses, " → ")))
		}
	}

	var users []struct {
		AccountID string `json:"accountId"`
	}
	code, _, err = req("/rest/api/3/user/search?query="+url.QueryEscape(d.Cfg.AgentUsername), &users)
	if err == nil && code == http.StatusOK {
		if len(users) == 0 {
			out = append(out, warn("JIRA agent user", "no JIRA user matches "+d.Cfg.AgentUsername,
				"Set AGENT_USERNAME to the agent's display name or email, or set JIRA_AGENT_ACCOUNT_ID.", "AGENT_USERNAME"))
		} else {
			out = append(out, ok("JIRA agent user", d.Cfg.AgentUsername))
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
	code, _, err := d.getJSON(ctx, d.SlackAPI+"/auth.test", map[string]string{"Authorization": "Bearer " + d.Cfg.SlackBotToken}, &resp)
	switch {
	case err != nil:
		return fail("Slack", err.Error(), "Check your network connection.")
	case code != http.StatusOK || !resp.OK:
		return fail("Slack", withReason("Slack rejected SLACK_BOT_TOKEN", code, resp.Error),
			"Copy the Bot User OAuth Token (xoxb-...) from the app's OAuth & Permissions page.", "SLACK_BOT_TOKEN")
	}
	detail := fmt.Sprintf("bot @%s in %s", resp.User, resp.Team)
	if d.Cfg.SlackArchitectChannel != "" || d.Cfg.SlackNotifyChannel != "" {
		detail += "; invite the bot to the architect and notify channels"
	}
	return ok("Slack", detail)
}

func (d *Doctor) anthropic(ctx context.Context) Result {
	headers := map[string]string{"x-api-key": d.Cfg.AnthropicAPIKey, "anthropic-version": "2023-06-01"}
	code, reason, err := d.getJSON(ctx, d.AnthropicAPI+"/v1/models?limit=1", headers, nil)
	switch {
	case err != nil:
		return fail("Anthropic", err.Error(), "Check your network connection.")
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fail("Anthropic", withReason("Anthropic rejected ANTHROPIC_API_KEY", code, reason),
			"Create a key at https://console.anthropic.com/settings/keys.", "ANTHROPIC_API_KEY")
	case code != http.StatusOK:
		return warn("Anthropic", withReason("Anthropic API returned an error", code, reason), "", "ANTHROPIC_API_KEY")
	}
	return ok("Anthropic", "API key works")
}

func (d *Doctor) getJSON(ctx context.Context, u string, headers map[string]string, v any) (int, string, error) {
	return d.do(ctx, u, func(r *http.Request) {
		for k, val := range headers {
			r.Header.Set(k, val)
		}
	}, v)
}

func (d *Doctor) getJSONBasic(ctx context.Context, u, user, pass string, headers map[string]string, v any) (int, string, error) {
	return d.do(ctx, u, func(r *http.Request) {
		r.SetBasicAuth(user, pass)
		for k, val := range headers {
			r.Header.Set(k, val)
		}
	}, v)
}

// do sends a GET and decodes a 200 response into v. For any other status
// it returns the service's error message, when the body has one.
func (d *Doctor) do(ctx context.Context, u string, prep func(*http.Request), v any) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, "", err
	}
	prep(req)
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, errorMessage(body), nil
	}
	if v != nil {
		if err := json.Unmarshal(body, v); err != nil {
			return resp.StatusCode, "", fmt.Errorf("unexpected response from %s: %w", req.URL.Host, err)
		}
	}
	return resp.StatusCode, "", nil
}

// errorMessage pulls the human-readable message out of an error body from
// GitHub ({"message"}), JIRA ({"errorMessages"}), Anthropic ({"error":
// {"message"}}) or a plain-text response.
func errorMessage(body []byte) string {
	var e struct {
		Message       string   `json:"message"`
		ErrorMessages []string `json:"errorMessages"`
		Error         struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch {
		case e.Message != "":
			return e.Message
		case len(e.ErrorMessages) > 0:
			return strings.Join(e.ErrorMessages, "; ")
		case e.Error.Message != "":
			return e.Error.Message
		}
		return ""
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 120 || strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}
