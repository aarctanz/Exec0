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

// SubmissionPayload carries the submission ID and trace context for
// distributed tracing propagation through the queue.
type SubmissionPayload struct {
	SubmissionID int64             `json:"submission_id"`
	TraceCarrier map[string]string `json:"trace_carrier,omitempty"`
}

// NewSubmissionTask creates an asynq.Task ready to be enqueued.
func NewSubmissionTask(submissionID int64, traceCarrier map[string]string) (*asynq.Task, error) {
	payload, err := json.Marshal(SubmissionPayload{
		SubmissionID: submissionID,
		TraceCarrier: traceCarrier,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal submission payload: %w", err)
	}
	return asynq.NewTask(TypeSubmissionExecute, payload), nil
}
