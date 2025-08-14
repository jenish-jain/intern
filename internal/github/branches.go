package github

import (
	"context"
	"fmt"
)

func (c *githubClient) CreateFeatureBranch(ctx context.Context, branchName string) error {
	// Placeholder: actual branch creation would use go-git or exec git
	if !isValidBranchName(branchName) {
		return fmt.Errorf("invalid branch name: %s", branchName)
	}
	return fmt.Errorf("CreateFeatureBranch not implemented: use go-git or exec git")
}

func (c *githubClient) SwitchBranch(ctx context.Context, branchName string) error {
	// Placeholder: actual branch switching would use go-git or exec git
	return fmt.Errorf("SwitchBranch not implemented: use go-git or exec git")
}

func isValidBranchName(name string) bool {
	// Simple convention: must not be empty and must not contain spaces
	return name != "" && !containsSpace(name)
}

func containsSpace(s string) bool {
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return false
}
