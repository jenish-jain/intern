// Package setup holds the interactive configuration wizard, the Slack app
// manifest and the doctor checks behind `agent setup` and `agent doctor`.
package setup

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Mode is how intern receives work.
type Mode string

const (
	// ModeSlack takes one-off tasks from Slack mentions (agent serve).
	ModeSlack Mode = "slack"
	// ModeJira builds tickets assigned to the agent in JIRA (agent start).
	ModeJira Mode = "jira"
	// ModeTeam plans with the architect in Slack and builds, reviews, tests
	// and merges the resulting JIRA backlog (agent serve).
	ModeTeam Mode = "team"
)

// Modes lists the modes in menu order.
var Modes = []struct {
	Mode    Mode
	Summary string
}{
	{ModeTeam, "Team mode: plan a project with the architect in Slack; developers, reviewer and tester build the JIRA backlog"},
	{ModeJira, "JIRA: build tickets assigned to the agent, one PR per ticket"},
	{ModeSlack, "Slack: one-off tasks from @intern mentions, one PR per task"},
}

// Field is one setting the wizard asks for.
type Field struct {
	Key      string
	Prompt   string
	Default  string
	Help     string
	Secret   bool
	Optional bool
	Check    func(string) error
}

// Section groups related fields under a heading, in the wizard and in .env.
type Section struct {
	Title  string
	Fields []Field
}

// Command is the agent subcommand that runs the mode.
func (m Mode) Command() string {
	if m == ModeJira {
		return "agent start"
	}
	return "agent serve"
}

// UsesSlack reports whether the mode needs the Slack app.
func (m Mode) UsesSlack() bool { return m == ModeSlack || m == ModeTeam }

// UsesJira reports whether the mode needs JIRA.
func (m Mode) UsesJira() bool { return m == ModeJira || m == ModeTeam }

// Fixed returns the settings a mode implies, written without asking.
func Fixed(m Mode) map[string]string {
	out := map[string]string{"AI_PROVIDER": "anthropic"}
	switch m {
	case ModeSlack:
		out["TICKETING_MODE"] = "slack"
	case ModeJira:
		out["TICKETING_MODE"] = "jira"
	case ModeTeam:
		out["TICKETING_MODE"] = "jira"
		out["AI_RUNNER"] = "claude-code"
		out["ALLOWED_WRITE_DIRS"] = "*"
	}
	return out
}

// Sections returns what the wizard asks for in a mode, in order.
func Sections(m Mode) []Section {
	var out []Section

	out = append(out, Section{Title: "GitHub", Fields: []Field{
		{Key: "GITHUB_TOKEN", Prompt: "GitHub token", Secret: true,
			Help: "Fine-grained token on the target repo with Contents, Pull requests and Issues: read and write."},
		{Key: "GITHUB_OWNER", Prompt: "Repository owner (user or org)", Check: notBlank},
		{Key: "GITHUB_REPO", Prompt: "Repository name", Check: notBlank,
			Help: "The repo intern writes code in. For a new project, create it with one commit on the base branch."},
		{Key: "BASE_BRANCH", Prompt: "Base branch", Default: "main", Check: notBlank},
	}})

	if m.UsesJira() {
		out = append(out, Section{Title: "JIRA", Fields: []Field{
			{Key: "JIRA_URL", Prompt: "JIRA URL", Default: "https://your-company.atlassian.net", Check: httpURL},
			{Key: "JIRA_EMAIL", Prompt: "JIRA account email", Check: notBlank},
			{Key: "JIRA_API_TOKEN", Prompt: "JIRA API token", Secret: true,
				Help: "Create one at https://id.atlassian.com/manage-profile/security/api-tokens"},
			{Key: "JIRA_PROJECT_KEY", Prompt: "JIRA project key", Check: notBlank},
			{Key: "AGENT_USERNAME", Prompt: "JIRA user the agent works as (display name or email)", Check: notBlank,
				Help: "Intern picks up tickets assigned to this user."},
			{Key: "POLLING_INTERVAL", Prompt: "How often to check JIRA", Default: "1m", Check: duration},
		}})
	}

	if m.UsesSlack() {
		fields := []Field{
			{Key: "SLACK_BOT_TOKEN", Prompt: "Slack bot token (xoxb-...)", Secret: true,
				Help: "OAuth & Permissions > Bot User OAuth Token."},
			{Key: "SLACK_SIGNING_SECRET", Prompt: "Slack signing secret", Secret: true,
				Help: "Basic Information > App Credentials > Signing Secret."},
		}
		if m == ModeTeam {
			fields = append(fields,
				Field{Key: "SLACK_ARCHITECT_CHANNEL", Prompt: "Architect channel ID (C...)", Optional: true, Check: channelID,
					Help: "Every new thread in this channel is a planning conversation. Leave empty to use `@intern plan:` only."},
				Field{Key: "SLACK_NOTIFY_CHANNEL", Prompt: "Notify channel ID for escalations and merges (C...)", Optional: true, Check: channelID},
			)
		}
		out = append(out, Section{Title: "Slack", Fields: fields})
	}

	ai := []Field{
		{Key: "ANTHROPIC_API_KEY", Prompt: "Anthropic API key (sk-ant-...)", Secret: true,
			Help: "Create one at https://console.anthropic.com/settings/keys"},
	}
	if m != ModeTeam {
		ai = append(ai, Field{Key: "AI_RUNNER", Prompt: "Runner (claude-code or legacy)", Default: "claude-code", Check: oneOf("claude-code", "legacy"),
			Help: "claude-code runs the Claude Code CLI as the developer; legacy is the built-in planner."})
	}
	out = append(out, Section{Title: "AI", Fields: ai})

	if m == ModeTeam {
		out = append(out, Section{Title: "Team", Fields: []Field{
			{Key: "REVIEW_ENABLED", Prompt: "Review every PR with the reviewer", Default: "true", Check: boolean},
			{Key: "TEST_ENABLED", Prompt: "Write acceptance tests with the tester", Default: "true", Check: boolean},
			{Key: "AUTO_MERGE", Prompt: "Merge approved PRs automatically when CI passes", Default: "false", Check: boolean,
				Help: "Start with false: ready PRs wait for you with the intern:ready-to-merge label."},
			{Key: "MAX_CONCURRENT_TICKETS", Prompt: "Tickets built in parallel", Default: "1", Check: positiveInt},
		}})
	}

	out = append(out, Section{Title: "Limits", Fields: []Field{
		{Key: "TICKET_BUDGET_USD", Prompt: "Spend cap per job in USD", Default: "2", Check: positiveNumber},
		{Key: "PROJECT_BUDGET_USD", Prompt: "Total spend cap in USD (0 for none)", Default: "20", Check: nonNegativeNumber},
	}})
	return out
}

func notBlank(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("a value is required")
	}
	return nil
}

func httpURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("expected a URL like https://your-company.atlassian.net")
	}
	return nil
}

func duration(s string) error {
	if _, err := time.ParseDuration(s); err != nil {
		return fmt.Errorf("expected a duration like 30s, 1m or 5m")
	}
	return nil
}

func channelID(s string) error {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "#") || !(strings.HasPrefix(s, "C") || strings.HasPrefix(s, "G")) {
		return fmt.Errorf("expected a channel ID like C0123ABCD (channel details > About), not a #name")
	}
	return nil
}

func boolean(s string) error {
	if _, err := strconv.ParseBool(s); err != nil {
		return fmt.Errorf("expected true or false")
	}
	return nil
}

func positiveInt(s string) error {
	if n, err := strconv.Atoi(s); err != nil || n < 1 {
		return fmt.Errorf("expected a whole number of at least 1")
	}
	return nil
}

func positiveNumber(s string) error {
	if f, err := strconv.ParseFloat(s, 64); err != nil || f <= 0 {
		return fmt.Errorf("expected a number above 0")
	}
	return nil
}

func nonNegativeNumber(s string) error {
	if f, err := strconv.ParseFloat(s, 64); err != nil || f < 0 {
		return fmt.Errorf("expected a number, 0 or more")
	}
	return nil
}

func oneOf(values ...string) func(string) error {
	return func(s string) error {
		for _, v := range values {
			if s == v {
				return nil
			}
		}
		return fmt.Errorf("expected one of: %s", strings.Join(values, ", "))
	}
}
