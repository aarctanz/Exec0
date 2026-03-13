CREATE TABLE IF NOT EXISTS submissions (
    id BIGSERIAL PRIMARY KEY,
    language_id BIGINT NOT NULL REFERENCES languages(id) ON DELETE RESTRICT,

    source_code TEXT NOT NULL,
    stdin TEXT,

    status TEXT NOT NULL DEFAULT 'pending',

    compile_output TEXT,
    stdout TEXT,
    stderr TEXT,
    message TEXT,
    internal_error TEXT,

    exit_code INTEGER,
    exit_signal INTEGER,

    cpu_time_limit DOUBLE PRECISION NOT NULL,
    cpu_extra_time DOUBLE PRECISION NOT NULL,
    wall_time_limit DOUBLE PRECISION NOT NULL,
    memory_limit INTEGER NOT NULL,
    stack_limit INTEGER NOT NULL,
    max_processes_and_or_threads INTEGER NOT NULL,
    enable_per_process_and_thread_time_limit BOOLEAN NOT NULL DEFAULT FALSE,
    enable_per_process_and_thread_memory_limit BOOLEAN NOT NULL DEFAULT FALSE,
    max_file_size INTEGER NOT NULL,
    number_of_runs INTEGER NOT NULL DEFAULT 1,
    redirect_stderr_to_stdout BOOLEAN NOT NULL DEFAULT FALSE,
    enable_network BOOLEAN NOT NULL DEFAULT FALSE,

    time DOUBLE PRECISION,
    wall_time DOUBLE PRECISION,
    memory INTEGER,

    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT submissions_source_code_not_empty CHECK (length(source_code) > 0),
    CONSTRAINT submissions_cpu_time_limit_positive CHECK (cpu_time_limit > 0),
    CONSTRAINT submissions_cpu_extra_time_nonnegative CHECK (cpu_extra_time >= 0),
    CONSTRAINT submissions_wall_time_limit_positive CHECK (wall_time_limit > 0),
    CONSTRAINT submissions_memory_limit_positive CHECK (memory_limit > 0),
    CONSTRAINT submissions_stack_limit_positive CHECK (stack_limit > 0),
    CONSTRAINT submissions_max_processes_positive CHECK (max_processes_and_or_threads > 0),
    CONSTRAINT submissions_max_file_size_positive CHECK (max_file_size > 0),
    CONSTRAINT submissions_number_of_runs_positive CHECK (number_of_runs > 0)
);
