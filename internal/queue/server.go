package queue

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"

	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/metrics"
	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
)

func NewServer(redisAddr string, concurrency int, dbQueries *queries.Queries) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency:    concurrency,
			RetryDelayFunc: asynq.DefaultRetryDelayFunc,
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				metrics.JobRetriesTotal.WithLabelValues("execution_error").Inc()
				retried, _ := asynq.GetRetryCount(ctx)
				maxRetry, _ := asynq.GetMaxRetry(ctx)
				if retried >= maxRetry {
					var payload tasks.SubmissionPayload
					if jsonErr := json.Unmarshal(task.Payload(), &payload); jsonErr != nil {
						log.Error().Err(jsonErr).Msg("failed to unmarshal payload in error handler")
						return
					}
					log.Error().
						Int64("submission_id", payload.SubmissionID).
						Err(err).
						Msg("submission exhausted all retries, marking as internal_error")
					dbQueries.CompleteSubmission(ctx, queries.CompleteSubmissionParams{
						ID:            payload.SubmissionID,
						Status:        "internal_error",
						InternalError: pgtype.Text{String: err.Error(), Valid: true},
					})
				}
			}),
		},
	)
}

// NewServeMux registers task handlers and returns a mux.
func NewServeMux(submissionHandler func(context.Context, *asynq.Task) error) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeSubmissionExecute, submissionHandler)
	return mux
}
