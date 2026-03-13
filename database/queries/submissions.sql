-- name: CreateSubmission :one
INSERT INTO submissions (
    language_id,
    source_code,
    stdin,
    cpu_time_limit,
    cpu_extra_time,
    wall_time_limit,
    memory_limit,
    stack_limit,
    max_processes_and_or_threads,
    enable_per_process_and_thread_time_limit,
    enable_per_process_and_thread_memory_limit,
    max_file_size,
    redirect_stderr_to_stdout,
    enable_network
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14
)
RETURNING *;

-- name: GetSubmissionByID :one
SELECT * FROM submissions
WHERE id = $1
LIMIT 1;

-- name: ListSubmissions :many
SELECT * FROM submissions
ORDER BY created_at DESC;

-- name: UpdateSubmissionStatus :exec
UPDATE submissions
SET
    status = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: CompleteSubmission :one
UPDATE submissions
SET
    status = $2,
    compile_output = $3,
    stdout = $4,
    stderr = $5,
    message = $6,
    internal_error = $7,
    exit_code = $8,
    exit_signal = $9,
    time = $10,
    wall_time = $11,
    memory = $12,
    started_at = $13,
    finished_at = $14,
    updated_at = NOW()
WHERE id = $1
RETURNING *;
