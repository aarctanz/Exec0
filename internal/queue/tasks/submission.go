package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
)

const TypeSubmissionExecute = "submission:execute"

type SubmissionPayload struct {
	SubmissionID int64 `json:"submission_id"`
}

func NewSubmissionTask(submissionID int64) (*asynq.Task, error) {
	payload, err := json.Marshal(SubmissionPayload{SubmissionID: submissionID})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal submission payload: %w", err)
	}
	return asynq.NewTask(TypeSubmissionExecute, payload), nil
}

func HandleSubmissionExecute(_ context.Context, t *asynq.Task) error {
	var payload SubmissionPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal submission payload: %w", err)
	}

	// TODO: fetch submission from DB, execute code, update status
	log.Printf("Processing submission %d", payload.SubmissionID)

	return nil
}
