package services

import (
	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
)

type Services struct {
	LanguagesService   *LanguagesService
	SubmissionsService *SubmissionsService
}

func New(queries *queries.Queries) *Services {
	languagesService := NewLanguagesService(queries)
	executionConfig := config.DefaultExecutionConfig()

	submissionsService := NewSubmissionsService(queries, languagesService, executionConfig)
	return &Services{
		LanguagesService:   languagesService,
		SubmissionsService: submissionsService,
	}
}
