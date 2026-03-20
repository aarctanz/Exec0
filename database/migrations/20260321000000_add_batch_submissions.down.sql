ALTER TABLE submissions
    DROP CONSTRAINT IF EXISTS submissions_mode_check,
    DROP COLUMN IF EXISTS mode;
