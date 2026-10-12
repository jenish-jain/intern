package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"intern/internal/repository"

	gh "github.com/google/go-github/v58/github"
)

type apiCall struct {
	method, path string
	body         map[string]any
}

// fakeGitHub serves a scripted subset of the REST API and records calls.
type fakeGitHub struct {
	mu       sync.Mutex
	calls    []apiCall
	status   string // combined status state ("" = no statuses)
	runs     string // JSON array of check runs
	reviewOK bool   // accept inline review comments
	conflict bool   // PR is not mergeable
}

func (f *fakeGitHub) handler(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		_ = json.Unmarshal(data, &body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, body})
	f.mu.Unlock()

	p := strings.TrimPrefix(r.URL.Path, "/api/v3")
	switch {
	case r.Method == "GET" && p == "/repos/o/r/pulls/7":
		mergeable := `"mergeable":true,"mergeable_state":"clean"`
		if f.conflict {
			mergeable = `"mergeable":false,"mergeable_state":"dirty"`
		}
		_, _ = io.WriteString(w, `{"number":7,"state":"open","merged":false,"html_url":"https://github.com/o/r/pull/7",`+mergeable+`,
			"head":{"ref":"feature/P-1","sha":"abc123"},"labels":[{"name":"intern:review"}]}`)
	case r.Method == "GET" && p == "/repos/o/r/commits/abc123/status":
		total := 0
		if f.status != "" {
			total = 1
		}
		statuses := "[]"
		if f.status == "error" {
			statuses = `[{"state":"error","context":"ci/build","target_url":"https://ci/1"}]`
		}
		_, _ = io.WriteString(w, `{"state":"`+f.status+`","total_count":`+string(rune('0'+total))+`,"statuses":`+statuses+`}`)
	case r.Method == "GET" && p == "/repos/o/r/commits/abc123/check-runs":
		runs := f.runs
		if runs == "" {
			runs = "[]"
		}
		_, _ = io.WriteString(w, `{"total_count":1,"check_runs":`+runs+`}`)
	case r.Method == "POST" && p == "/repos/o/r/pulls/7/reviews":
		if comments, ok := body["comments"].([]any); ok && len(comments) > 0 && !f.reviewOK {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"message":"Line could not be resolved"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":1}`)
	case r.Method == "POST" && p == "/repos/o/r/issues/7/comments":
		_, _ = io.WriteString(w, `{"id":1}`)
	case r.Method == "POST" && p == "/repos/o/r/issues/7/labels":
		_, _ = io.WriteString(w, `[]`)
	case r.Method == "DELETE" && strings.HasPrefix(p, "/repos/o/r/issues/7/labels/"):
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"message":"Label does not exist"}`)
	case r.Method == "PUT" && p == "/repos/o/r/pulls/7/merge":
		_, _ = io.WriteString(w, `{"merged":true,"message":"Pull Request successfully merged"}`)
	case r.Method == "DELETE" && p == "/repos/o/r/git/refs/heads/feature/P-1":
		w.WriteHeader(204)
	default:
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"message":"not scripted: `+r.Method+" "+p+`"}`)
	}
}

func (f *fakeGitHub) find(method, suffix string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.method == method && strings.HasSuffix(c.path, suffix) {
			out = append(out, c)
		}
	}
	return out
}

func newFakeClient(t *testing.T, f *fakeGitHub) *githubClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	api := gh.NewClient(nil)
	base, _ := url.Parse(srv.URL + "/api/v3/")
	api.BaseURL = base
	return &githubClient{ghClient: api, owner: "o", repo: "r"}
}

const prURL = "https://github.com/o/r/pull/7"

func TestGetPR_CheckStates(t *testing.T) {
	cases := []struct {
		name   string
		status string
		runs   string
		want   repository.CheckState
	}{
		{"no CI", "", "", repository.ChecksNone},
		{"status success", "success", "", repository.ChecksSuccess},
		{"run in progress", "", `[{"status":"in_progress"}]`, repository.ChecksPending},
		{"run failed", "success", `[{"status":"completed","conclusion":"failure","name":"test","html_url":"https://gh/run/1"}]`, repository.ChecksFailure},
		{"skipped counts as ok", "", `[{"status":"completed","conclusion":"skipped"},{"status":"completed","conclusion":"success"}]`, repository.ChecksSuccess},
		{"status error", "error", "", repository.ChecksFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient(t, &fakeGitHub{status: tc.status, runs: tc.runs})
			st, err := c.GetPR(context.Background(), prURL)
			if err != nil {
				t.Fatal(err)
			}
			if st.Checks != tc.want {
				t.Errorf("checks = %s, want %s", st.Checks, tc.want)
			}
			if st.HeadBranch != "feature/P-1" || st.HeadSHA != "abc123" || !st.HasLabel("intern:review") {
				t.Errorf("unexpected PR state %+v", st)
			}
		})
	}
}

func TestReviewPR_FallsBackWhenInlineCommentsRejected(t *testing.T) {
	f := &fakeGitHub{}
	c := newFakeClient(t, f)
	err := c.ReviewPR(context.Background(), prURL, "Changes requested", []repository.ReviewComment{{Path: "a.go", Line: 3, Body: "nil check"}})
	if err != nil {
		t.Fatal(err)
	}
	reviews := f.find("POST", "/pulls/7/reviews")
	if len(reviews) != 2 {
		t.Fatalf("expected inline attempt then fallback, got %d calls", len(reviews))
	}
	fallback := reviews[1].body
	if fallback["event"] != "COMMENT" || !strings.Contains(fallback["body"].(string), "`a.go:3` nil check") {
		t.Errorf("fallback review body = %+v", fallback)
	}
}

func TestReviewPR_InlineAccepted(t *testing.T) {
	f := &fakeGitHub{reviewOK: true}
	c := newFakeClient(t, f)
	if err := c.ReviewPR(context.Background(), prURL, "LGTM", []repository.ReviewComment{{Path: "a.go", Line: 3, Body: "nit"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.find("POST", "/pulls/7/reviews")); n != 1 {
		t.Errorf("expected one review call, got %d", n)
	}
}

func TestMergePR_UsesHeadSHAAndDeletesBranch(t *testing.T) {
	f := &fakeGitHub{}
	c := newFakeClient(t, f)
	if err := c.MergePR(context.Background(), prURL, "squash"); err != nil {
		t.Fatal(err)
	}
	merge := f.find("PUT", "/pulls/7/merge")
	if len(merge) != 1 || merge[0].body["merge_method"] != "squash" || merge[0].body["sha"] != "abc123" {
		t.Errorf("merge call = %+v", merge)
	}
	if len(f.find("DELETE", "/git/refs/heads/feature/P-1")) != 1 {
		t.Error("head branch not deleted")
	}
}

func TestLabelsAndComments(t *testing.T) {
	f := &fakeGitHub{}
	c := newFakeClient(t, f)
	ctx := context.Background()
	if err := c.AddLabels(ctx, prURL, "intern:approved"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLabel(ctx, prURL, "intern:absent"); err != nil {
		t.Errorf("removing a missing label should not fail: %v", err)
	}
	if err := c.CommentPR(ctx, prURL, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := prNumber("https://github.com/o/r/pull/abc"); err == nil {
		t.Error("expected error for malformed PR URL")
	}
}

func TestFetchBranch_FromLocalRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin := filepath.Join(root, "origin")
	run(root, "init", "-q", "-b", "main", origin)
	run(origin, "config", "user.email", "t@t")
	run(origin, "config", "user.name", "t")
	run(origin, "commit", "-q", "--allow-empty", "-m", "init")
	clone := filepath.Join(root, "clone")
	run(root, "clone", "-q", origin, clone)
	// The branch appears on the remote after the clone was made.
	run(origin, "checkout", "-q", "-b", "feature/P-1")
	run(origin, "commit", "-q", "--allow-empty", "-m", "work")
	want := run(origin, "rev-parse", "HEAD")

	paths, err := repository.NewRepositoryPath(root, "clone")
	if err != nil {
		t.Fatal(err)
	}
	c := &githubClient{repoURL: origin, paths: paths}
	if err := c.FetchBranch(context.Background(), "feature/P-1"); err != nil {
		t.Fatalf("FetchBranch: %v", err)
	}
	if got := run(clone, "rev-parse", "refs/heads/feature/P-1"); got != want {
		t.Errorf("local branch at %s, want %s", got, want)
	}
}

func TestGetPR_NamesFailedChecks(t *testing.T) {
	c := newFakeClient(t, &fakeGitHub{status: "error", runs: `[{"status":"completed","conclusion":"failure","name":"test","html_url":"https://gh/run/1"}]`})
	st, err := c.GetPR(context.Background(), prURL)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(st.FailedChecks, " | ")
	if !strings.Contains(got, "ci/build: https://ci/1") || !strings.Contains(got, "test (failure): https://gh/run/1") {
		t.Errorf("failed checks = %q", got)
	}
}

func TestMergePR_Conflict(t *testing.T) {
	f := &fakeGitHub{conflict: true}
	c := newFakeClient(t, f)
	err := c.MergePR(context.Background(), prURL, "squash")
	if !errors.Is(err, repository.ErrMergeConflict) {
		t.Fatalf("expected ErrMergeConflict, got %v", err)
	}
	if len(f.find("PUT", "/pulls/7/merge")) != 0 {
		t.Error("should not attempt a merge GitHub already reports as conflicting")
	}
}
