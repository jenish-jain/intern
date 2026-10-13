package setup

import (
	"strings"
	"testing"
)

func TestValues_Describe(t *testing.T) {
	v := Values{
		EnvFile: ".env",
		File:    map[string]string{"GITHUB_TOKEN": "github_pat_new_value_1234", "JIRA_PROJECT_KEY": "TODO", "JIRA_EMAIL": "a@b.com"},
		Shell:   map[string]string{"GITHUB_TOKEN": "ghp_old_exported_9876", "JIRA_EMAIL": "a@b.com"},
	}
	got := v.Describe("GITHUB_TOKEN")
	for _, want := range []string{"from your shell", "overrides", "ghp_…9876", "gith…1234", "unset GITHUB_TOKEN"} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe(GITHUB_TOKEN) = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "old_exported") || strings.Contains(got, "new_value") {
		t.Errorf("secret leaked: %q", got)
	}
	if got := v.Describe("JIRA_PROJECT_KEY"); got != "JIRA_PROJECT_KEY=TODO in .env" {
		t.Errorf("project key isn't secret, got %q", got)
	}
	if got := v.Describe("JIRA_EMAIL"); !strings.Contains(got, "a@b.com from your shell") {
		t.Errorf("same value in shell and file: %q", got)
	}
	if got := v.Describe("BASE_BRANCH"); got != "BASE_BRANCH is not set" {
		t.Errorf("unset key: %q", got)
	}
}
