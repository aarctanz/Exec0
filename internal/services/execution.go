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
	"sync"
	"time"

	"github.com/aarctanz/Exec0/internal/database/queries"
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
	// Try hint first
	id := int(hint % int64(maxBoxID))
	if !p.used[id] {
		p.used[id] = true
		return id
	}
	// Linear scan for a free box
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
	Status     string // RE (runtime error), SG (signal), TO (timeout), XX (internal error)
	Message    string
}

// Execute is the main entry point called by the worker.
func (e *ExecutionService) Execute(ctx context.Context, submissionID int64) error {
	log.Printf("[sub %d] starting execution", submissionID)

	sub, err := e.queries.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		log.Printf("[sub %d] failed to fetch submission: %v", submissionID, err)
		return fmt.Errorf("failed to fetch submission %d: %w", submissionID, err)
	}
	log.Printf("[sub %d] fetched: lang=%d mem=%dKB cpu=%.1fs wall=%.1fs processes=%d fsize=%dKB network=%v",
		submissionID, sub.LanguageID, sub.MemoryLimit, sub.CpuTimeLimit, sub.WallTimeLimit,
		sub.MaxProcessesAndOrThreads, sub.MaxFileSize, sub.EnableNetwork)

	lang, err := e.queries.GetLanguageByID(ctx, sub.LanguageID)
	if err != nil {
		log.Printf("[sub %d] failed to fetch language %d: %v", submissionID, sub.LanguageID, err)
		return fmt.Errorf("failed to fetch language %d: %w", sub.LanguageID, err)
	}
	log.Printf("[sub %d] language: %s %s, source_file=%s, has_compile=%v",
		submissionID, lang.Name, lang.Version, lang.SourceFile, lang.CompileCommand.Valid)

	boxID := globalBoxPool.acquire(submissionID)
	if boxID < 0 {
		log.Printf("[sub %d] no free box IDs available", submissionID)
		return fmt.Errorf("no free box IDs available")
	}
	defer globalBoxPool.release(boxID)

	log.Printf("[sub %d] initializing box %d", submissionID, boxID)
	boxDir, err := e.initBox(boxID)
	if err != nil {
		log.Printf("[sub %d] box init failed: %v", submissionID, err)
		return fmt.Errorf("failed to init box %d: %w", boxID, err)
	}
	log.Printf("[sub %d] box %d ready at %s", submissionID, boxID, boxDir)
	defer e.cleanupBox(boxID)

	log.Printf("[sub %d] writing source (%d bytes) and stdin (%d bytes)",
		submissionID, len(sub.SourceCode), len(sub.Stdin.String))
	if err := e.createFiles(boxDir, sub.SourceCode, lang.SourceFile, sub.Stdin.String); err != nil {
		log.Printf("[sub %d] failed to create files: %v", submissionID, err)
		return fmt.Errorf("failed to create files in box %d: %w", boxID, err)
	}

	startedAt := time.Now()
	metaFile := filepath.Join(os.TempDir(), fmt.Sprintf("isolate_meta_%d.txt", boxID))
	defer os.Remove(metaFile)

	// Compile if language has a compile command
	if lang.CompileCommand.Valid {
		log.Printf("[sub %d] compiling with: %s", submissionID, lang.CompileCommand.String)
		e.updateStatus(ctx, submissionID, "compiling")

		compileMeta, compileOutput, err := e.compile(boxID, lang.CompileCommand.String, metaFile, sub)
		if err != nil {
			log.Printf("[sub %d] compile step failed: %v", submissionID, err)
			return fmt.Errorf("compilation error in box %d: %w", boxID, err)
		}

		log.Printf("[sub %d] compile done: exit_code=%d status=%q time=%.3fs mem=%dKB output=%q",
			submissionID, compileMeta.ExitCode, compileMeta.Status, compileMeta.Time,
			compileMeta.Memory, truncate(compileOutput, 200))

		if compileMeta.Status != "" || compileMeta.ExitCode != 0 {
			finishedAt := time.Now()
			log.Printf("[sub %d] compilation error, completing submission", submissionID)
			e.completeSubmission(ctx, sub.ID, "compilation_error", compileOutput, "", "", compileMeta, startedAt, finishedAt)
			return nil
		}

		log.Printf("[sub %d] compilation succeeded, resetting metadata", submissionID)
		os.Remove(metaFile)
	}

	// Run
	log.Printf("[sub %d] running with: %s", submissionID, lang.RunCommand)
	e.updateStatus(ctx, submissionID, "running")

	runMeta, err := e.run(boxID, lang.RunCommand, metaFile, sub)
	if err != nil {
		log.Printf("[sub %d] run step failed: %v", submissionID, err)
		return fmt.Errorf("run error in box %d: %w", boxID, err)
	}

	finishedAt := time.Now()

	log.Printf("[sub %d] run done: exit_code=%d exit_signal=%d status=%q time=%.3fs wall=%.3fs mem=%dKB message=%q",
		submissionID, runMeta.ExitCode, runMeta.ExitSignal, runMeta.Status,
		runMeta.Time, runMeta.WallTime, runMeta.Memory, runMeta.Message)

	stdout, _ := os.ReadFile(filepath.Join(boxDir, "box", "stdout.txt"))
	stderr, _ := os.ReadFile(filepath.Join(boxDir, "box", "stderr.txt"))
	log.Printf("[sub %d] output: stdout=%d bytes, stderr=%d bytes", submissionID, len(stdout), len(stderr))

	status := "accepted"
	if runMeta.Status != "" {
		status = e.mapIsolateStatus(runMeta.Status)
	}

	log.Printf("[sub %d] completing: status=%s, total_duration=%.3fs", submissionID, status, finishedAt.Sub(startedAt).Seconds())
	e.completeSubmission(ctx, sub.ID, status, "", string(stdout), string(stderr), runMeta, startedAt, finishedAt)
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
// If a stale box exists, it cleans it up first and retries.
func (e *ExecutionService) initBox(boxID int) (string, error) {
	id := strconv.Itoa(boxID)
	cmd := exec.Command("isolate", "--box-id", id, "--cg", "--init")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Init failed — likely a stale box. Try cleaning up (with and without --cg) then retry.
	log.Printf("[box %d] init failed, attempting stale box cleanup", boxID)
	exec.Command("isolate", "--box-id", id, "--cg", "--cleanup").Run()
	exec.Command("isolate", "--box-id", id, "--cleanup").Run()

	cmd = exec.Command("isolate", "--box-id", id, "--cg", "--init")
	output, err = cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Printf("[box %d] isolate --init stderr: %s", boxID, string(exitErr.Stderr))
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

	if sub.EnablePerProcessAndThreadTimeLimit {
		args = append(args, "--cg-timing")
	}

	if sub.EnablePerProcessAndThreadMemoryLimit {
		args = append(args, "--mem", strconv.Itoa(int(sub.MemoryLimit)))
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

	log.Printf("[box %d] compile cmd: isolate %s", boxID, strings.Join(args, " "))

	cmd := exec.Command("isolate", args...)
	output, cmdErr := cmd.CombinedOutput()

	if cmdErr != nil {
		log.Printf("[box %d] compile exited with error: %v, output: %s", boxID, cmdErr, truncate(string(output), 500))
	}

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		log.Printf("[box %d] failed to read compile metadata: %v (isolate error: %v, output: %s)",
			boxID, err, cmdErr, truncate(string(output), 500))
		return nil, string(output), fmt.Errorf("failed to read compile metadata (isolate error: %v, output: %s): %w", cmdErr, string(output), err)
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

	log.Printf("[box %d] run cmd: isolate %s", boxID, strings.Join(args, " "))

	cmd := exec.Command("isolate", args...)
	output, cmdErr := cmd.CombinedOutput()

	if cmdErr != nil {
		log.Printf("[box %d] run exited with error: %v, output: %s", boxID, cmdErr, truncate(string(output), 500))
	}

	meta, err := e.readMetadata(metaFile)
	if err != nil {
		log.Printf("[box %d] failed to read run metadata: %v (isolate error: %v, output: %s)",
			boxID, err, cmdErr, truncate(string(output), 500))
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
func (e *ExecutionService) cleanupBox(boxID int) {
	log.Printf("[box %d] cleaning up", boxID)
	cmd := exec.Command("isolate", "--box-id", strconv.Itoa(boxID), "--cg", "--cleanup")
	if err := cmd.Run(); err != nil {
		log.Printf("[box %d] cleanup failed: %v", boxID, err)
	}
}

// updateStatus updates submission status in the DB.
func (e *ExecutionService) updateStatus(ctx context.Context, submissionID int64, status string) {
	log.Printf("[sub %d] status -> %s", submissionID, status)
	e.queries.UpdateSubmissionStatus(ctx, queries.UpdateSubmissionStatusParams{
		ID:     submissionID,
		Status: status,
	})
}

// completeSubmission writes final results back to the DB.
func (e *ExecutionService) completeSubmission(ctx context.Context, submissionID int64, status, compileOutput, stdout, stderr string, meta *Metadata, startedAt, finishedAt time.Time) {
	log.Printf("[sub %d] completing: status=%s exit_code=%d time=%.3fs memory=%dKB",
		submissionID, status, meta.ExitCode, meta.Time, meta.Memory)
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
