ALTER TABLE submissions
    DROP COLUMN IF EXISTS stdin,
    DROP COLUMN IF EXISTS stdout,
    DROP COLUMN IF EXISTS stderr,
    DROP COLUMN IF EXISTS exit_code,
    DROP COLUMN IF EXISTS exit_signal;
