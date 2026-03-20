-- name: CreateTestCaseResult :one
INSERT INTO test_case_results (
    submission_id,
    position,
    stdin,
    expected_output
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetTestCaseResultsBySubmissionID :many
SELECT * FROM test_case_results
WHERE submission_id = $1
ORDER BY position ASC;

-- name: UpdateTestCaseResult :one
UPDATE test_case_results
SET
    stdout = $2,
    stderr = $3,
    exit_code = $4,
    exit_signal = $5,
    status = $6,
    time = $7,
    wall_time = $8,
    memory = $9
WHERE id = $1
RETURNING *;
