package github

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"intern/internal/repository"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gh "github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

type githubClient struct {
	ghClient *gh.Client
	owner    string
	repo     string
	token    string // Store the token for git operations
}

func NewClient(token, owner, repo string) repository.RepositoryClient {
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

	refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName)
	err = repo.PushContext(ctx, &git.PushOptions{
		Auth:     &http.BasicAuth{Username: c.token, Password: ""},
		RefSpecs: []config.RefSpec{config.RefSpec(refspec)},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to push to remote: %w", err)
	}
	return nil
}

func (c *githubClient) CreatePullRequest(ctx context.Context, baseBranch, headBranch, title, body string) (string, error) {
	// Determine a valid base branch: prefer provided, else repo default
	base := baseBranch
	if base == "" {
		repo, _, err := c.ghClient.Repositories.Get(ctx, c.owner, c.repo)
		if err == nil && repo != nil && repo.GetDefaultBranch() != "" {
			base = repo.GetDefaultBranch()
		}
	}
	// If still empty or invalid, try to validate/fallback
	if base == "" {
		base = "main"
	}
	// Validate base exists; if not, fallback to repo default if available
	if _, _, err := c.ghClient.Git.GetRef(ctx, c.owner, c.repo, fmt.Sprintf("refs/heads/%s", base)); err != nil {
		repo, _, rerr := c.ghClient.Repositories.Get(ctx, c.owner, c.repo)
		if rerr == nil && repo != nil && repo.GetDefaultBranch() != "" {
			base = repo.GetDefaultBranch()
		}
	}

	newPR := &gh.NewPullRequest{
		Title: gh.String(title),
		Head:  gh.String(headBranch),
		Base:  gh.String(base),
		Body:  gh.String(body),
	}
	pr, _, err := c.ghClient.PullRequests.Create(ctx, c.owner, c.repo, newPR)
	if err != nil {
		return "", fmt.Errorf("failed to create pull request: %w", err)
	}
	if pr == nil || pr.GetHTMLURL() == "" {
		return "", fmt.Errorf("pull request created but URL missing")
	}
	return pr.GetHTMLURL(), nil
}
