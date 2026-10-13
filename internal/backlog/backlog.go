// Package backlog reads and validates the architect's backlog: epics and
// tickets with acceptance criteria and dependencies, proposed in
// .intern/backlog.yaml and imported into the ticketing system once a
// human approves them.
package backlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Path is the backlog file's location relative to the repo root.
const Path = ".intern/backlog.yaml"

// Ticket is one unit of work: small enough for one pull request.
type Ticket struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
	BlockedBy          []string `yaml:"blocked_by,omitempty"`
	Size               string   `yaml:"size,omitempty"` // S, M or L

	Epic string `yaml:"-"` // filled from the enclosing epic
}

// Epic groups tickets that deliver one capability.
type Epic struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Goal    string   `yaml:"goal,omitempty"`
	Tickets []Ticket `yaml:"tickets"`
}

// Backlog is the whole proposal.
type Backlog struct {
	Epics []Epic `yaml:"epics"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

// Load reads the backlog from repoRoot. found is false when there is none.
func Load(repoRoot string) (b Backlog, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, Path))
	if errors.Is(err, os.ErrNotExist) {
		return Backlog{}, false, nil
	}
	if err != nil {
		return Backlog{}, false, fmt.Errorf("read %s: %w", Path, err)
	}
	if err := yaml.Unmarshal(data, &b); err != nil {
		return Backlog{}, true, fmt.Errorf("parse %s: %w", Path, err)
	}
	for i := range b.Epics {
		for j := range b.Epics[i].Tickets {
			b.Epics[i].Tickets[j].Epic = b.Epics[i].ID
		}
	}
	return b, true, nil
}

// Tickets returns every ticket in file order.
func (b Backlog) Tickets() []Ticket {
	var out []Ticket
	for _, e := range b.Epics {
		out = append(out, e.Tickets...)
	}
	return out
}

// Validate checks the backlog is importable. known maps ticket IDs that
// were already imported to their ticket keys; dependencies may point at
// them.
func (b Backlog) Validate(known map[string]string) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	ids := map[string]bool{}
	epicIDs := map[string]bool{}
	if len(b.Epics) == 0 {
		add("the backlog has no epics")
	}
	for _, e := range b.Epics {
		if !idPattern.MatchString(e.ID) {
			add("epic %q: id must be letters, digits, - or _ and start with a letter", e.ID)
		}
		if epicIDs[e.ID] {
			add("epic %q appears twice", e.ID)
		}
		epicIDs[e.ID] = true
		if strings.TrimSpace(e.Title) == "" {
			add("epic %q has no title", e.ID)
		}
		if len(e.Tickets) == 0 {
			add("epic %q has no tickets", e.ID)
		}
		for _, t := range e.Tickets {
			if !idPattern.MatchString(t.ID) {
				add("ticket %q: id must be letters, digits, - or _ and start with a letter", t.ID)
			}
			if ids[t.ID] {
				add("ticket %q appears twice", t.ID)
			}
			ids[t.ID] = true
			if strings.TrimSpace(t.Title) == "" {
				add("ticket %q has no title", t.ID)
			}
			if strings.TrimSpace(t.Description) == "" {
				add("ticket %q has no description", t.ID)
			}
			if len(t.AcceptanceCriteria) == 0 {
				add("ticket %q has no acceptance criteria", t.ID)
			}
			switch t.Size {
			case "", "S", "M", "L":
			default:
				add("ticket %q: size must be S, M or L", t.ID)
			}
		}
	}
	for _, t := range b.Tickets() {
		for _, dep := range t.BlockedBy {
			if dep == t.ID {
				add("ticket %q is blocked by itself", t.ID)
			} else if !ids[dep] && known[dep] == "" {
				add("ticket %q is blocked by unknown ticket %q", t.ID, dep)
			}
		}
	}
	if len(problems) == 0 {
		if _, err := b.Order(known); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("backlog is invalid:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

// Order returns the tickets not yet in known, ordered so every ticket
// comes after the tickets that block it. It fails on dependency cycles.
func (b Backlog) Order(known map[string]string) ([]Ticket, error) {
	byID := map[string]Ticket{}
	var pending []string
	for _, t := range b.Tickets() {
		if known[t.ID] != "" {
			continue
		}
		byID[t.ID] = t
		pending = append(pending, t.ID)
	}
	done := map[string]bool{}
	for id := range known {
		done[id] = true
	}
	var out []Ticket
	for len(pending) > 0 {
		progressed := false
		var rest []string
		for _, id := range pending {
			ready := true
			for _, dep := range byID[id].BlockedBy {
				if !done[dep] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, byID[id])
				done[id] = true
				progressed = true
			} else {
				rest = append(rest, id)
			}
		}
		if !progressed {
			sort.Strings(rest)
			return nil, fmt.Errorf("dependency cycle among tickets: %s", strings.Join(rest, ", "))
		}
		pending = rest
	}
	return out, nil
}

// TicketDescription renders a ticket's description with its acceptance
// criteria, as the developer, reviewer and tester will read it.
func (t Ticket) TicketDescription() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(t.Description))
	b.WriteString("\n\nAcceptance criteria:\n")
	for _, c := range t.AcceptanceCriteria {
		b.WriteString("- " + strings.TrimSpace(c) + "\n")
	}
	if t.Epic != "" {
		b.WriteString("\nEpic: " + t.Epic)
		if t.Size != "" {
			b.WriteString(" | Size: " + t.Size)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Summary renders the backlog for a person to approve, marking tickets
// that already exist.
func (b Backlog) Summary(known map[string]string) string {
	var sb strings.Builder
	total, existing := 0, 0
	for _, e := range b.Epics {
		fmt.Fprintf(&sb, "*%s: %s*\n", e.ID, e.Title)
		if e.Goal != "" {
			fmt.Fprintf(&sb, "_%s_\n", e.Goal)
		}
		for _, t := range e.Tickets {
			total++
			line := fmt.Sprintf("• %s %s", t.ID, t.Title)
			if t.Size != "" {
				line += " [" + t.Size + "]"
			}
			if len(t.BlockedBy) > 0 {
				line += " (after " + strings.Join(t.BlockedBy, ", ") + ")"
			}
			if key := known[t.ID]; key != "" {
				existing++
				line += " — already " + key
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "%d tickets in %d epics", total, len(b.Epics))
	if existing > 0 {
		fmt.Fprintf(&sb, " (%d already created, %d new)", existing, total-existing)
	}
	return sb.String()
}
