package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	r.git("config", "user.email", "t@t")
	r.git("config", "user.name", "t")
	r.write("README.md", "hi\n")
	r.write("internal/a.go", "package internal\n")
	r.git("add", ".")
	r.git("commit", "-q", "-m", "init")
	return r
}

func (r *repo) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) base() string {
	r.t.Helper()
	sha, err := HeadSHA(context.Background(), r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	return sha
}

func reasons(vs []Violation) string {
	var s []string
	for _, v := range vs {
		s = append(s, v.String())
	}
	return strings.Join(s, "; ")
}

func TestCheck_CleanChangeIncludingRunnerCommits(t *testing.T) {
	r := newRepo(t)
	base := r.base()

	// The runner commits one change itself and leaves another uncommitted.
	r.write("internal/b.go", "package internal\n")
	r.git("add", ".")
	r.git("commit", "-q", "-m", "runner commit")
	r.write("internal/a.go", "package internal\n\nfunc A() {}\n")
	r.write(".ai-intern/journal.json", "{}")

	changes, violations, err := Check(context.Background(), r.dir, base, Policy{AllowedDirs: []string{"internal"}, MaxFiles: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("unexpected violations: %s", reasons(violations))
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want internal/a.go and internal/b.go", changes)
	}
	for _, c := range changes {
		if strings.HasPrefix(c.Path, AgentStateDir) {
			t.Errorf("agent state must not be staged: %+v", c)
		}
	}
}

func TestCheck_Violations(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(r *repo)
		policy Policy
		want   string
	}{
		{"outside allowlist", func(r *repo) { r.write("web/app.ts", "x") }, Policy{AllowedDirs: []string{"internal"}}, `directory "web" is not in the allowed list`},
		{"root file without dot", func(r *repo) { r.write("Makefile", "x") }, Policy{AllowedDirs: []string{"internal"}}, `directory "Makefile"`},
		{"protected path", func(r *repo) { r.write(".github/workflows/ci.yml", "x") }, Policy{AllowedDirs: []string{"*"}, ProtectedPaths: []string{".github/"}}, "path is protected"},
		{"too many files", func(r *repo) { r.write("internal/x.go", "x"); r.write("internal/y.go", "y") }, Policy{MaxFiles: 1}, "2 files changed, limit is 1"},
		{"large file", func(r *repo) { r.write("internal/big.bin", strings.Repeat("a", 2048)) }, Policy{MaxFileBytes: 1024}, "limit is 1024"},
		{"secret", func(r *repo) {
			r.write("internal/cfg.go", "const k = \"AKIAABCDEFGHIJKLMNOP\"\n")
		}, Policy{}, "AWS access key"},
		{"private key", func(r *repo) {
			r.write("internal/key.pem", "-----BEGIN RSA PRIVATE KEY-----\nabc\n")
		}, Policy{}, "private key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo(t)
			base := r.base()
			tc.setup(r)
			_, violations, err := Check(context.Background(), r.dir, base, tc.policy)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(reasons(violations), tc.want) {
				t.Errorf("violations %q do not mention %q", reasons(violations), tc.want)
			}
		})
	}
}

func TestCheck_AllowsRootFilesWithDotAndDeletions(t *testing.T) {
	r := newRepo(t)
	base := r.base()
	r.write("Makefile", "all:\n")
	if err := os.Remove(filepath.Join(r.dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	changes, violations, err := Check(context.Background(), r.dir, base, Policy{AllowedDirs: []string{"internal", "."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("unexpected violations: %s", reasons(violations))
	}
	if len(changes) != 2 {
		t.Errorf("changes = %+v, want Makefile added and README.md deleted", changes)
	}
}

func TestCheckPath_Traversal(t *testing.T) {
	if got := checkPath("../etc/passwd", Policy{AllowedDirs: []string{"*"}}); got == "" {
		t.Error("expected traversal to be rejected")
	}
}
