package queue

import (
	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/hibiken/asynq"
)

func NewServer(redisAddr string, concurrency int) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency:    concurrency,
			RetryDelayFunc: asynq.DefaultRetryDelayFunc,
		},
	)
}

// NewServeMux registers task handlers and returns a mux ready to be
// passed to server.Run(). Same pattern as http.ServeMux — each task
// type maps to a handler function.
func NewServeMux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeSubmissionExecute, tasks.HandleSubmissionExecute)
	return mux
}
