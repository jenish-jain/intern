package slack

// ThreadRef identifies the Slack conversation a ticket originated from, so
// status updates and results can be replied back into the right thread.
type ThreadRef struct {
	Channel  string
	ThreadTS string
}
