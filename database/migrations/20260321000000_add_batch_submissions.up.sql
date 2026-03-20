-- Add mode column to submissions
ALTER TABLE submissions
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'single';

ALTER TABLE submissions
    ADD CONSTRAINT submissions_mode_check CHECK (mode IN ('single', 'batch'));
