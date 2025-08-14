package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

type Client interface {
	HealthCheck(ctx context.Context) error
	Raw() *gh.Client
}

type githubClient struct {
	ghClient *gh.Client
	owner    string
	repo     string
}

func NewClient(token, owner, repo string) Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := gh.NewClient(oauth2.NewClient(context.Background(), ts))
	return &githubClient{
		ghClient: client,
		owner:    owner,
		repo:     repo,
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
