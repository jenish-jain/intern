package setup

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func prompter(input string) (*Prompter, *strings.Builder) {
	out := &strings.Builder{}
	return &Prompter{In: bufio.NewReader(strings.NewReader(input)), Out: out}, out
}

func TestAsk_DefaultsRetriesAndKeepsSecrets(t *testing.T) {
	p, out := prompter("\n#arch\nC0ARCH\n\n")
	f := Field{Key: "SLACK_ARCHITECT_CHANNEL", Prompt: "Channel", Optional: true, Check: channelID}
	if v, err := p.Ask(f, ""); err != nil || v != "" {
		t.Fatalf("optional field left empty: got %q, %v", v, err)
	}
	if v, _ := p.Ask(f, ""); v != "C0ARCH" {
		t.Errorf("want the retried value C0ARCH, got %q", v)
	}
	if !strings.Contains(out.String(), "not a #name") {
		t.Error("an invalid value should explain the expected format")
	}
	secret := Field{Key: "GITHUB_TOKEN", Prompt: "Token", Secret: true}
	if v, _ := p.Ask(secret, "ghp_existing_value"); v != "ghp_existing_value" {
		t.Errorf("enter should keep the current secret, got %q", v)
	}
	if strings.Contains(out.String(), "ghp_existing_value") {
		t.Error("the current secret must not be printed")
	}
}

func TestAsk_RequiredFieldReprompts(t *testing.T) {
	p, out := prompter("\nacme\n")
	v, err := p.Ask(Field{Key: "GITHUB_OWNER", Prompt: "Owner", Check: notBlank}, "")
	if err != nil || v != "acme" {
		t.Fatalf("got %q, %v", v, err)
	}
	if !strings.Contains(out.String(), "A value is required") {
		t.Error("an empty required field should be asked again")
	}
}

func TestCollect_TeamModeWritesALoadableEnv(t *testing.T) {
	answers := []string{
		"ghp_tok", "acme", "todo", "", // GitHub (base branch default)
		"https://acme.atlassian.net", "bot@acme.com", "jira tok$en", "TODO", "ai-intern", "", // JIRA
		"xoxb-1", "sig", "C0ARCH", "", // Slack
		"sk-ant-1",         // AI
		"", "", "true", "", // Team
		"", "", // Limits
	}
	p, _ := prompter(strings.Join(answers, "\n") + "\n")
	existing := map[string]string{"CUSTOM_SETTING": "kept", "SLACK_NOTIFY_CHANNEL": "C0OLD"}
	values, err := p.Collect(ModeTeam, existing)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values["SLACK_NOTIFY_CHANNEL"]; !ok {
		t.Error("enter on an optional field with a current value should keep it")
	}

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("OLD=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnv(path, ModeTeam, values); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != "OLD=1\n" {
		t.Errorf("previous file should be kept as .bak, got %q", b)
	}
	got, err := godotenv.Read(path)
	if err != nil {
		t.Fatalf("written .env doesn't parse: %v", err)
	}
	want := map[string]string{
		"TICKETING_MODE": "jira", "AI_RUNNER": "claude-code", "ALLOWED_WRITE_DIRS": "*",
		"BASE_BRANCH": "main", "JIRA_API_TOKEN": "jira tok$en", "SLACK_NOTIFY_CHANNEL": "C0OLD",
		"AUTO_MERGE": "true", "REVIEW_ENABLED": "true", "PROJECT_BUDGET_USD": "20", "CUSTOM_SETTING": "kept",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if ModeOf(got) != ModeTeam {
		t.Errorf("ModeOf the written file = %s, want team", ModeOf(got))
	}
}

func TestSections_OnlyAskWhatTheModeNeeds(t *testing.T) {
	keys := func(m Mode) map[string]bool {
		out := map[string]bool{}
		for _, s := range Sections(m) {
			for _, f := range s.Fields {
				out[f.Key] = true
			}
		}
		return out
	}
	if k := keys(ModeSlack); k["JIRA_URL"] || k["SLACK_ARCHITECT_CHANNEL"] || !k["SLACK_BOT_TOKEN"] {
		t.Errorf("slack mode asks the wrong things: %v", k)
	}
	if k := keys(ModeJira); k["SLACK_BOT_TOKEN"] || !k["JIRA_URL"] || !k["AI_RUNNER"] {
		t.Errorf("jira mode asks the wrong things: %v", k)
	}
	if k := keys(ModeTeam); k["AI_RUNNER"] || !k["SLACK_ARCHITECT_CHANNEL"] || !k["REVIEW_ENABLED"] {
		t.Errorf("team mode asks the wrong things: %v", k)
	}
}

func TestSlackManifest(t *testing.T) {
	without := SlackManifest("")
	if strings.Contains(without, "event_subscriptions") {
		t.Error("without a URL the manifest should leave events out, since Slack verifies the URL on save")
	}
	with := SlackManifest("https://intern.run.app/")
	for _, want := range []string{"request_url: https://intern.run.app/slack/events", "app_mention", "message.channels", "channels:history", "chat:write"} {
		if !strings.Contains(with, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}
