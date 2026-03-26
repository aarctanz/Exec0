package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/aarctanz/Exec0/internal/metrics"
	"github.com/aarctanz/Exec0/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxBoxID = 999

var boxIDCounter atomic.Int64

func nextBoxID() int {
	return int(boxIDCounter.Add(1) % int64(maxBoxID))
}

type ExecutionService struct {
	queries *queries.Queries
	config  *config.ExecutionConfig
}

func NewExecutionService(queries *queries.Queries, cfg *config.ExecutionConfig) *ExecutionService {
	return &ExecutionService{
		queries: queries,
		config:  cfg,
	}
}

// Metadata holds parsed isolate metadata output.
type Metadata struct {
	Time       float64
	WallTime   float64
	Memory     int32
	ExitCode   int32
	ExitSignal int32
	Status     string
	Message    string
}

// Execute is the main entry point called by the worker.
func (e *ExecutionService) Execute(ctx context.Context, submissionID int64) error {
	ctx, span := telemetry.Tracer("execution").Start(ctx, "Execute",
		trace.WithAttributes(attribute.Int64("submission_id", submissionID)),
	)
	defer span.End()

	startTime := time.Now()
	metrics.WorkerActiveJobs.Inc()
	defer metrics.WorkerActiveJobs.Dec()

	// Correlate logs with trace
	traceID := span.SpanContext().TraceID().String()
	log := logger.FromContext(ctx).With().
		Int64("submission_id", submissionID).
		Str("trace_id", traceID).
		Logger()
	log.Info().Msg("starting execution")

	dbStart := time.Now()
	sub, err := e.queries.GetSubmissionByID(ctx, submissionID)
	metrics.DBOperationDuration.WithLabelValues("get_submission").Observe(time.Since(dbStart).Seconds())
	if err != nil {
		metrics.DBFailuresTotal.WithLabelValues("get_submission").Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch submission")
		log.Error().Err(err).Msg("failed to fetch submission")
		return fmt.Errorf("failed to fetch submission %d: %w", submissionID, err)
	}

	// Queue wait time: time between creation and execution start
	metrics.JobQueueWait.WithLabelValues("default").Observe(startTime.Sub(sub.CreatedAt.Time).Seconds())

	log.Info().
		Int64("language_id", sub.LanguageID).
		Int32("memory_limit_kb", sub.MemoryLimit).
		Float64("cpu_time_limit", sub.CpuTimeLimit).
		Float64("wall_time_limit", sub.WallTimeLimit).
		Int32("max_processes", sub.MaxProcessesAndOrThreads).
		Int32("max_file_size_kb", sub.MaxFileSize).
		Bool("enable_network", sub.EnableNetwork).
		Msg("submission fetched")

	dbStart = time.Now()
	lang, err := e.queries.GetLanguageByID(ctx, sub.LanguageID)
	metrics.DBOperationDuration.WithLabelValues("get_language").Observe(time.Since(dbStart).Seconds())
	if err != nil {
		metrics.DBFailuresTotal.WithLabelValues("get_language").Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch language")
		log.Error().Err(err).Int64("language_id", sub.LanguageID).Msg("failed to fetch language")
		return fmt.Errorf("failed to fetch language %d: %w", sub.LanguageID, err)
	}
	span.SetAttributes(
		attribute.String("language", lang.Name),
		attribute.String("language_version", lang.Version),
	)
	log.Info().
		Str("language", lang.Name).
		Str("version", lang.Version).
		Str("source_file", lang.SourceFile).
		Bool("has_compile", lang.CompileCommand.Valid).
		Msg("language resolved")

	// Fetch test case results for this submission
	testCases, err := e.queries.GetTestCaseResultsBySubmissionID(ctx, submissionID)
	if err != nil || len(testCases) == 0 {
		span.RecordError(err)
		log.Error().Err(err).Msg("failed to fetch test cases")
		return fmt.Errorf("failed to fetch test cases for submission %d: %w", submissionID, err)
	}

	switch sub.Mode {
	case "single":
		return e.executeSingle(ctx, span, &log, sub, lang, testCases[0], startTime)
	case "batch":
		return e.executeBatch(ctx, span, &log, sub, lang, testCases, startTime)
	default:
		return fmt.Errorf("unsupported mode: %s", sub.Mode)
	}
}

func (e *ExecutionService) executeSingle(ctx context.Context, span trace.Span, log *zerolog.Logger, sub queries.Submission, lang queries.GetLanguageByIDRow, tc queries.TestCaseResult, startTime time.Time) error {
	submissionID := sub.ID

	boxID := nextBoxID()
	log2 := log.With().Int("box_id", boxID).Logger()
	log = &log2

	log.Info().Msg("initializing sandbox")
	_, initSpan := telemetry.Tracer("execution").Start(ctx, "sandbox.init",
		trace.WithAttributes(attribute.Int("box_id", boxID)),
	)
	boxDir, err := e.initBox(log, boxID)
	if err != nil {
		initSpan.RecordError(err)
		initSpan.SetStatus(codes.Error, "box init failed")
		initSpan.End()
		metrics.SandboxFailuresTotal.WithLabelValues("init").Inc()
		log.Error().Err(err).Msg("box init failed")
		return fmt.Errorf("failed to init box %d: %w", boxID, err)
	}
	initSpan.End()
	log.Info().Str("box_dir", boxDir).Msg("sandbox ready")
	defer e.cleanupBox(log, boxID)

	stdin := tc.Stdin.String
	log.Info().
		Int("source_bytes", len(sub.SourceCode)).
		Int("stdin_bytes", len(stdin)).
		Msg("writing files")
	if err := e.createFiles(boxDir, sub.SourceCode, lang.SourceFile, stdin); err != nil {
		log.Error().Err(err).Msg("failed to create files")
		return fmt.Errorf("failed to create files in box %d: %w", boxID, err)
	}

	startedAt := time.Now()
	metaFile := filepath.Join(os.TempDir(), fmt.Sprintf("isolate_meta_%d.txt", boxID))
	defer os.Remove(metaFile)

	// Compile if language has a compile command
	if lang.CompileCommand.Valid {
		log.Info().Str("compile_cmd", lang.CompileCommand.String).Msg("compiling")
		e.updateStatus(ctx, log, submissionID, "compiling")

		_, compileSpan := telemetry.Tracer("execution").Start(ctx, "sandbox.compile")
		compileStart := time.Now()
		compileMeta, compileOutput, err := e.compile(log, boxID, lang.CompileCommand.String, metaFile, sub)
		metrics.JobDuration.WithLabelValues("compile", lang.Name).Observe(time.Since(compileStart).Seconds())
		if err != nil {
			compileSpan.RecordError(err)
			compileSpan.SetStatus(codes.Error, "compile failed")
			compileSpan.End()
			metrics.SandboxFailuresTotal.WithLabelValues("compile").Inc()
			log.Error().Err(err).Msg("compile step failed")
			return fmt.Errorf("compilation error in box %d: %w", boxID, err)
		}

		log.Info().
			Int32("exit_code", compileMeta.ExitCode).
			Str("status", compileMeta.Status).
			Float64("time_s", compileMeta.Time).
			Int32("memory_kb", compileMeta.Memory).
			Str("output", truncate(compileOutput, 200)).
			Msg("compile done")

		if compileMeta.Status != "" || compileMeta.ExitCode != 0 {
			compileSpan.SetAttributes(attribute.String("result", "compilation_error"))
			compileSpan.End()
			finishedAt := time.Now()
			log.Warn().Msg("compilation error, completing submission")
			e.updateTestCaseResult(ctx, log, tc.ID, "compilation_error", "", "", compileMeta)
			span.SetAttributes(attribute.String("status", "compilation_error"))
			metrics.JobsProcessedTotal.WithLabelValues("compilation_error", lang.Name).Inc()
			metrics.JobDuration.WithLabelValues("total", lang.Name).Observe(time.Since(startTime).Seconds())
			return e.completeSubmission(ctx, log, sub.ID, "compilation_error", compileOutput, compileMeta, startedAt, finishedAt)
		}

		compileSpan.SetAttributes(attribute.String("result", "success"))
		compileSpan.End()
		log.Info().Msg("compilation succeeded, resetting metadata")
		os.Remove(metaFile)
	}

	// Run
	log.Info().Str("run_cmd", lang.RunCommand).Msg("running")
	e.updateStatus(ctx, log, submissionID, "running")

	_, runSpan := telemetry.Tracer("execution").Start(ctx, "sandbox.run")
	runStart := time.Now()
	runMeta, err := e.run(log, boxID, lang.RunCommand, metaFile, sub)
	metrics.JobDuration.WithLabelValues("run", lang.Name).Observe(time.Since(runStart).Seconds())
	if err != nil {
		runSpan.RecordError(err)
		runSpan.SetStatus(codes.Error, "run failed")
		runSpan.End()
		metrics.SandboxFailuresTotal.WithLabelValues("run").Inc()
		log.Error().Err(err).Msg("run step failed")
		return fmt.Errorf("run error in box %d: %w", boxID, err)
	}
	runSpan.End()

	finishedAt := time.Now()

	log.Info().
		Int32("exit_code", runMeta.ExitCode).
		Int32("exit_signal", runMeta.ExitSignal).
		Str("status", runMeta.Status).
		Float64("time_s", runMeta.Time).
		Float64("wall_time_s", runMeta.WallTime).
		Int32("memory_kb", runMeta.Memory).
		Str("message", runMeta.Message).
		Msg("run done")

	stdout, stdoutTruncated := e.readOutputWithLimit(log, filepath.Join(boxDir, "box", "stdout.txt"), e.config.MaxStdoutBytes)
	stderr, stderrTruncated := e.readOutputWithLimit(log, filepath.Join(boxDir, "box", "stderr.txt"), e.config.MaxStderrBytes)
	log.Info().Int("stdout_bytes", len(stdout)).Bool("stdout_truncated", stdoutTruncated).Int("stderr_bytes", len(stderr)).Bool("stderr_truncated", stderrTruncated).Msg("output read")

	// Determine status
	status := "accepted"
	if runMeta.Status != "" {
		status = e.mapIsolateStatus(runMeta.Status, runMeta, sub.MemoryLimit)
	} else if tc.ExpectedOutput.Valid && strings.TrimSpace(string(stdout)) != strings.TrimSpace(tc.ExpectedOutput.String) {
		status = "wrong_answer"
	}

	// Update test case result
	e.updateTestCaseResult(ctx, log, tc.ID, status, string(stdout), string(stderr), runMeta)

	log.Info().
		Str("status", status).
		Float64("total_duration_s", finishedAt.Sub(startedAt).Seconds()).
		Msg("completing submission")

	span.SetAttributes(
		attribute.String("status", status),
		attribute.Int("stdout_bytes", len(stdout)),
		attribute.Int("stderr_bytes", len(stderr)),
		attribute.Float64("time_s", runMeta.Time),
		attribute.Float64("wall_time_s", runMeta.WallTime),
		attribute.Int64("memory_kb", int64(runMeta.Memory)),
	)

	metrics.JobsProcessedTotal.WithLabelValues(status, lang.Name).Inc()
	metrics.JobDuration.WithLabelValues("total", lang.Name).Observe(time.Since(startTime).Seconds())

	log.Info().
		Float64("execution_time_s", time.Since(startTime).Seconds()).
		Msg("execution complete")
	return e.completeSubmission(ctx, log, sub.ID, status, "", runMeta, startedAt, finishedAt)
}

func (e *ExecutionService) executeBatch(ctx context.Context, span trace.Span, log *zerolog.Logger, sub queries.Submission, lang queries.GetLanguageByIDRow, testCases []queries.TestCaseResult, startTime time.Time) error {
	submissionID := sub.ID
	startedAt := time.Now()

	// Temp dir for storing compiled artifacts
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("exec0-%d-", submissionID))
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: Compile (if needed)
	if lang.CompileCommand.Valid {
		log.Info().Str("compile_cmd", lang.CompileCommand.String).Msg("compiling")
		e.updateStatus(ctx, log, submissionID, "compiling")

		compileBoxID := nextBoxID()
		compileLog := log.With().Int("box_id", compileBoxID).Logger()

		_, compileSpan := telemetry.Tracer("execution").Start(ctx, "sandbox.compile")
		boxDir, err := e.initBox(&compileLog, compileBoxID)
		if err != nil {
			compileSpan.RecordError(err)
			compileSpan.SetStatus(codes.Error, "box init failed")
			compileSpan.End()
			metrics.SandboxFailuresTotal.WithLabelValues("init").Inc()
			return fmt.Errorf("failed to init compile box %d: %w", compileBoxID, err)
		}

		// Write source code (no stdin needed for compilation)
		if err := e.createFiles(boxDir, sub.SourceCode, lang.SourceFile, ""); err != nil {
			e.cleanupBox(&compileLog, compileBoxID)
			compileSpan.End()
			return fmt.Errorf("failed to create files in compile box: %w", err)
		}

		metaFile := filepath.Join(os.TempDir(), fmt.Sprintf("isolate_meta_%d.txt", compileBoxID))
		compileStart := time.Now()
		compileMeta, compileOutput, err := e.compile(&compileLog, compileBoxID, lang.CompileCommand.String, metaFile, sub)
		metrics.JobDuration.WithLabelValues("compile", lang.Name).Observe(time.Since(compileStart).Seconds())
		os.Remove(metaFile)

		if err != nil {
			compileSpan.RecordError(err)
			compileSpan.SetStatus(codes.Error, "compile failed")
			compileSpan.End()
			e.cleanupBox(&compileLog, compileBoxID)
			metrics.SandboxFailuresTotal.WithLabelValues("compile").Inc()
			return fmt.Errorf("compilation error in box %d: %w", compileBoxID, err)
		}

		if compileMeta.Status != "" || compileMeta.ExitCode != 0 {
			compileSpan.SetAttributes(attribute.String("result", "compilation_error"))
			compileSpan.End()
			e.cleanupBox(&compileLog, compileBoxID)
			finishedAt := time.Now()
			// Mark all test cases as compilation_error
			for _, tc := range testCases {
				e.updateTestCaseResult(ctx, log, tc.ID, "compilation_error", "", "", compileMeta)
			}
			span.SetAttributes(attribute.String("status", "compilation_error"))
			metrics.JobsProcessedTotal.WithLabelValues("compilation_error", lang.Name).Inc()
			metrics.JobDuration.WithLabelValues("total", lang.Name).Observe(time.Since(startTime).Seconds())
			return e.completeSubmission(ctx, log, submissionID, "compilation_error", compileOutput, compileMeta, startedAt, finishedAt)
		}

		compileSpan.SetAttributes(attribute.String("result", "success"))
		compileSpan.End()
		log.Info().Msg("compilation succeeded, saving artifacts")

		// Copy compiled artifacts from box to temp dir
		boxPath := filepath.Join(boxDir, "box")
		if err := copyDir(boxPath, tmpDir); err != nil {
			e.cleanupBox(&compileLog, compileBoxID)
			return fmt.Errorf("failed to copy compiled artifacts: %w", err)
		}
		e.cleanupBox(&compileLog, compileBoxID)
	} else {
		// Interpreted language: just save source file to temp dir
		if err := os.WriteFile(filepath.Join(tmpDir, lang.SourceFile), []byte(sub.SourceCode), 0644); err != nil {
			return fmt.Errorf("failed to write source to temp dir: %w", err)
		}
	}

	// Step 2: Run each test case in a fresh box
	e.updateStatus(ctx, log, submissionID, "running")

	var totalTime, totalWallTime float64
	var maxMemory int32
	overallStatus := "accepted"

	for i, tc := range testCases {
		tcLog := log.With().Int("test_case", i+1).Int16("position", tc.Position).Logger()
		_, tcSpan := telemetry.Tracer("execution").Start(ctx, fmt.Sprintf("sandbox.run.tc%d", i+1),
			trace.WithAttributes(attribute.Int("position", int(tc.Position))),
		)

		runBoxID := nextBoxID()
		boxDir, err := e.initBox(&tcLog, runBoxID)
		if err != nil {
			tcSpan.RecordError(err)
			tcSpan.End()
			metrics.SandboxFailuresTotal.WithLabelValues("init").Inc()
			tcLog.Error().Err(err).Msg("box init failed for test case")
			e.updateTestCaseResult(ctx, &tcLog, tc.ID, "internal_error", "", "", &Metadata{})
			overallStatus = "internal_error"
			continue
		}

		// Copy artifacts into box
		boxPath := filepath.Join(boxDir, "box")
		if err := copyDir(tmpDir, boxPath); err != nil {
			tcSpan.End()
			e.cleanupBox(&tcLog, runBoxID)
			tcLog.Error().Err(err).Msg("failed to copy artifacts")
			e.updateTestCaseResult(ctx, &tcLog, tc.ID, "internal_error", "", "", &Metadata{})
			overallStatus = "internal_error"
			continue
		}

		// Write stdin
		stdin := tc.Stdin.String
		if err := os.WriteFile(filepath.Join(boxPath, "stdin.txt"), []byte(stdin), 0644); err != nil {
			tcSpan.End()
			e.cleanupBox(&tcLog, runBoxID)
			tcLog.Error().Err(err).Msg("failed to write stdin")
			e.updateTestCaseResult(ctx, &tcLog, tc.ID, "internal_error", "", "", &Metadata{})
			overallStatus = "internal_error"
			continue
		}

		metaFile := filepath.Join(os.TempDir(), fmt.Sprintf("isolate_meta_%d.txt", runBoxID))
		runStart := time.Now()
		runMeta, err := e.run(&tcLog, runBoxID, lang.RunCommand, metaFile, sub)
		metrics.JobDuration.WithLabelValues("run", lang.Name).Observe(time.Since(runStart).Seconds())
		os.Remove(metaFile)

		if err != nil {
			tcSpan.RecordError(err)
			tcSpan.End()
			e.cleanupBox(&tcLog, runBoxID)
			metrics.SandboxFailuresTotal.WithLabelValues("run").Inc()
			tcLog.Error().Err(err).Msg("run failed for test case")
			e.updateTestCaseResult(ctx, &tcLog, tc.ID, "internal_error", "", "", &Metadata{})
			overallStatus = "internal_error"
			continue
		}

		stdout, _ := e.readOutputWithLimit(&tcLog, filepath.Join(boxPath, "stdout.txt"), e.config.MaxStdoutBytes)
		stderr, _ := e.readOutputWithLimit(&tcLog, filepath.Join(boxPath, "stderr.txt"), e.config.MaxStderrBytes)
		e.cleanupBox(&tcLog, runBoxID)

		// Determine per-test-case status
		tcStatus := "accepted"
		if runMeta.Status != "" {
			tcStatus = e.mapIsolateStatus(runMeta.Status, runMeta, sub.MemoryLimit)
		} else if tc.ExpectedOutput.Valid && strings.TrimSpace(string(stdout)) != strings.TrimSpace(tc.ExpectedOutput.String) {
			tcStatus = "wrong_answer"
		}

		e.updateTestCaseResult(ctx, &tcLog, tc.ID, tcStatus, string(stdout), string(stderr), runMeta)

		tcSpan.SetAttributes(attribute.String("status", tcStatus))
		tcSpan.End()

		tcLog.Info().
			Str("status", tcStatus).
			Float64("time_s", runMeta.Time).
			Int32("memory_kb", runMeta.Memory).
			Msg("test case completed")

		// Aggregate metrics
		totalTime += runMeta.Time
		totalWallTime += runMeta.WallTime
		if runMeta.Memory > maxMemory {
			maxMemory = runMeta.Memory
		}

		// Track first non-AC as overall status
		if overallStatus == "accepted" && tcStatus != "accepted" {
			overallStatus = tcStatus
		}
	}

	finishedAt := time.Now()

	// Build aggregated metadata for submission completion
	aggMeta := &Metadata{
		Time:     totalTime,
		WallTime: totalWallTime,
		Memory:   maxMemory,
	}

	log.Info().
		Str("status", overallStatus).
		Float64("total_time_s", totalTime).
		Int32("max_memory_kb", maxMemory).
		Int("test_cases", len(testCases)).
		Msg("batch execution complete")

	span.SetAttributes(
		attribute.String("status", overallStatus),
		attribute.Float64("time_s", totalTime),
		attribute.Float64("wall_time_s", totalWallTime),
		attribute.Int64("memory_kb", int64(maxMemory)),
		attribute.Int("test_case_count", len(testCases)),
	)

	metrics.JobsProcessedTotal.WithLabelValues(overallStatus, lang.Name).Inc()
	metrics.JobDuration.WithLabelValues("total", lang.Name).Observe(time.Since(startTime).Seconds())

	return e.completeSubmission(ctx, log, submissionID, overallStatus, "", aggMeta, startedAt, finishedAt)
}

// copyDir copies all files from src directory to dst directory (non-recursive, files only).
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// readOutputWithLimit reads a file and truncates if it exceeds the size limit.
// Returns the content (possibly truncated) and a flag indicating if truncation occurred.
func (e *ExecutionService) readOutputWithLimit(log *zerolog.Logger, path string, maxBytes int) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to read output file")
		return []byte{}, false
	}

	if len(data) > maxBytes {
		log.Warn().Str("path", filepath.Base(path)).Int("size_bytes", len(data)).Int("max_bytes", maxBytes).Msg("output truncated due to size limit")
		return append(data[:maxBytes], []byte("...")...), true
	}

	return data, false
}

// mapIsolateStatus converts isolate status codes to submission statuses.
// For signal kills (SG), checks if memory exceeded the limit to classify as memory_limit_exceeded.
func (e *ExecutionService) mapIsolateStatus(isolateStatus string, meta *Metadata, memoryLimitKB int32) string {
	switch isolateStatus {
	case "TO":
		return "time_limit_exceeded"
	case "SG":
		if meta.ExitSignal == 9 && meta.Memory >= memoryLimitKB {
			return "memory_limit_exceeded"
		}
		return "runtime_error"
	case "RE":
		return "runtime_error"
	case "XX":
		return "internal_error"
	default:
		return "runtime_error"
	}
}

// initBox creates an isolate sandbox and returns the box directory path.
func (e *ExecutionService) initBox(log *zerolog.Logger, boxID int) (string, error) {
	id := strconv.Itoa(boxID)
	cmd := exec.Command("isolate", "--box-id", id, "--cg", "--init")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	log.Warn().Int("box_id", boxID).Msg("init failed, attempting stale box cleanup")
	exec.Command("isolate", "--box-id", id, "--cg", "--cleanup").Run()
	exec.Command("isolate", "--box-id", id, "--cleanup").Run()

	cmd = exec.Command("isolate", "--box-id", id, "--cg", "--init")
	output, err = cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Error().Str("stderr", string(exitErr.Stderr)).Msg("isolate --init failed")
		}
		return "", fmt.Errorf("isolate --init failed after cleanup: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// createFiles writes the source code and stdin into the box directory.
func (e *ExecutionService) createFiles(boxDir, sourceCode, sourceFile, stdin string) error {
	boxPath := filepath.Join(boxDir, "box")

	if err := os.WriteFile(filepath.Join(boxPath, sourceFile), []byte(sourceCode), 0644); err != nil {
		return fmt.Errorf("failed to write source file: %w", err)
	}

	if err := os.WriteFile(filepath.Join(boxPath, "stdin.txt"), []byte(stdin), 0644); err != nil {
		return fmt.Errorf("failed to write stdin file: %w", err)
	}

	return nil
}

// buildIsolateArgs constructs common isolate arguments from submission resource limits.
func (e *ExecutionService) buildIsolateArgs(boxID int, metaFile string, sub queries.Submission) []string {
	args := []string{
		"--box-id", strconv.Itoa(boxID),
		"--meta", metaFile,
		"--cg",
		"--cg-mem", strconv.Itoa(int(sub.MemoryLimit)),
		"--dir=/etc:maybe",
		"--fsize", strconv.Itoa(int(sub.MaxFileSize)),
		fmt.Sprintf("--processes=%d", sub.MaxProcessesAndOrThreads),
	}

	// Note: --cg-timing is not supported in isolate 2.2.1.
	// With --cg enabled, --time already measures CPU time of the main process.
	// EnablePerProcessAndThreadTimeLimit is acknowledged but has no additional effect.

	if sub.EnablePerProcessAndThreadMemoryLimit {
		args = append(args, "--mem", strconv.Itoa(int(sub.MemoryLimit)))
	}

	if sub.EnableNetwork {
		args = append(args, "--share-net")
	}

	return args
}

// Fixed compile-time resource limits — independent of submission limits.
const (
	compileCPUTime  = 5.0    // seconds
	compileWallTime = 30.0   // seconds — generous for concurrent javac under CPU contention
	compileMemoryKB = 512000 // 512MB
)

// compile runs the compile command inside isolate.
func (e *ExecutionService) compile(log *zerolog.Logger, boxID int, compileCmd, metaFile string, sub queries.Submission) (*Metadata, string, error) {
	resolvedCmd := fmt.Sprintf(compileCmd, "")

	args := []string{
		"--box-id", strconv.Itoa(boxID),
		"--meta", metaFile,
		"--cg",
		"--cg-mem", strconv.Itoa(compileMemoryKB),
		"--dir=/etc:maybe",
		"--fsize", strconv.Itoa(int(sub.MaxFileSize)),
		fmt.Sprintf("--processes=%d", sub.MaxProcessesAndOrThreads),
		"--time", fmt.Sprintf("%.1f", compileCPUTime),
		"--wall-time", fmt.Sprintf("%.1f", compileWallTime),
		"--stderr-to-stdout",
		"--run", "--",
		"/bin/bash", "-c", resolvedCmd,
	}

	log.Debug().Str("cmd", "isolate "+strings.Join(args, " ")).Msg("compile command")

	cmd := exec.Command("isolate", args...)
	output, cmdErr := cmd.CombinedOutput()

	if cmdErr != nil {
		log.Warn().Err(cmdErr).Str("output", truncate(string(output), 500)).Msg("compile exited with error")
		if exitErr, ok := cmdErr.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return nil, string(output), fmt.Errorf("isolate configuration error: %s", truncate(string(output), 500))
		}
	}

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		log.Error().Err(err).Str("isolate_error", fmt.Sprint(cmdErr)).Str("output", truncate(string(output), 500)).Msg("failed to read compile metadata")
		return nil, string(output), fmt.Errorf("failed to read compile metadata (isolate error: %v, output: %s): %w", cmdErr, string(output), err)
	}

	return meta, string(output), nil
}

// run executes the program inside isolate with the submission's resource limits.
func (e *ExecutionService) run(log *zerolog.Logger, boxID int, runCmd, metaFile string, sub queries.Submission) (*Metadata, error) {
	args := e.buildIsolateArgs(boxID, metaFile, sub)
	args = append(args,
		"--time", fmt.Sprintf("%.1f", sub.CpuTimeLimit),
		"--extra-time", fmt.Sprintf("%.1f", sub.CpuExtraTime),
		"--wall-time", fmt.Sprintf("%.1f", sub.WallTimeLimit),
		"--stack", strconv.Itoa(int(sub.StackLimit)),
		"--stdin", "stdin.txt",
		"--stdout", "stdout.txt",
		"--stderr", "stderr.txt",
	)

	if sub.RedirectStderrToStdout {
		args = append(args, "--stderr-to-stdout")
	}

	args = append(args, "--run", "--", "/bin/bash", "-c", runCmd)

	log.Debug().Str("cmd", "isolate "+strings.Join(args, " ")).Msg("run command")

	cmd := exec.Command("isolate", args...)
	output, cmdErr := cmd.CombinedOutput()

	if cmdErr != nil {
		log.Warn().Err(cmdErr).Str("output", truncate(string(output), 500)).Msg("run exited with error")
		// Exit code 2 = isolate usage/config error (not a sandboxed program failure)
		if exitErr, ok := cmdErr.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return nil, fmt.Errorf("isolate configuration error: %s", truncate(string(output), 500))
		}
	}

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		log.Error().Err(err).Str("isolate_error", fmt.Sprint(cmdErr)).Str("output", truncate(string(output), 500)).Msg("failed to read run metadata")
		return nil, fmt.Errorf("failed to read run metadata (isolate error: %v, output: %s): %w", cmdErr, string(output), err)
	}

	return meta, nil
}

// readMetadata parses an isolate metadata file (key:value per line).
func (e *ExecutionService) readMetadata(metaFile string) (*Metadata, error) {
	file, err := os.Open(metaFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata file: %w", err)
	}
	defer file.Close()

	meta := &Metadata{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		switch key {
		case "time":
			meta.Time, _ = strconv.ParseFloat(value, 64)
		case "time-wall":
			meta.WallTime, _ = strconv.ParseFloat(value, 64)
		case "max-rss":
			v, _ := strconv.ParseInt(value, 10, 32)
			meta.Memory = int32(v)
		case "exitcode":
			v, _ := strconv.ParseInt(value, 10, 32)
			meta.ExitCode = int32(v)
		case "exitsig":
			v, _ := strconv.ParseInt(value, 10, 32)
			meta.ExitSignal = int32(v)
		case "status":
			meta.Status = value
		case "message":
			meta.Message = value
		}
	}

	return meta, scanner.Err()
}

// cleanupBox destroys the isolate sandbox.
func (e *ExecutionService) cleanupBox(log *zerolog.Logger, boxID int) {
	log.Info().Msg("cleaning up sandbox")
	cmd := exec.Command("isolate", "--box-id", strconv.Itoa(boxID), "--cg", "--cleanup")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("cleanup failed")
	}
}

// updateStatus updates submission status in the DB.
func (e *ExecutionService) updateStatus(ctx context.Context, log *zerolog.Logger, submissionID int64, status string) {
	log.Info().Str("status", status).Msg("status update")
	err := e.queries.UpdateSubmissionStatus(ctx, queries.UpdateSubmissionStatusParams{
		ID:     submissionID,
		Status: status,
	})
	if err != nil {
		log.Error().Err(err).Str("status", status).Msg("failed to update submission status")
	}
}

// completeSubmission writes final results back to the DB with retry.
func (e *ExecutionService) completeSubmission(ctx context.Context, log *zerolog.Logger, submissionID int64, status, compileOutput string, meta *Metadata, startedAt, finishedAt time.Time) error {
	log.Info().
		Str("status", status).
		Float64("time_s", meta.Time).
		Int32("memory_kb", meta.Memory).
		Msg("completing submission")

	params := queries.CompleteSubmissionParams{
		ID:            submissionID,
		Status:        status,
		CompileOutput: pgtype.Text{String: compileOutput, Valid: compileOutput != ""},
		Message:       pgtype.Text{String: meta.Message, Valid: meta.Message != ""},
		InternalError: pgtype.Text{},
		Time:          pgtype.Float8{Float64: meta.Time, Valid: true},
		WallTime:      pgtype.Float8{Float64: meta.WallTime, Valid: true},
		Memory:        pgtype.Int4{Int32: meta.Memory, Valid: true},
		StartedAt:     pgtype.Timestamptz{Time: startedAt, Valid: true},
		FinishedAt:    pgtype.Timestamptz{Time: finishedAt, Valid: true},
	}

	var err error
	for attempt := 0; attempt < 3; attempt++ {
		_, err = e.queries.CompleteSubmission(ctx, params)
		if err == nil {
			return nil
		}
		log.Warn().Err(err).Int("attempt", attempt+1).Msg("failed to complete submission, retrying")
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	log.Error().Err(err).Msg("failed to complete submission after retries")
	return fmt.Errorf("failed to complete submission %d: %w", submissionID, err)
}

// updateTestCaseResult writes per-test-case execution results to the DB.
func (e *ExecutionService) updateTestCaseResult(ctx context.Context, log *zerolog.Logger, tcID int64, status, stdout, stderr string, meta *Metadata) {
	_, err := e.queries.UpdateTestCaseResult(ctx, queries.UpdateTestCaseResultParams{
		ID:         tcID,
		Stdout:     pgtype.Text{String: stdout, Valid: stdout != ""},
		Stderr:     pgtype.Text{String: stderr, Valid: stderr != ""},
		ExitCode:   pgtype.Int4{Int32: meta.ExitCode, Valid: true},
		ExitSignal: pgtype.Int4{Int32: meta.ExitSignal, Valid: meta.ExitSignal != 0},
		Status:     status,
		Time:       pgtype.Float8{Float64: meta.Time, Valid: true},
		WallTime:   pgtype.Float8{Float64: meta.WallTime, Valid: true},
		Memory:     pgtype.Int4{Int32: meta.Memory, Valid: true},
	})
	if err != nil {
		log.Error().Err(err).Int64("test_case_id", tcID).Msg("failed to update test case result")
	}
}

// truncate shortens a string for log output.
func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
