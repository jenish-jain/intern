package github

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"intern/internal/repository"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gh "github.com/google/go-github/v58/github"
)

var _ repository.PullRequests = (*githubClient)(nil)

// prNumber extracts the PR number from a URL such as
// "https://github.com/owner/repo/pull/123".
func prNumber(prURL string) (int, error) {
	numStr := prURL[strings.LastIndex(prURL, "/")+1:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PR URL %q: %w", prURL, err)
	}
	return num, nil
}

// auth returns HTTP credentials for GitHub remotes, and none for local
// remotes used in tests.
func (c *githubClient) auth() *http.BasicAuth {
	if c.repoURL != "" && !strings.HasPrefix(c.repoURL, "http") {
		return nil
	}
	return &http.BasicAuth{Username: c.owner, Password: c.token}
}

// FetchBranch fetches branch from origin and points the local branch at
// it, so stages after the developer (review, test) can work on a fresh
// instance whose clone never had the branch.
func (c *githubClient) FetchBranch(ctx context.Context, branch string) error {
	repo, err := git.PlainOpen(c.paths.Root())
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	spec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:%s", branch, remoteRef))
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{spec},
		Auth:       c.auth(),
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("fetch %s: %w", branch, err)
	}
	ref, err := repo.Reference(remoteRef, true)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", remoteRef, err)
	}
	local := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), ref.Hash())
	if err := repo.Storer.SetReference(local); err != nil {
		return fmt.Errorf("update local branch %s: %w", branch, err)
	}
	return nil
}

func (c *githubClient) GetPR(ctx context.Context, prURL string) (repository.PRState, error) {
	num, err := prNumber(prURL)
	if err != nil {
		return repository.PRState{}, err
	}
	pr, _, err := c.ghClient.PullRequests.Get(ctx, c.owner, c.repo, num)
	if err != nil {
		return repository.PRState{}, fmt.Errorf("get PR #%d: %w", num, err)
	}
	st := repository.PRState{
		Number:     num,
		URL:        pr.GetHTMLURL(),
		HeadBranch: pr.GetHead().GetRef(),
		HeadSHA:    pr.GetHead().GetSHA(),
		Merged:     pr.GetMerged(),
		Closed:     pr.GetState() == "closed",
	}
	for _, l := range pr.Labels {
		st.Labels = append(st.Labels, l.GetName())
	}
	if st.HeadSHA != "" && !st.Merged {
		if st.Checks, st.FailedChecks, err = c.checkState(ctx, st.HeadSHA); err != nil {
			return st, err
		}
	}
	return st, nil
}

// checkState combines commit statuses and check runs for sha, and names
// the ones that failed.
func (c *githubClient) checkState(ctx context.Context, sha string) (repository.CheckState, []string, error) {
	pending, seen := false, false
	var failed []string

	combined, _, err := c.ghClient.Repositories.GetCombinedStatus(ctx, c.owner, c.repo, sha, nil)
	if err != nil {
		return "", nil, fmt.Errorf("get commit status: %w", err)
	}
	if combined.GetTotalCount() > 0 {
		seen = true
		if combined.GetState() == "pending" {
			pending = true
		}
		for _, st := range combined.Statuses {
			if s := st.GetState(); s == "failure" || s == "error" {
				failed = append(failed, describeCheck(st.GetContext(), st.GetTargetURL()))
			}
		}
		if s := combined.GetState(); (s == "failure" || s == "error") && len(failed) == 0 {
			failed = append(failed, "commit status "+s)
		}
	}

	runs, _, err := c.ghClient.Checks.ListCheckRunsForRef(ctx, c.owner, c.repo, sha, &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return "", nil, fmt.Errorf("list check runs: %w", err)
	}
	for _, run := range runs.CheckRuns {
		seen = true
		if run.GetStatus() != "completed" {
			pending = true
			continue
		}
		switch run.GetConclusion() {
		case "success", "neutral", "skipped":
		default:
			failed = append(failed, describeCheck(run.GetName()+" ("+run.GetConclusion()+")", run.GetHTMLURL()))
		}
	}

	switch {
	case len(failed) > 0:
		return repository.ChecksFailure, failed, nil
	case pending:
		return repository.ChecksPending, nil, nil
	case seen:
		return repository.ChecksSuccess, nil, nil
	default:
		return repository.ChecksNone, nil, nil
	}
}

func describeCheck(name, url string) string {
	if url == "" {
		return name
	}
	return name + ": " + url
}

func (c *githubClient) CommentPR(ctx context.Context, prURL, body string) error {
	num, err := prNumber(prURL)
	if err != nil {
		return err
	}
	if _, _, err := c.ghClient.Issues.CreateComment(ctx, c.owner, c.repo, num, &gh.IssueComment{Body: gh.String(body)}); err != nil {
		return fmt.Errorf("comment on PR #%d: %w", num, err)
	}
	return nil
}

func (c *githubClient) ReviewPR(ctx context.Context, prURL, body string, comments []repository.ReviewComment) error {
	num, err := prNumber(prURL)
	if err != nil {
		return err
	}
	req := &gh.PullRequestReviewRequest{Body: gh.String(body), Event: gh.String("COMMENT")}
	for _, cm := range comments {
		if cm.Path == "" || cm.Line <= 0 {
			continue
		}
		req.Comments = append(req.Comments, &gh.DraftReviewComment{
			Path: gh.String(cm.Path), Line: gh.Int(cm.Line), Side: gh.String("RIGHT"), Body: gh.String(cm.Body),
		})
	}
	_, _, err = c.ghClient.PullRequests.CreateReview(ctx, c.owner, c.repo, num, req)
	if err == nil || len(req.Comments) == 0 {
		if err != nil {
			return fmt.Errorf("review PR #%d: %w", num, err)
		}
		return nil
	}
	// GitHub rejects inline comments on lines outside the diff. Fall back
	// to one review with the comments folded into the body.
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n### Comments\n")
	for _, cm := range comments {
		fmt.Fprintf(&b, "- `%s:%d` %s\n", cm.Path, cm.Line, cm.Body)
	}
	fallback := &gh.PullRequestReviewRequest{Body: gh.String(b.String()), Event: gh.String("COMMENT")}
	if _, _, err2 := c.ghClient.PullRequests.CreateReview(ctx, c.owner, c.repo, num, fallback); err2 != nil {
		return fmt.Errorf("review PR #%d: %w (inline attempt: %v)", num, err2, err)
	}
	return nil
}

func (c *githubClient) AddLabels(ctx context.Context, prURL string, labels ...string) error {
	num, err := prNumber(prURL)
	if err != nil {
		return err
	}
	if _, _, err := c.ghClient.Issues.AddLabelsToIssue(ctx, c.owner, c.repo, num, labels); err != nil {
		return fmt.Errorf("label PR #%d: %w", num, err)
	}
	return nil
}

func (c *githubClient) RemoveLabel(ctx context.Context, prURL, label string) error {
	num, err := prNumber(prURL)
	if err != nil {
		return err
	}
	resp, err := c.ghClient.Issues.RemoveLabelForIssue(ctx, c.owner, c.repo, num, label)
	if err != nil && (resp == nil || resp.StatusCode != 404) {
		return fmt.Errorf("unlabel PR #%d: %w", num, err)
	}
	return nil
}

func (c *githubClient) MergePR(ctx context.Context, prURL, method string) error {
	num, err := prNumber(prURL)
	if err != nil {
		return err
	}
	pr, _, err := c.ghClient.PullRequests.Get(ctx, c.owner, c.repo, num)
	if err != nil {
		return fmt.Errorf("get PR #%d: %w", num, err)
	}
	opts := &gh.PullRequestOptions{MergeMethod: method, SHA: pr.GetHead().GetSHA()}
	if pr.Mergeable != nil && !pr.GetMergeable() {
		return fmt.Errorf("merge PR #%d: %w", num, repository.ErrMergeConflict)
	}
	res, resp, err := c.ghClient.PullRequests.Merge(ctx, c.owner, c.repo, num, "", opts)
	if err != nil {
		// 405 means GitHub won't merge it as-is; with a dirty mergeable
		// state that's a conflict the developer has to resolve.
		if resp != nil && resp.StatusCode == 405 && pr.GetMergeableState() == "dirty" {
			return fmt.Errorf("merge PR #%d: %w", num, repository.ErrMergeConflict)
		}
		return fmt.Errorf("merge PR #%d: %w", num, err)
	}
	if !res.GetMerged() {
		return fmt.Errorf("merge PR #%d: %s", num, res.GetMessage())
	}
	// Branch cleanup is best-effort; the merge already happened.
	_, _ = c.ghClient.Git.DeleteRef(ctx, c.owner, c.repo, "heads/"+pr.GetHead().GetRef())
	return nil
}

// selfLogin returns the token owner's login, looked up once.
func (c *githubClient) selfLogin(ctx context.Context) string {
	c.loginOnce.Do(func() {
		if u, _, err := c.ghClient.Users.Get(ctx, ""); err == nil {
			c.login = u.GetLogin()
		}
	})
	return c.login
}

func (c *githubClient) ListComments(ctx context.Context, prURL string, since time.Time) ([]repository.PRComment, error) {
	num, err := prNumber(prURL)
	if err != nil {
		return nil, err
	}
	self := c.selfLogin(ctx)
	type stamped struct {
		at time.Time
		c  repository.PRComment
	}
	var all []stamped

	issueComments, _, err := c.ghClient.Issues.ListComments(ctx, c.owner, c.repo, num, &gh.IssueListCommentsOptions{
		Since: &since, ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list PR comments: %w", err)
	}
	for _, ic := range issueComments {
		if ic.GetUser().GetLogin() == self || ic.GetCreatedAt().Before(since) {
			continue
		}
		all = append(all, stamped{ic.GetCreatedAt().Time, repository.PRComment{Author: ic.GetUser().GetLogin(), Body: ic.GetBody()}})
	}
	reviewComments, _, err := c.ghClient.PullRequests.ListComments(ctx, c.owner, c.repo, num, &gh.PullRequestListCommentsOptions{
		Since: since, ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list PR review comments: %w", err)
	}
	for _, rc := range reviewComments {
		if rc.GetUser().GetLogin() == self || rc.GetCreatedAt().Before(since) {
			continue
		}
		all = append(all, stamped{rc.GetCreatedAt().Time, repository.PRComment{
			Author: rc.GetUser().GetLogin(), Body: rc.GetBody(), Path: rc.GetPath(), Line: rc.GetLine(),
		}})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	out := make([]repository.PRComment, len(all))
	for i, s := range all {
		out[i] = s.c
	}
	return out, nil
}
