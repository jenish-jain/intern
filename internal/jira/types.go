package jira

type Ticket struct {
	ID          string
	Key         string
	Summary     string
	Description string
	Status      string
	Priority    string
	Assignee    string
	Reporter    string
	URL         string
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
