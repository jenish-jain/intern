package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"intern/internal/runner"
)

func TestSchemasAreValidJSON(t *testing.T) {
	for role, schema := range schemas {
		var v map[string]any
		if err := json.Unmarshal([]byte(schema), &v); err != nil {
			t.Errorf("%s schema is not valid JSON: %v", role, err)
		}
	}
	for _, role := range []runner.Role{runner.RoleDeveloper, runner.RoleReviewer, runner.RoleTester, runner.RoleArchitect} {
		if schemas[role] == "" {
			t.Errorf("missing schema for %s", role)
		}
	}
}

func TestToResult_ReviewAndTestFields(t *testing.T) {
	var ev resultEvent
	raw := `{"type":"result","subtype":"success","structured_output":{"status":"done","summary":"s","verdict":"request_changes",
		"comments":[{"path":"a.go","line":4,"body":"handle error"}]}}`
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	res := ev.toResult()
	if res.Review == nil || res.Review.Approve || len(res.Review.Comments) != 1 || res.Review.Comments[0].Line != 4 {
		t.Errorf("review not parsed: %+v", res.Review)
	}
	if res.Test != nil {
		t.Error("test report should be nil for a review")
	}

	raw = `{"type":"result","subtype":"success","structured_output":{"status":"done","summary":"s","tests_passed":false,"failures":["login rejects valid password"]}}`
	ev = resultEvent{}
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	res = ev.toResult()
	if res.Test == nil || res.Test.Passed || res.Test.Failures[0] != "login rejects valid password" {
		t.Errorf("test report not parsed: %+v", res.Test)
	}
}

func TestArgs_ReviewerCannotEdit(t *testing.T) {
	r := New(Config{})
	dir := t.TempDir()
	rev := strings.Join(r.args(runner.Job{Role: runner.RoleReviewer, Workdir: dir}, "persona"), "\n")
	allowed := between(rev, "--allowedTools\n", "\n")
	denied := between(rev, "--disallowedTools\n", "\n")
	if strings.Contains(allowed, "Edit") || strings.Contains(allowed, "Write") {
		t.Errorf("reviewer allowed edit tools: %s", allowed)
	}
	if !strings.Contains(denied, "Edit") || !strings.Contains(denied, "Write") {
		t.Errorf("reviewer edit tools not denied: %s", denied)
	}
	if !strings.Contains(rev, `"verdict"`) {
		t.Error("reviewer schema not used")
	}

	dev := strings.Join(r.args(runner.Job{Role: runner.RoleDeveloper, Workdir: dir}, "persona"), "\n")
	if !strings.Contains(between(dev, "--allowedTools\n", "\n"), "Edit") {
		t.Error("developer should be allowed to edit")
	}
}

// between returns the text after start up to the next end.
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	if j := strings.Index(s, end); j >= 0 {
		return s[:j]
	}
	return s
}
