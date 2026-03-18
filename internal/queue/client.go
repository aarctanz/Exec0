package queue

import (
	"fmt"

	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/hibiken/asynq"
)

type Client struct {
	client *asynq.Client
}

func NewClient(redisAddr string) *Client {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &Client{client: client}
}

func (c *Client) EnqueueSubmission(submissionID int64) error {
	task, err := tasks.NewSubmissionTask(submissionID)
	if err != nil {
		return fmt.Errorf("failed to create submission task: %w", err)
	}

	_, err = c.client.Enqueue(task, asynq.MaxRetry(3))
	if err != nil {
		return fmt.Errorf("failed to enqueue submission task: %w", err)
	}

	return nil
}

func (c *Client) Close() error {
	return c.client.Close()
}
