package github

import (
	"context"
	"errors"
	"intern/internal/github/mocks"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	go_github "github.com/google/go-github/v58/github" // Alias go-github
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/mock/gomock"
)

// MockGitHubClient is a mock for the github.Client interface
type MockGitHubClient struct {
	mock.Mock
}

func (m *MockGitHubClient) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockGitHubClient) Raw() *go_github.Client {
	args := m.Called()
	return args.Get(0).(*go_github.Client)
}

func (m *MockGitHubClient) CloneRepository(ctx context.Context, destPath string) error {
	args := m.Called(ctx, destPath)
	return args.Error(0)
}

func (m *MockGitHubClient) SyncWithRemote(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockGitHubClient) ListFiles(ctx context.Context, path string) ([]string, error) {
	args := m.Called(ctx, path)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitHubClient) CreateBranch(ctx context.Context, branchName string) error {
	args := m.Called(ctx, branchName)
	return args.Error(0)
}

func (m *MockGitHubClient) SwitchBranch(ctx context.Context, branchName string) error {
	args := m.Called(ctx, branchName)
	return args.Error(0)
}

func TestNewClient(t *testing.T) {
	c := NewClient("token", "owner", "repo")
	if c == nil {
		t.Fatal("expected client, got nil")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	mock.EXPECT().HealthCheck(gomock.Any()).Return(nil)
	if err := mock.HealthCheck(context.Background()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mock := mocks.NewMockClient(ctrl)
	mock.EXPECT().HealthCheck(gomock.Any()).Return(errors.New("fail"))
	if err := mock.HealthCheck(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestRaw(t *testing.T) {
	c := NewClient("token", "owner", "repo")
	if c.Raw() == nil {
		t.Error("expected non-nil Raw client")
	}
}

func TestCloneRepository(t *testing.T) {
	// Create a temporary directory for the cloned repository
	tempDir, err := ioutil.TempDir("", "test-clone-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set a dummy AGENT_WORKING_DIR for the test
	originalWorkingDir := os.Getenv("AGENT_WORKING_DIR")
	os.Setenv("AGENT_WORKING_DIR", tempDir)
	defer os.Setenv("AGENT_WORKING_DIR", originalWorkingDir)

	// Create a dummy remote repository for cloning
	remoteRepoDir, err := ioutil.TempDir("", "test-remote-")
	assert.NoError(t, err)
	defer os.RemoveAll(remoteRepoDir)

	r, err := git.PlainInit(remoteRepoDir, false)
	assert.NoError(t, err)
	w, err := r.Worktree()
	assert.NoError(t, err)

	// Commit a dummy file to the remote repo
	filePath := filepath.Join(remoteRepoDir, "testfile.txt")
	err = ioutil.WriteFile(filePath, []byte("hello world"), 0644)
	assert.NoError(t, err)

	_, err = w.Add("testfile.txt")
	assert.NoError(t, err)

	_, err = w.Commit("initial commit", &git.CommitOptions{})
	assert.NoError(t, err)

	// Create a githubClient instance
	c := NewClient("dummy_token", "test_owner", "test_repo") // token is not used in local clone

	// Perform the clone operation
	clonePath := filepath.Join(tempDir, "test_repo")
	err = c.CloneRepository(context.Background(), clonePath)
	assert.NoError(t, err)

	// Verify the repository was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	assert.NoError(t, err)

	// Verify file exists
	content, err := ioutil.ReadFile(filepath.Join(clonePath, "testfile.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestSyncWithRemote(t *testing.T) {
	// Setup similar to TestCloneRepository, then add a new commit to remote
	tempDir, err := ioutil.TempDir("", "test-sync-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set a dummy AGENT_WORKING_DIR
	originalWorkingDir := os.Getenv("AGENT_WORKING_DIR")
	os.Setenv("AGENT_WORKING_DIR", tempDir)
	defer os.Setenv("AGENT_WORKING_DIR", originalWorkingDir)

	remoteRepoDir, err := ioutil.TempDir("", "test-remote-sync-")
	assert.NoError(t, err)
	defer os.RemoveAll(remoteRepoDir)

	r, err := git.PlainInit(remoteRepoDir, false)
	assert.NoError(t, err)
	w, err := r.Worktree()
	assert.NoError(t, err)
	// Initial commit in remote
	ioutil.WriteFile(filepath.Join(remoteRepoDir, "initial.txt"), []byte("initial"), 0644)
	_, err = w.Add("initial.txt")
	assert.NoError(t, err)
	_, err = w.Commit("initial commit", &git.CommitOptions{})
	assert.NoError(t, err)

	// Clone to local
	clonePath := filepath.Join(tempDir, "test_repo_sync")
	_, err = git.PlainClone(clonePath, false, &git.CloneOptions{
		URL:      remoteRepoDir,
		Progress: os.Stdout,
	})
	assert.NoError(t, err)

	// Add a new commit to the remote after cloning
	ioutil.WriteFile(filepath.Join(remoteRepoDir, "newfile.txt"), []byte("new content"), 0644)
	w.Add("newfile.txt")
	_, err = w.Commit("new commit", &git.CommitOptions{})
	assert.NoError(t, err)

	// Create a githubClient instance
	c := NewClient("dummy_token", "test_owner", "test_repo_sync")

	// Perform sync
	err = c.SyncWithRemote(context.Background())
	assert.NoError(t, err)

	// Verify new file is in local repo
	content, err := ioutil.ReadFile(filepath.Join(clonePath, "newfile.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "new content", string(content))
}

func TestListFiles(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test-list-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set a dummy AGENT_WORKING_DIR
	originalWorkingDir := os.Getenv("AGENT_WORKING_DIR")
	os.Setenv("AGENT_WORKING_DIR", tempDir)
	defer os.Setenv("AGENT_WORKING_DIR", originalWorkingDir)

	repoDir := filepath.Join(tempDir, "test_repo_list")
	_, err = git.PlainInit(repoDir, false)
	assert.NoError(t, err)

	// Create dummy files
	_ = os.MkdirAll(filepath.Join(repoDir, "dir1"), 0755)
	_ = ioutil.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("content"), 0644)
	_ = ioutil.WriteFile(filepath.Join(repoDir, "dir1", "file2.txt"), []byte("content"), 0644)

	c := NewClient("dummy_token", "test_owner", "test_repo_list")
	files, err := c.ListFiles(context.Background(), "")
	assert.NoError(t, err)

	expectedFiles := []string{
		"file1.txt",
		filepath.Join("dir1", "file2.txt"),
	}
	assert.ElementsMatch(t, expectedFiles, files)
}

func TestCreateBranch(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test-branch-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set a dummy AGENT_WORKING_DIR
	originalWorkingDir := os.Getenv("AGENT_WORKING_DIR")
	os.Setenv("AGENT_WORKING_DIR", tempDir)
	defer os.Setenv("AGENT_WORKING_DIR", originalWorkingDir)

	repoDir := filepath.Join(tempDir, "test_repo_branch")
	r, err := git.PlainInit(repoDir, false)
	assert.NoError(t, err)
	w, err := r.Worktree()
	assert.NoError(t, err)
	// Initial commit to have a HEAD
	ioutil.WriteFile(filepath.Join(repoDir, "initial.txt"), []byte("initial"), 0644)
	_, err = w.Add("initial.txt")
	assert.NoError(t, err)
	_, err = w.Commit("initial commit", &git.CommitOptions{})
	assert.NoError(t, err)

	c := NewClient("dummy_token", "test_owner", "test_repo_branch")

	err = c.CreateBranch(context.Background(), "new-feature")
	assert.NoError(t, err)

	// Verify branch exists locally
	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/new-feature"), true)
	assert.NoError(t, err)
	assert.NotNil(t, ref)
}

func TestSwitchBranch(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test-switch-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set a dummy AGENT_WORKING_DIR
	originalWorkingDir := os.Getenv("AGENT_WORKING_DIR")
	os.Setenv("AGENT_WORKING_DIR", tempDir)
	defer os.Setenv("AGENT_WORKING_DIR", originalWorkingDir)

	repoDir := filepath.Join(tempDir, "test_repo_switch")
	r, err := git.PlainInit(repoDir, false)
	assert.NoError(t, err)
	w, err := r.Worktree()
	assert.NoError(t, err)
	// Initial commit on master
	ioutil.WriteFile(filepath.Join(repoDir, "master.txt"), []byte("master"), 0644)
	_, err = w.Add("master.txt")
	assert.NoError(t, err)
	_, err = w.Commit("master commit", &git.CommitOptions{})
	assert.NoError(t, err)

	// Create a new branch and commit a file to it
	newBranchRef := plumbing.ReferenceName("refs/heads/feature-x")
	newBranchHash, err := w.Commit("feature commit", &git.CommitOptions{})
	assert.NoError(t, err)
	_ = r.Storer.SetReference(plumbing.NewHashReference(newBranchRef, newBranchHash))

	// Create a githubClient instance
	c := NewClient("dummy_token", "test_owner", "test_repo_switch")

	// Switch to the new branch
	err = c.SwitchBranch(context.Background(), "feature-x")
	assert.NoError(t, err)

	// Verify current branch is feature-x
	head, err := r.Head()
	assert.NoError(t, err)
	assert.Equal(t, newBranchRef, head.Name())
}
