package github

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gh "github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

type Client interface {
	HealthCheck(ctx context.Context) error
	Raw() *gh.Client
	// Methods implementing repository.RepositoryClient
	CloneRepository(ctx context.Context, destPath string) error
	SyncWithRemote(ctx context.Context) error
	ListFiles(ctx context.Context, path string) ([]string, error)
	CreateBranch(ctx context.Context, branchName string) error
	SwitchBranch(ctx context.Context, branchName string) error
	AddFile(ctx context.Context, filePath string) error
	Commit(ctx context.Context, message string) error
	Push(ctx context.Context, branchName string) error
}

type githubClient struct {
	ghClient *gh.Client
	owner    string
	repo     string
	token    string // Store the token for git operations
}

func NewClient(token, owner, repo string) Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := gh.NewClient(oauth2.NewClient(context.Background(), ts))
	return &githubClient{
		ghClient: client,
		owner:    owner,
		repo:     repo,
		token:    token,
	}
}

func (c *githubClient) HealthCheck(ctx context.Context) error {
	repo, _, err := c.ghClient.Repositories.Get(ctx, c.owner, c.repo)
	if err != nil {
		return fmt.Errorf("GitHub health check failed: %w", err)
	}
	if repo == nil || repo.GetName() == "" {
		return fmt.Errorf("GitHub health check: repo info missing")
	}
	return nil
}

func (c *githubClient) Raw() *gh.Client {
	return c.ghClient
}

// Implement RepositoryClient interface methods
func (c *githubClient) CloneRepository(ctx context.Context, destPath string) error {
	_, err := git.PlainCloneContext(ctx, destPath, false, &git.CloneOptions{
		URL:      fmt.Sprintf("https://github.com/%s/%s.git", c.owner, c.repo),
		Auth:     &http.BasicAuth{Username: c.token, Password: ""}, // Using token as username
		Progress: os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("failed to clone repository %s/%s: %w", c.owner, c.repo, err)
	}
	return nil
}

func (c *githubClient) SyncWithRemote(ctx context.Context) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo) // Assuming working dir is set
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.PullContext(ctx, &git.PullOptions{
		Auth:     &http.BasicAuth{Username: c.token, Password: ""},
		Progress: os.Stdout,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull from remote: %w", err)
	}
	return nil
}

func (c *githubClient) ListFiles(ctx context.Context, path string) ([]string, error) {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)

	var files []string
	err := filepath.Walk(filepath.Join(repoPath, path), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(repoPath, p)
			files = append(files, relPath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files in %s: %w", path, err)
	}
	return files, nil
}

func (c *githubClient) CreateBranch(ctx context.Context, branchName string) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD ref: %w", err)
	}

	newRef := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName))

	err = repo.Storer.SetReference(plumbing.NewHashReference(newRef, headRef.Hash()))
	if err != nil {
		return fmt.Errorf("failed to create local branch %s: %w", branchName, err)
	}
	return nil
}

func (c *githubClient) SwitchBranch(ctx context.Context, branchName string) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName)),
	})
	if err != nil {
		return fmt.Errorf("failed to switch to branch %s: %w", branchName, err)
	}
	return nil
}

func (c *githubClient) AddFile(ctx context.Context, filePath string) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	_, err = w.Add(filePath)
	if err != nil {
		return fmt.Errorf("failed to add file %s: %w", filePath, err)
	}
	return nil
}

func (c *githubClient) Commit(ctx context.Context, message string) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	_, err = w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AI Intern Agent",
			Email: "ai-intern@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit changes: %w", err)
	}
	return nil
}

func (c *githubClient) Push(ctx context.Context, branchName string) error {
	repoPath := filepath.Join(os.Getenv("AGENT_WORKING_DIR"), c.repo)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	err = repo.PushContext(ctx, &git.PushOptions{
		Auth: &http.BasicAuth{Username: c.token, Password: ""},
		RefSpecs: []git.RefSpec{
			git.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName)),
		},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to push to remote: %w", err)
	}
	return nil
}
