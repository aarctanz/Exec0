package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/metrics"
	"github.com/aarctanz/Exec0/internal/models/submissions"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/telemetry"
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

func (s *SubmissionsService) ListSubmissions(ctx context.Context, page, perPage int32) ([]queries.Submission, error) {
	ctx, span := telemetry.Tracer("submissions").Start(ctx, "ListSubmissions")
	defer span.End()

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
	subs, err := s.queries.ListSubmissions(ctx, queries.ListSubmissionsParams{
		Limit:  perPage,
		Offset: offset,
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if subs == nil {
		subs = []queries.Submission{}
	}
	span.SetAttributes(attribute.Int("result_count", len(subs)))
	return subs, nil
}

func (s *SubmissionsService) GetSubmissionById(ctx context.Context, submissionID int) (queries.Submission, error) {
	ctx, span := telemetry.Tracer("submissions").Start(ctx, "GetSubmissionById")
	defer span.End()
	span.SetAttributes(attribute.Int("submission_id", submissionID))

	sub, err := s.queries.GetSubmissionByID(ctx, int64(submissionID))
	if err != nil {
		span.RecordError(err)
		return queries.Submission{}, err
	}
	return sub, nil
}

func (s *SubmissionsService) CreateSubmission(ctx context.Context, dto submissions.CreateSubmissionDTO) (int64, error) {
	ctx, span := telemetry.Tracer("submissions").Start(ctx, "CreateSubmission")
	defer span.End()
	span.SetAttributes(attribute.Int64("language_id", dto.LanguageID))

	if dto.LanguageID == 0 {
		return 0, errors.New("language_id is required")
	}
	if dto.SourceCode == "" {
		return 0, errors.New("source_code is required")
	}

	params := queries.CreateSubmissionParams{
		LanguageID: dto.LanguageID,
		SourceCode: dto.SourceCode,
		Mode:       "single",
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

	dbStart := time.Now()
	sub, err := s.queries.CreateSubmission(ctx, params)
	metrics.DBOperationDuration.WithLabelValues("create_submission").Observe(time.Since(dbStart).Seconds())
	if err != nil {
		metrics.DBFailuresTotal.WithLabelValues("create_submission").Inc()
		return 0, err
	}

	// Create single test case result
	stdin := ""
	if dto.Stdin != nil {
		stdin = *dto.Stdin
	}
	expectedOutput := ""
	if dto.ExpectedOutput != nil {
		expectedOutput = *dto.ExpectedOutput
	}
	_, err = s.queries.CreateTestCaseResult(ctx, queries.CreateTestCaseResultParams{
		SubmissionID:   sub.ID,
		Position:       1,
		Stdin:          pgtype.Text{String: stdin, Valid: true},
		ExpectedOutput: pgtype.Text{String: expectedOutput, Valid: expectedOutput != ""},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create test case result: %w", err)
	}

	// Resolve language name for metric label
	langName := "unknown"
	if lang, err := s.languagesService.GetLanguageByID(ctx, dto.LanguageID); err == nil {
		langName = lang.Name
	}
	metrics.SubmissionsCreatedTotal.WithLabelValues(langName).Inc()

	if err := s.queueClient.EnqueueSubmission(ctx, sub.ID); err != nil {
		metrics.EnqueueFailuresTotal.Inc()
		return 0, fmt.Errorf("failed to enqueue submission: %w", err)
	}

	return sub.ID, nil
}

func (s *SubmissionsService) CreateBatchSubmission(ctx context.Context, dto submissions.CreateBatchSubmissionDTO) (int64, error) {
	ctx, span := telemetry.Tracer("submissions").Start(ctx, "CreateBatchSubmission")
	defer span.End()
	span.SetAttributes(attribute.Int64("language_id", dto.LanguageID), attribute.Int("test_case_count", len(dto.TestCases)))

	if dto.LanguageID == 0 {
		return 0, errors.New("language_id is required")
	}
	if dto.SourceCode == "" {
		return 0, errors.New("source_code is required")
	}
	if len(dto.TestCases) == 0 {
		return 0, errors.New("test_cases is required")
	}

	params := queries.CreateSubmissionParams{
		LanguageID: dto.LanguageID,
		SourceCode: dto.SourceCode,
		Mode:       "batch",
	}

	params.CpuTimeLimit = valOrDefault(dto.CpuTimeLimit, s.executionConfig.DefaultCPUTime, s.executionConfig.MaxCPUTime)
	params.CpuExtraTime = s.executionConfig.DefaultCPUExtraTime
	params.WallTimeLimit = valOrDefault(dto.WallTimeLimit, s.executionConfig.DefaultWallTime, s.executionConfig.MaxWallTime)
	params.MemoryLimit = valOrDefaultInt(dto.MemoryLimit, s.executionConfig.DefaultMemoryKB, s.executionConfig.MaxMemoryKB)
	params.StackLimit = valOrDefaultInt(dto.StackLimit, s.executionConfig.DefaultStackKB, s.executionConfig.MaxStackKB)
	params.MaxProcessesAndOrThreads = valOrDefaultInt(dto.MaxProcessesAndOrThreads, s.executionConfig.DefaultMaxProcessesAndThreads, s.executionConfig.MaxMaxProcessesAndThreads)
	params.MaxFileSize = valOrDefaultInt(dto.MaxFileSize, s.executionConfig.DefaultMaxFileSizeKB, s.executionConfig.MaxMaxFileSizeKB)
	params.EnablePerProcessAndThreadTimeLimit = boolOrDefault(dto.EnablePerProcessAndThreadTimeLimit, false)
	params.EnablePerProcessAndThreadMemoryLimit = boolOrDefault(dto.EnablePerProcessAndThreadMemoryLimit, false)
	params.RedirectStderrToStdout = boolOrDefault(dto.RedirectStderrToStdout, false)
	if dto.EnableNetwork != nil {
		params.EnableNetwork = *dto.EnableNetwork && s.executionConfig.AllowEnableNetwork
	} else {
		params.EnableNetwork = s.executionConfig.DefaultEnableNetwork
	}

	dbStart := time.Now()
	sub, err := s.queries.CreateSubmission(ctx, params)
	metrics.DBOperationDuration.WithLabelValues("create_submission").Observe(time.Since(dbStart).Seconds())
	if err != nil {
		metrics.DBFailuresTotal.WithLabelValues("create_submission").Inc()
		return 0, err
	}

	// Create test case results for each test case
	for i, tc := range dto.TestCases {
		expectedOutput := ""
		hasExpected := false
		if tc.ExpectedOutput != nil {
			expectedOutput = *tc.ExpectedOutput
			hasExpected = expectedOutput != ""
		}
		_, err = s.queries.CreateTestCaseResult(ctx, queries.CreateTestCaseResultParams{
			SubmissionID:   sub.ID,
			Position:       int16(i + 1),
			Stdin:          pgtype.Text{String: tc.Stdin, Valid: true},
			ExpectedOutput: pgtype.Text{String: expectedOutput, Valid: hasExpected},
		})
		if err != nil {
			return 0, fmt.Errorf("failed to create test case result %d: %w", i+1, err)
		}
	}

	langName := "unknown"
	if lang, err := s.languagesService.GetLanguageByID(ctx, dto.LanguageID); err == nil {
		langName = lang.Name
	}
	metrics.SubmissionsCreatedTotal.WithLabelValues(langName).Inc()

	if err := s.queueClient.EnqueueSubmission(ctx, sub.ID); err != nil {
		metrics.EnqueueFailuresTotal.Inc()
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

func (s *SubmissionsService) GetTestCaseResults(ctx context.Context, submissionID int64) ([]map[string]any, error) {
	tcs, err := s.queries.GetTestCaseResultsBySubmissionID(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, len(tcs))
	for i, tc := range tcs {
		results[i] = map[string]any{
			"id":              tc.ID,
			"position":        tc.Position,
			"stdin":           tc.Stdin.String,
			"expected_output": tc.ExpectedOutput.String,
			"stdout":          tc.Stdout.String,
			"stderr":          tc.Stderr.String,
			"exit_code":       tc.ExitCode.Int32,
			"exit_signal":     tc.ExitSignal.Int32,
			"status":          tc.Status,
			"time":            tc.Time.Float64,
			"wall_time":       tc.WallTime.Float64,
			"memory":          tc.Memory.Int32,
		}
	}
	return results, nil
}

func (s *SubmissionsService) CompleteSubmission(ctx context.Context, arg queries.CompleteSubmissionParams) {
	s.queries.CompleteSubmission(ctx, arg)
}

func (s *SubmissionsService) UpdateSubmissionStatus(ctx context.Context, submissionID int, status string) {
	arg := queries.UpdateSubmissionStatusParams{
		ID:     int64(submissionID),
		Status: status,
	}
	s.queries.UpdateSubmissionStatus(ctx, arg)
}
