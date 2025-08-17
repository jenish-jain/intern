package repository

import (
	"context"
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
