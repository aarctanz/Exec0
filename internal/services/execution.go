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
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxBoxID = 999

// boxPool manages allocation of isolate box IDs to prevent collisions.
type boxPool struct {
	mu   sync.Mutex
	used map[int]bool
}

var globalBoxPool = &boxPool{used: make(map[int]bool)}

func (p *boxPool) acquire(hint int64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := int(hint % int64(maxBoxID))
	if !p.used[id] {
		p.used[id] = true
		return id
	}
	for i := 0; i < maxBoxID; i++ {
		if !p.used[i] {
			p.used[i] = true
			return i
		}
	}
	return -1
}

func (p *boxPool) release(id int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, id)
}

type ExecutionService struct {
	queries *queries.Queries
}

func NewExecutionService(queries *queries.Queries) *ExecutionService {
	return &ExecutionService{
		queries: queries,
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
	startTime := time.Now()
	log := logger.FromContext(ctx).With().Int64("submission_id", submissionID).Logger()
	log.Info().Msg("starting execution")

	sub, err := e.queries.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch submission")
		return fmt.Errorf("failed to fetch submission %d: %w", submissionID, err)
	}
	log.Info().
		Int64("language_id", sub.LanguageID).
		Int32("memory_limit_kb", sub.MemoryLimit).
		Float64("cpu_time_limit", sub.CpuTimeLimit).
		Float64("wall_time_limit", sub.WallTimeLimit).
		Int32("max_processes", sub.MaxProcessesAndOrThreads).
		Int32("max_file_size_kb", sub.MaxFileSize).
		Bool("enable_network", sub.EnableNetwork).
		Msg("submission fetched")

	lang, err := e.queries.GetLanguageByID(ctx, sub.LanguageID)
	if err != nil {
		log.Error().Err(err).Int64("language_id", sub.LanguageID).Msg("failed to fetch language")
		return fmt.Errorf("failed to fetch language %d: %w", sub.LanguageID, err)
	}
	log.Info().
		Str("language", lang.Name).
		Str("version", lang.Version).
		Str("source_file", lang.SourceFile).
		Bool("has_compile", lang.CompileCommand.Valid).
		Msg("language resolved")

	boxID := globalBoxPool.acquire(submissionID)
	if boxID < 0 {
		log.Error().Msg("no free box IDs available")
		return fmt.Errorf("no free box IDs available")
	}
	defer globalBoxPool.release(boxID)

	log = log.With().Int("box_id", boxID).Logger()

	log.Info().Msg("initializing sandbox")
	boxDir, err := e.initBox(&log, boxID)
	if err != nil {
		log.Error().Err(err).Msg("box init failed")
		return fmt.Errorf("failed to init box %d: %w", boxID, err)
	}
	log.Info().Str("box_dir", boxDir).Msg("sandbox ready")
	defer e.cleanupBox(&log, boxID)

	log.Info().
		Int("source_bytes", len(sub.SourceCode)).
		Int("stdin_bytes", len(sub.Stdin.String)).
		Msg("writing files")
	if err := e.createFiles(boxDir, sub.SourceCode, lang.SourceFile, sub.Stdin.String); err != nil {
		log.Error().Err(err).Msg("failed to create files")
		return fmt.Errorf("failed to create files in box %d: %w", boxID, err)
	}

	startedAt := time.Now()
	metaFile := filepath.Join(os.TempDir(), fmt.Sprintf("isolate_meta_%d.txt", boxID))
	defer os.Remove(metaFile)

	// Compile if language has a compile command
	if lang.CompileCommand.Valid {
		log.Info().Str("compile_cmd", lang.CompileCommand.String).Msg("compiling")
		e.updateStatus(ctx, &log, submissionID, "compiling")

		compileMeta, compileOutput, err := e.compile(&log, boxID, lang.CompileCommand.String, metaFile, sub)
		if err != nil {
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
			finishedAt := time.Now()
			log.Warn().Msg("compilation error, completing submission")
			e.completeSubmission(ctx, &log, sub.ID, "compilation_error", compileOutput, "", "", compileMeta, startedAt, finishedAt)
			return nil
		}

		log.Info().Msg("compilation succeeded, resetting metadata")
		os.Remove(metaFile)
	}

	// Run
	log.Info().Str("run_cmd", lang.RunCommand).Msg("running")
	e.updateStatus(ctx, &log, submissionID, "running")

	runMeta, err := e.run(&log, boxID, lang.RunCommand, metaFile, sub)
	if err != nil {
		log.Error().Err(err).Msg("run step failed")
		return fmt.Errorf("run error in box %d: %w", boxID, err)
	}

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

	stdout, _ := os.ReadFile(filepath.Join(boxDir, "box", "stdout.txt"))
	stderr, _ := os.ReadFile(filepath.Join(boxDir, "box", "stderr.txt"))
	log.Info().Int("stdout_bytes", len(stdout)).Int("stderr_bytes", len(stderr)).Msg("output read")

	status := "accepted"
	if runMeta.Status != "" {
		status = e.mapIsolateStatus(runMeta.Status)
	}

	log.Info().
		Str("status", status).
		Float64("total_duration_s", finishedAt.Sub(startedAt).Seconds()).
		Msg("completing submission")
	e.completeSubmission(ctx, &log, sub.ID, status, "", string(stdout), string(stderr), runMeta, startedAt, finishedAt)
	log.Info().
		Float64("execution_time_s", time.Since(startTime).Seconds()).
		Msg("execution complete")
	return nil
}

// mapIsolateStatus converts isolate status codes to submission statuses.
func (e *ExecutionService) mapIsolateStatus(isolateStatus string) string {
	switch isolateStatus {
	case "TO":
		return "time_limit_exceeded"
	case "SG":
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

// compile runs the compile command inside isolate.
func (e *ExecutionService) compile(log *zerolog.Logger, boxID int, compileCmd, metaFile string, sub queries.Submission) (*Metadata, string, error) {
	resolvedCmd := fmt.Sprintf(compileCmd, "")

	args := e.buildIsolateArgs(boxID, metaFile, sub)
	args = append(args,
		"--time", fmt.Sprintf("%.1f", sub.WallTimeLimit),
		"--wall-time", fmt.Sprintf("%.1f", sub.WallTimeLimit*2),
		"--stderr-to-stdout",
		"--run", "--",
		"/bin/bash", "-c", resolvedCmd,
	)

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
	e.queries.UpdateSubmissionStatus(ctx, queries.UpdateSubmissionStatusParams{
		ID:     submissionID,
		Status: status,
	})
}

// completeSubmission writes final results back to the DB.
func (e *ExecutionService) completeSubmission(ctx context.Context, log *zerolog.Logger, submissionID int64, status, compileOutput, stdout, stderr string, meta *Metadata, startedAt, finishedAt time.Time) {
	log.Info().
		Str("status", status).
		Int32("exit_code", meta.ExitCode).
		Float64("time_s", meta.Time).
		Int32("memory_kb", meta.Memory).
		Msg("completing submission")
	e.queries.CompleteSubmission(ctx, queries.CompleteSubmissionParams{
		ID:            submissionID,
		Status:        status,
		CompileOutput: pgtype.Text{String: compileOutput, Valid: compileOutput != ""},
		Stdout:        pgtype.Text{String: stdout, Valid: stdout != ""},
		Stderr:        pgtype.Text{String: stderr, Valid: stderr != ""},
		Message:       pgtype.Text{String: meta.Message, Valid: meta.Message != ""},
		InternalError: pgtype.Text{},
		ExitCode:      pgtype.Int4{Int32: meta.ExitCode, Valid: true},
		ExitSignal:    pgtype.Int4{Int32: meta.ExitSignal, Valid: meta.ExitSignal != 0},
		Time:          pgtype.Float8{Float64: meta.Time, Valid: true},
		WallTime:      pgtype.Float8{Float64: meta.WallTime, Valid: true},
		Memory:        pgtype.Int4{Int32: meta.Memory, Valid: true},
		StartedAt:     pgtype.Timestamptz{Time: startedAt, Valid: true},
		FinishedAt:    pgtype.Timestamptz{Time: finishedAt, Valid: true},
	})
}

// truncate shortens a string for log output.
func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
