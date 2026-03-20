CREATE TABLE IF NOT EXISTS test_case_results (
    id BIGSERIAL PRIMARY KEY,
    submission_id BIGINT NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL,

    stdin TEXT,
    expected_output TEXT,
    stdout TEXT,
    stderr TEXT,

    exit_code INTEGER,
    exit_signal INTEGER,

    status TEXT NOT NULL DEFAULT 'pending',

    time DOUBLE PRECISION,
    wall_time DOUBLE PRECISION,
    memory INTEGER,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (submission_id, position)
);

CREATE INDEX idx_test_case_results_submission_id ON test_case_results(submission_id);
