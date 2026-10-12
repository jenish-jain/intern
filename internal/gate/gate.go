// Package gate checks a ticket branch's changes before intern pushes them.
// It inspects git, not the runner's account of what it did, so the same
// rules hold whichever engine produced the changes.
package gate

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// AgentStateDir is intern's own directory inside the clone. It is never
// staged or checked.
const AgentStateDir = ".ai-intern"

// DefaultMaxFileBytes is the largest file a change may add.
const DefaultMaxFileBytes = 1 << 20

// Policy is what a change must satisfy.
type Policy struct {
	// AllowedDirs are permitted first path segments. "." allows root-level
	// files and "*" allows everything.
	AllowedDirs []string
	// MaxFiles caps the number of changed files (0 means no cap).
	MaxFiles int
	// ProtectedPaths are path prefixes that must not change.
	ProtectedPaths []string
	// MaxFileBytes caps the size of each added or modified file (0 uses
	// DefaultMaxFileBytes).
	MaxFileBytes int64
	// OnlyPaths, when set, restricts changes to matching paths. An entry
	// ending in "/" matches that directory anywhere in the tree; any other
	// entry is a glob matched against the file name (e.g. "*_test.go").
	OnlyPaths []string
}

// DefaultTestPaths are where testers may write when the project doesn't
// say otherwise.
var DefaultTestPaths = []string{
	"tests/", "test/", "e2e/", "__tests__/", "spec/",
	"*_test.go", "*.test.ts", "*.test.tsx", "*.test.js", "*.spec.ts", "*.spec.js",
	"test_*.py", "*_test.py", "*_spec.rb",
}

// MatchesAny reports whether path matches one of the OnlyPaths-style
// patterns.
func MatchesAny(path string, patterns []string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	for _, p := range patterns {
		if strings.HasSuffix(p, "/") {
			dir := strings.TrimSuffix(p, "/")
			if strings.HasPrefix(clean, dir+"/") || strings.Contains(clean, "/"+dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// Change is one changed path. Status is git's name-status letter
// (A, M, D, R, C, T).
type Change struct {
	Status string
	Path   string
}

// Violation is a rule a change broke.
type Violation struct {
	Path   string
	Reason string
}

func (v Violation) String() string { return v.Path + ": " + v.Reason }

// HeadSHA returns the current commit, used as the base a runner's changes
// are diffed against.
func HeadSHA(ctx context.Context, repoRoot string) (string, error) {
	out, err := git(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// StageAll stages every change in the worktree except intern's own state.
func StageAll(ctx context.Context, repoRoot string) error {
	_, err := git(ctx, repoRoot, "add", "-A", "--", ".", ":(exclude)"+AgentStateDir)
	return err
}

// Changed lists staged changes relative to base, including any commits
// the runner made itself.
func Changed(ctx context.Context, repoRoot, base string) ([]Change, error) {
	out, err := git(ctx, repoRoot, "diff", "--cached", "--name-status", "--no-renames", base, "--", ".", ":(exclude)"+AgentStateDir)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		changes = append(changes, Change{Status: parts[0][:1], Path: parts[1]})
	}
	return changes, nil
}

// Check stages the worktree and validates every change since base
// against policy. It returns the changes and any violations.
func Check(ctx context.Context, repoRoot, base string, p Policy) ([]Change, []Violation, error) {
	if err := StageAll(ctx, repoRoot); err != nil {
		return nil, nil, err
	}
	changes, err := Changed(ctx, repoRoot, base)
	if err != nil {
		return nil, nil, err
	}
	var violations []Violation
	if p.MaxFiles > 0 && len(changes) > p.MaxFiles {
		violations = append(violations, Violation{Path: "*", Reason: fmt.Sprintf("%d files changed, limit is %d", len(changes), p.MaxFiles)})
	}
	maxBytes := p.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	for _, c := range changes {
		if reason := checkPath(c.Path, p); reason != "" {
			violations = append(violations, Violation{Path: c.Path, Reason: reason})
			continue
		}
		if c.Status == "D" {
			continue
		}
		if info, err := os.Lstat(filepath.Join(repoRoot, c.Path)); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				violations = append(violations, Violation{Path: c.Path, Reason: "symlinks are not allowed"})
			} else if info.Size() > maxBytes {
				violations = append(violations, Violation{Path: c.Path, Reason: fmt.Sprintf("file is %d bytes, limit is %d", info.Size(), maxBytes)})
			}
		}
	}
	secrets, err := scanSecrets(ctx, repoRoot, base)
	if err != nil {
		return changes, violations, err
	}
	return changes, append(violations, secrets...), nil
}

func checkPath(path string, p Policy) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if filepath.IsAbs(path) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "path escapes the repository"
	}
	if len(p.OnlyPaths) > 0 && !MatchesAny(clean, p.OnlyPaths) {
		return "only test files may be changed in this stage"
	}
	for _, prefix := range p.ProtectedPaths {
		prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/")
		if prefix != "" && (clean == prefix || strings.HasPrefix(clean, prefix+"/")) {
			return "path is protected"
		}
	}
	if len(p.AllowedDirs) == 0 || contains(p.AllowedDirs, "*") {
		return ""
	}
	first := clean
	if i := strings.IndexByte(clean, '/'); i >= 0 {
		first = clean[:i]
	} else if contains(p.AllowedDirs, ".") {
		return ""
	}
	if !contains(p.AllowedDirs, first) {
		return fmt.Sprintf("directory %q is not in the allowed list", first)
	}
	return ""
}

// secretPatterns match credentials that must never be committed.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{40,}\b`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
}

// scanSecrets checks added lines in the staged diff against base.
func scanSecrets(ctx context.Context, repoRoot, base string) ([]Violation, error) {
	out, err := git(ctx, repoRoot, "diff", "--cached", "-U0", "--no-color", base, "--", ".", ":(exclude)"+AgentStateDir)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	seen := map[string]bool{}
	file := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++ ") {
			file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			continue
		}
		if !strings.HasPrefix(line, "+") {
			continue
		}
		for _, sp := range secretPatterns {
			if sp.re.MatchString(line) && !seen[file+sp.name] {
				seen[file+sp.name] = true
				violations = append(violations, Violation{Path: file, Reason: "contains what looks like a " + sp.name})
			}
		}
	}
	return violations, sc.Err()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
