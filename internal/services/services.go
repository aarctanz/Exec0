package services

import (
	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/queue"
)

type Services struct {
	LanguagesService   *LanguagesService
	SubmissionsService *SubmissionsService
}

func New(queries *queries.Queries, queueClient *queue.Client) *Services {
	languagesService := NewLanguagesService(queries)
	executionConfig := config.DefaultExecutionConfig()

	submissionsService := NewSubmissionsService(queries, languagesService, executionConfig, queueClient)
	return &Services{
		LanguagesService:   languagesService,
		SubmissionsService: submissionsService,
	}
}
