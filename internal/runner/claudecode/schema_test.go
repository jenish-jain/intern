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
	allowedSet := toSet(allowed)
	for _, tool := range editTools {
		if allowedSet[tool] {
			t.Errorf("reviewer allowed %s", tool)
		}
		if !toSet(denied)[tool] {
			t.Errorf("reviewer did not deny %s", tool)
		}
	}
	if !strings.Contains(rev, `"verdict"`) {
		t.Error("reviewer schema not used")
	}

	dev := strings.Join(r.args(runner.Job{Role: runner.RoleDeveloper, Workdir: dir}, "persona"), "\n")
	if !toSet(between(dev, "--allowedTools\n", "\n"))["Edit"] {
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

func toSet(csv string) map[string]bool {
	m := map[string]bool{}
	for _, t := range strings.Split(csv, ",") {
		m[t] = true
	}
	return m
}

// TestToResult_RecoversLeakedFields uses output captured from a real
// reviewer run, where the model put its comments inside the summary.
func TestToResult_RecoversLeakedFields(t *testing.T) {
	leaked := "Max is inverted: it returns the smaller argument. Fix the comparison and add tests.</summary>\n" +
		`<parameter name="comments">[{"path":"calc/max.go","line":5,"body":"Comparison is backwards."},` +
		`{"path":"calc/max_test.go","line":4,"body":"Add table cases."}]`
	raw, _ := json.Marshal(map[string]any{
		"type": "result", "subtype": "success",
		"structured_output": map[string]any{"status": "done", "verdict": "request_changes", "summary": leaked},
	})
	var ev resultEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	res := ev.toResult()
	if strings.Contains(res.Summary, "<parameter") || strings.Contains(res.Summary, "</summary>") {
		t.Errorf("markup left in summary: %q", res.Summary)
	}
	if !strings.HasPrefix(res.Summary, "Max is inverted") {
		t.Errorf("summary text lost: %q", res.Summary)
	}
	if res.Review == nil || len(res.Review.Comments) != 2 || res.Review.Comments[0].Line != 5 {
		t.Errorf("comments not recovered: %+v", res.Review)
	}
}

func TestToResult_LeakedScalarFields(t *testing.T) {
	leaked := `All criteria pass.</summary><parameter name="tests_passed">true</parameter><parameter name="failures">[]</parameter>`
	raw, _ := json.Marshal(map[string]any{
		"type": "result", "subtype": "success",
		"structured_output": map[string]any{"status": "done", "summary": leaked},
	})
	var ev resultEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	res := ev.toResult()
	if res.Test == nil || !res.Test.Passed || res.Summary != "All criteria pass." {
		t.Errorf("got %+v / %q", res.Test, res.Summary)
	}
}
