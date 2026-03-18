package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/models/submissions"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/jackc/pgx/v5/pgtype"
)

type SubmissionsService struct {
	queries          *queries.Queries
	languagesService *LanguagesService
	executionConfig  *config.ExecutionConfig
	queueClient      *queue.Client
}

func NewSubmissionsService(queries *queries.Queries, languageService *LanguagesService, executionConfig *config.ExecutionConfig, queueClient *queue.Client) *SubmissionsService {
	return &SubmissionsService{
		queries:          queries,
		languagesService: languageService,
		executionConfig:  executionConfig,
		queueClient:      queueClient,
	}
}

func (s *SubmissionsService) ListSubmissions(page, perPage int32) ([]queries.Submission, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage
	return s.queries.ListSubmissions(context.Background(), queries.ListSubmissionsParams{
		Limit:  perPage,
		Offset: offset,
	})
}

func (s *SubmissionsService) GetSubmissionById(submissionID int) (queries.Submission, error) {
	sub, err := s.queries.GetSubmissionByID(context.Background(), int64(submissionID))
	if err != nil {
		return queries.Submission{}, err
	}
	return sub, nil
}

func (s *SubmissionsService) CreateSubmission(dto submissions.CreateSubmissionDTO) (int64, error) {
	if dto.LanguageID == 0 {
		return 0, errors.New("language_id is required")
	}
	if dto.SourceCode == "" {
		return 0, errors.New("source_code is required")
	}

	params := queries.CreateSubmissionParams{
		LanguageID: dto.LanguageID,
		SourceCode: dto.SourceCode,
	}

	// Stdin
	if dto.Stdin == nil {
		params.Stdin = pgtype.Text{String: "", Valid: true}
	} else {
		params.Stdin = pgtype.Text{String: *dto.Stdin, Valid: true}
	}

	// CPU time limit
	params.CpuTimeLimit = valOrDefault(dto.CpuTimeLimit, s.executionConfig.DefaultCPUTime, s.executionConfig.MaxCPUTime)

	// CPU extra time (not exposed in DTO, use default)
	params.CpuExtraTime = s.executionConfig.DefaultCPUExtraTime

	// Wall time limit
	params.WallTimeLimit = valOrDefault(dto.WallTimeLimit, s.executionConfig.DefaultWallTime, s.executionConfig.MaxWallTime)

	// Memory limit
	params.MemoryLimit = valOrDefaultInt(dto.MemoryLimit, s.executionConfig.DefaultMemoryKB, s.executionConfig.MaxMemoryKB)

	// Stack limit
	params.StackLimit = valOrDefaultInt(dto.StackLimit, s.executionConfig.DefaultStackKB, s.executionConfig.MaxStackKB)

	// Max processes/threads
	params.MaxProcessesAndOrThreads = valOrDefaultInt(dto.MaxProcessesAndOrThreads, s.executionConfig.DefaultMaxProcessesAndThreads, s.executionConfig.MaxMaxProcessesAndThreads)

	// Max file size
	params.MaxFileSize = valOrDefaultInt(dto.MaxFileSize, s.executionConfig.DefaultMaxFileSizeKB, s.executionConfig.MaxMaxFileSizeKB)

	// Bool configs
	params.EnablePerProcessAndThreadTimeLimit = boolOrDefault(dto.EnablePerProcessAndThreadTimeLimit, false)
	params.EnablePerProcessAndThreadMemoryLimit = boolOrDefault(dto.EnablePerProcessAndThreadMemoryLimit, false)
	params.RedirectStderrToStdout = boolOrDefault(dto.RedirectStderrToStdout, false)

	// Network: use default, but disallow if not permitted
	if dto.EnableNetwork != nil {
		params.EnableNetwork = *dto.EnableNetwork && s.executionConfig.AllowEnableNetwork
	} else {
		params.EnableNetwork = s.executionConfig.DefaultEnableNetwork
	}

	sub, err := s.queries.CreateSubmission(context.Background(), params)
	if err != nil {
		return 0, err
	}

	if err := s.queueClient.EnqueueSubmission(sub.ID); err != nil {
		return 0, fmt.Errorf("failed to enqueue submission: %w", err)
	}

	return sub.ID, nil
}

func valOrDefault(val *float64, def, max float64) float64 {
	if val == nil {
		return def
	}
	if *val > max {
		return max
	}
	return *val
}

func valOrDefaultInt(val *int32, def, max int32) int32 {
	if val == nil {
		return def
	}
	if *val > max {
		return max
	}
	return *val
}

func boolOrDefault(val *bool, def bool) bool {
	if val == nil {
		return def
	}
	return *val
}

func (s *SubmissionsService) CompleteSubmission(arg queries.CompleteSubmissionParams) {
	s.queries.CompleteSubmission(context.Background(), arg)
}

func (s *SubmissionsService) UpdateSubmissionStatus(submissionID int, status string) {
	arg := queries.UpdateSubmissionStatusParams{
		ID:     int64(submissionID),
		Status: status,
	}
	s.queries.UpdateSubmissionStatus(context.Background(), arg)
}
