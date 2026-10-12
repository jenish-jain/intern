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
	Priority       string
	Assignee       string
	Reporter       string
	URL            string
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
