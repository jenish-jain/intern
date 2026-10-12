// Package personas holds the role instructions given to agent runners.
// They ship inside the intern binary so they are versioned with the
// orchestrator that depends on their output contract.
package personas

import (
	_ "embed"
	"fmt"

	"intern/internal/runner"
)

//go:embed developer.md
var developer string

//go:embed reviewer.md
var reviewer string

//go:embed tester.md
var tester string

//go:embed architect.md
var architect string

// For returns the system prompt addition for a role.
func For(role runner.Role) (string, error) {
	switch role {
	case runner.RoleDeveloper:
		return developer, nil
	case runner.RoleReviewer:
		return reviewer, nil
	case runner.RoleTester:
		return tester, nil
	case runner.RoleArchitect:
		return architect, nil
	default:
		return "", fmt.Errorf("no persona defined for role %q", role)
	}
}
