package ticketing

type Ticket struct {
	ID          string
	Key         string
	Summary     string
	Description string
	Status      string
	// StatusCategory is the backend's coarse status bucket. For JIRA this
	// is the statusCategory key: "new", "indeterminate" or "done".
	StatusCategory string
	// Blockers are tickets this one is blocked by (JIRA "is blocked by"
	// links). The orchestrator holds a ticket until every blocker is done.
	Blockers []Blocker
	Priority string
	Assignee string
	Reporter string
	URL      string
}

var PriorityMap = map[string]int{
	"Highest": 1,
	"High":    2,
	"Medium":  3,
	"Low":     4,
	"Lowest":  5,
}

var Statuses = []string{
	"To Do",
	"In Progress",
	"Done",
	"Blocked",
}

// Canonical workflow statuses the orchestrator transitions tickets to.
// Backends resolve these by name (see Client.UpdateTicketStatus).
const (
	StatusToDo       = "To Do"
	StatusInProgress = "In Progress"
	StatusInReview   = "In Review"
	StatusDone       = "Done"
)

// StatusCategoryDone is the StatusCategory value for finished tickets.
const StatusCategoryDone = "done"

// NewTicket describes a ticket to create via Workflow.CreateTicket.
type NewTicket struct {
	Project     string
	Summary     string
	Description string
	IssueType   string // e.g. "Task", "Story"; defaults to "Task"
	Labels      []string
}

// Blocker is a ticket that must finish before another can start.
type Blocker struct {
	Key  string
	Done bool // blocker's status category is done
}

// OpenBlockers returns the keys of blockers that are not done yet.
func (t Ticket) OpenBlockers() []string {
	var open []string
	for _, b := range t.Blockers {
		if !b.Done {
			open = append(open, b.Key)
		}
	}
	return open
}
