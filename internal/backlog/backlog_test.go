package backlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `
epics:
  - id: SETUP
    title: Project skeleton
    tickets:
      - id: SETUP-1
        title: Scaffold the service
        description: Create the module layout and CI.
        acceptance_criteria: ["go test ./... passes on an empty project"]
        size: S
  - id: AUTH
    title: Authentication
    goal: Users can sign up and log in.
    tickets:
      - id: AUTH-2
        title: Login endpoint
        description: POST /login returns a session token.
        acceptance_criteria: ["valid credentials return 200 and a token", "invalid ones return 401"]
        blocked_by: [AUTH-1]
        size: M
      - id: AUTH-1
        title: User store
        description: Persist users with hashed passwords.
        acceptance_criteria: ["passwords are never stored in plain text"]
        blocked_by: [SETUP-1]
`

func load(t *testing.T, content string) Backlog {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, Path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	b, found, err := Load(dir)
	if err != nil || !found {
		t.Fatalf("Load: found %v err %v", found, err)
	}
	return b
}

func ids(ts []Ticket) string {
	var s []string
	for _, t := range ts {
		s = append(s, t.ID)
	}
	return strings.Join(s, ",")
}

func TestValidateAndOrder(t *testing.T) {
	b := load(t, sample)
	if err := b.Validate(nil); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	order, err := b.Order(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(order); got != "SETUP-1,AUTH-1,AUTH-2" {
		t.Errorf("order = %s", got)
	}
	if order[1].Epic != "AUTH" {
		t.Errorf("epic not filled: %+v", order[1])
	}

	// Already-imported tickets are skipped and still satisfy dependencies.
	order, err = b.Order(map[string]string{"SETUP-1": "P-10", "AUTH-1": "P-11"})
	if err != nil || ids(order) != "AUTH-2" {
		t.Errorf("order with known = %s, %v", ids(order), err)
	}
}

func TestValidateProblems(t *testing.T) {
	bad := `
epics:
  - id: E
    title: Epic
    tickets:
      - id: A
        title: A
        description: d
        acceptance_criteria: [x]
        blocked_by: [B]
      - id: B
        title: B
        description: d
        acceptance_criteria: [x]
        blocked_by: [A]
      - id: C
        title: ""
        description: ""
        acceptance_criteria: []
        blocked_by: [ZZZ]
        size: XL
`
	err := load(t, bad).Validate(nil)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{`ticket "C" has no title`, `"C" has no acceptance criteria`, `unknown ticket "ZZZ"`, "size must be S, M or L"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}

	cyc := strings.Replace(bad, "      - id: C\n        title: \"\"\n        description: \"\"\n        acceptance_criteria: []\n        blocked_by: [ZZZ]\n        size: XL\n", "", 1)
	if err := load(t, cyc).Validate(nil); err == nil || !strings.Contains(err.Error(), "dependency cycle among tickets: A, B") {
		t.Errorf("expected cycle error, got %v", err)
	}
}

func TestDependencyOnKnownTicket(t *testing.T) {
	b := load(t, `
epics:
  - id: E
    title: Epic
    tickets:
      - id: NEW-1
        title: t
        description: d
        acceptance_criteria: [x]
        blocked_by: [OLD-1]
`)
	if err := b.Validate(nil); err == nil {
		t.Error("unknown dependency should fail without known tickets")
	}
	if err := b.Validate(map[string]string{"OLD-1": "P-1"}); err != nil {
		t.Errorf("dependency on an imported ticket should pass: %v", err)
	}
}

func TestRendering(t *testing.T) {
	b := load(t, sample)
	desc := b.Epics[1].Tickets[0].TicketDescription()
	for _, want := range []string{"POST /login", "Acceptance criteria:\n- valid credentials", "Epic: AUTH | Size: M"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}
	sum := b.Summary(map[string]string{"SETUP-1": "P-10"})
	for _, want := range []string{"*AUTH: Authentication*", "• AUTH-2 Login endpoint [M] (after AUTH-1)", "already P-10", "3 tickets in 2 epics (1 already created, 2 new)"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	if _, found, err := Load(t.TempDir()); found || err != nil {
		t.Errorf("found %v err %v", found, err)
	}
}
