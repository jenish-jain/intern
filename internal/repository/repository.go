package repository

import (
	"context"
	"errors"
)

type RepositoryClient interface {
	CloneRepository(ctx context.Context, destPath string) error
	SyncWithRemote(ctx context.Context) error
	ListFiles(ctx context.Context, path string) ([]string, error)
	CreateBranch(ctx context.Context, branchName string) error
	SwitchBranch(ctx context.Context, branchName string) error
	AddFile(ctx context.Context, filePath string) error
	Commit(ctx context.Context, message string) error
	Push(ctx context.Context, branchName string) error
	CreatePullRequest(ctx context.Context, baseBranch, headBranch, title, body string) (string, error)
	HasLocalChanges(ctx context.Context) (bool, error)
	IsPRMerged(ctx context.Context, prURL string) (bool, error)
}

type RepositoryService struct {
	Client RepositoryClient
}

func NewRepositoryService(client RepositoryClient) *RepositoryService {
	return &RepositoryService{Client: client}
}

func (r *RepositoryService) CloneRepository(ctx context.Context, destPath string) error {
	return r.Client.CloneRepository(ctx, destPath)
}

func (r *RepositoryService) SyncWithRemote(ctx context.Context) error {
	return r.Client.SyncWithRemote(ctx)
}

func (r *RepositoryService) ListFiles(ctx context.Context, path string) ([]string, error) {
	return r.Client.ListFiles(ctx, path)
}

func (r *RepositoryService) CreateBranch(ctx context.Context, branchName string) error {
	return r.Client.CreateBranch(ctx, branchName)
}

func (r *RepositoryService) SwitchBranch(ctx context.Context, branchName string) error {
	return r.Client.SwitchBranch(ctx, branchName)
}

func (r *RepositoryService) AddFile(ctx context.Context, filePath string) error {
	return r.Client.AddFile(ctx, filePath)
}

func (r *RepositoryService) Commit(ctx context.Context, message string) error {
	return r.Client.Commit(ctx, message)
}

func (r *RepositoryService) Push(ctx context.Context, branchName string) error {
	return r.Client.Push(ctx, branchName)
}

func (r *RepositoryService) CreatePullRequest(ctx context.Context, baseBranch, headBranch, title, body string) (string, error) {
	return r.Client.CreatePullRequest(ctx, baseBranch, headBranch, title, body)
}

func (r *RepositoryService) HasLocalChanges(ctx context.Context) (bool, error) {
	return r.Client.HasLocalChanges(ctx)
}

func (r *RepositoryService) IsPRMerged(ctx context.Context, prURL string) (bool, error) {
	return r.Client.IsPRMerged(ctx, prURL)
}

// CheckState summarizes CI on a commit.
type CheckState string

const (
	ChecksNone    CheckState = "none" // no CI configured for the commit
	ChecksPending CheckState = "pending"
	ChecksSuccess CheckState = "success"
	ChecksFailure CheckState = "failure"
)

// PRState is the current state of a pull request.
type PRState struct {
	Number     int
	URL        string
	HeadBranch string
	HeadSHA    string
	Merged     bool
	Closed     bool
	Checks     CheckState
	// FailedChecks names the failing statuses and check runs, with links
	// where GitHub provides them.
	FailedChecks []string
	Labels       []string
}

// HasLabel reports whether the PR carries label.
func (p PRState) HasLabel(label string) bool {
	for _, l := range p.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// ReviewComment is an inline comment on a PR line.
type ReviewComment struct {
	Path string
	Line int
	Body string
}

// PullRequests is implemented by VCS backends that support the review,
// test and merge stages. It is optional; RepositoryService returns
// ErrUnsupported when the backend doesn't implement it.
type PullRequests interface {
	// FetchBranch makes the local branch match the remote branch.
	FetchBranch(ctx context.Context, branch string) error
	GetPR(ctx context.Context, prURL string) (PRState, error)
	// CommentPR posts a plain comment on the PR conversation.
	CommentPR(ctx context.Context, prURL, body string) error
	// ReviewPR posts a review (as a comment, since a bot can't approve
	// its own PR) with optional inline comments.
	ReviewPR(ctx context.Context, prURL, body string, comments []ReviewComment) error
	AddLabels(ctx context.Context, prURL string, labels ...string) error
	RemoveLabel(ctx context.Context, prURL, label string) error
	// MergePR merges with method "merge", "squash" or "rebase" and
	// deletes the head branch. It returns ErrMergeConflict when the PR
	// can't merge cleanly.
	MergePR(ctx context.Context, prURL, method string) error
}

// ErrMergeConflict means the PR can't merge cleanly into its base.
var ErrMergeConflict = errors.New("pull request has merge conflicts")

// ErrUnsupported is returned when the backend lacks an optional capability.
var ErrUnsupported = errors.New("operation not supported by repository backend")

func (r *RepositoryService) prs() (PullRequests, error) {
	p, ok := r.Client.(PullRequests)
	if !ok {
		return nil, ErrUnsupported
	}
	return p, nil
}

func (r *RepositoryService) FetchBranch(ctx context.Context, branch string) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.FetchBranch(ctx, branch)
}

func (r *RepositoryService) GetPR(ctx context.Context, prURL string) (PRState, error) {
	p, err := r.prs()
	if err != nil {
		return PRState{}, err
	}
	return p.GetPR(ctx, prURL)
}

func (r *RepositoryService) CommentPR(ctx context.Context, prURL, body string) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.CommentPR(ctx, prURL, body)
}

func (r *RepositoryService) ReviewPR(ctx context.Context, prURL, body string, comments []ReviewComment) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.ReviewPR(ctx, prURL, body, comments)
}

func (r *RepositoryService) AddLabels(ctx context.Context, prURL string, labels ...string) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.AddLabels(ctx, prURL, labels...)
}

func (r *RepositoryService) RemoveLabel(ctx context.Context, prURL, label string) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.RemoveLabel(ctx, prURL, label)
}

func (r *RepositoryService) MergePR(ctx context.Context, prURL, method string) error {
	p, err := r.prs()
	if err != nil {
		return err
	}
	return p.MergePR(ctx, prURL, method)
}
