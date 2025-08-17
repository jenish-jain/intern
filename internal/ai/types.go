package ai

type CodeChangeOperation string

const (
	OperationCreate CodeChangeOperation = "create"
	OperationUpdate CodeChangeOperation = "update"
)

type CodeChange struct {
	Path      string              `json:"path"`
	Operation CodeChangeOperation `json:"operation"`
	Content   string              `json:"content"`
}
