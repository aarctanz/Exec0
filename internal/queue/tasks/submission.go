package tasks

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// Task type name — acts as the routing key in asynq.
// When a task with this type is dequeued, asynq routes it to the handler
// registered under this name.
const TypeSubmissionExecute = "submission:execute"

// SubmissionPayload carries only the submission ID.
// The worker fetches the full submission from the DB rather than
// serializing source code into Redis.
type SubmissionPayload struct {
	SubmissionID int64 `json:"submission_id"`
}

// NewSubmissionTask creates an asynq.Task ready to be enqueued.
func NewSubmissionTask(submissionID int64) (*asynq.Task, error) {
	payload, err := json.Marshal(SubmissionPayload{SubmissionID: submissionID})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal submission payload: %w", err)
	}
	return asynq.NewTask(TypeSubmissionExecute, payload), nil
}
