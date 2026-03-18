package services

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxBoxID = 2147483647 // 2^31 - 1

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
	Status     string // RE (runtime error), SG (signal), TO (timeout), XX (internal error)
	Message    string
}

// Execute is the main entry point called by the worker.
func (e *ExecutionService) Execute(ctx context.Context, submissionID int64) error {
	sub, err := e.queries.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return fmt.Errorf("failed to fetch submission %d: %w", submissionID, err)
	}

	lang, err := e.queries.GetLanguageByID(ctx, sub.LanguageID)
	if err != nil {
		return fmt.Errorf("failed to fetch language %d: %w", sub.LanguageID, err)
	}

	boxID := int(submissionID % maxBoxID)

	boxDir, err := e.initBox(boxID)
	if err != nil {
		return fmt.Errorf("failed to init box %d: %w", boxID, err)
	}
	defer e.cleanupBox(boxID)

	if err := e.createFiles(boxDir, sub.SourceCode, lang.SourceFile, sub.Stdin.String); err != nil {
		return fmt.Errorf("failed to create files in box %d: %w", boxID, err)
	}

	startedAt := time.Now()

	// Compile if language has a compile command
	if lang.CompileCommand.Valid {
		e.updateStatus(ctx, submissionID, "compiling")

		compileMetaFile := filepath.Join(boxDir, "meta_compile.txt")
		compileMeta, compileOutput, err := e.compile(boxID, lang.CompileCommand.String, compileMetaFile, sub)
		if err != nil {
			return fmt.Errorf("compilation error in box %d: %w", boxID, err)
		}

		if compileMeta.Status != "" || compileMeta.ExitCode != 0 {
			finishedAt := time.Now()
			e.completeSubmission(ctx, sub.ID, "compilation_error", compileOutput, "", "", compileMeta, startedAt, finishedAt)
			return nil
		}
	}

	// Run (supports number_of_runs)
	e.updateStatus(ctx, submissionID, "running")

	numRuns := int(sub.NumberOfRuns)
	if numRuns < 1 {
		numRuns = 1
	}

	var bestMeta *Metadata
	for i := 0; i < numRuns; i++ {
		runMetaFile := filepath.Join(boxDir, fmt.Sprintf("meta_run_%d.txt", i))
		runMeta, err := e.run(boxID, lang.RunCommand, runMetaFile, sub)
		if err != nil {
			return fmt.Errorf("run error in box %d: %w", boxID, err)
		}

		// Keep the run with the lowest CPU time (best performance)
		if bestMeta == nil || runMeta.Time < bestMeta.Time {
			bestMeta = runMeta
		}

		// Stop early on failure
		if runMeta.Status != "" {
			bestMeta = runMeta
			break
		}
	}

	finishedAt := time.Now()

	stdout, _ := os.ReadFile(filepath.Join(boxDir, "box", "stdout.txt"))
	stderr, _ := os.ReadFile(filepath.Join(boxDir, "box", "stderr.txt"))

	status := "completed"
	if bestMeta.Status != "" {
		status = e.mapIsolateStatus(bestMeta.Status)
	}

	e.completeSubmission(ctx, sub.ID, status, "", string(stdout), string(stderr), bestMeta, startedAt, finishedAt)
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
func (e *ExecutionService) initBox(boxID int) (string, error) {
	cmd := exec.Command("isolate", "--box-id", strconv.Itoa(boxID), "--init")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("isolate --init failed: %w", err)
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
		"--mem", strconv.Itoa(int(sub.MemoryLimit)),
		"--processes", strconv.Itoa(int(sub.MaxProcessesAndOrThreads)),
		"--fsize", strconv.Itoa(int(sub.MaxFileSize)),
	}

	if sub.EnablePerProcessAndThreadTimeLimit {
		args = append(args, "--cg", "--cg-timing")
	}

	if sub.EnablePerProcessAndThreadMemoryLimit {
		args = append(args, "--cg", "--cg-mem", strconv.Itoa(int(sub.MemoryLimit)))
	}

	if sub.EnableNetwork {
		args = append(args, "--share-net")
	}

	return args
}

// compile runs the compile command inside isolate.
func (e *ExecutionService) compile(boxID int, compileCmd, metaFile string, sub queries.Submission) (*Metadata, string, error) {
	resolvedCmd := fmt.Sprintf(compileCmd, "")

	args := e.buildIsolateArgs(boxID, metaFile, sub)
	args = append(args,
		"--time", fmt.Sprintf("%.1f", sub.WallTimeLimit),
		"--wall-time", fmt.Sprintf("%.1f", sub.WallTimeLimit*2),
		"--stderr-to-stdout",
		"--run", "--",
		"/bin/bash", "-c", resolvedCmd,
	)

	cmd := exec.Command("isolate", args...)
	output, _ := cmd.CombinedOutput()

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		return nil, string(output), fmt.Errorf("failed to read compile metadata: %w", err)
	}

	return meta, string(output), nil
}

// run executes the program inside isolate with the submission's resource limits.
func (e *ExecutionService) run(boxID int, runCmd, metaFile string, sub queries.Submission) (*Metadata, error) {
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

	cmd := exec.Command("isolate", args...)
	cmd.Run()

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read run metadata: %w", err)
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
func (e *ExecutionService) cleanupBox(boxID int) {
	cmd := exec.Command("isolate", "--box-id", strconv.Itoa(boxID), "--cleanup")
	if err := cmd.Run(); err != nil {
		log.Printf("failed to cleanup box %d: %v", boxID, err)
	}
}

// updateStatus updates submission status in the DB.
func (e *ExecutionService) updateStatus(ctx context.Context, submissionID int64, status string) {
	e.queries.UpdateSubmissionStatus(ctx, queries.UpdateSubmissionStatusParams{
		ID:     submissionID,
		Status: status,
	})
}

// completeSubmission writes final results back to the DB.
func (e *ExecutionService) completeSubmission(ctx context.Context, submissionID int64, status, compileOutput, stdout, stderr string, meta *Metadata, startedAt, finishedAt time.Time) {
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

