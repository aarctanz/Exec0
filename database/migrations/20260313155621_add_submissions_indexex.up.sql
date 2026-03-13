CREATE INDEX idx_submissions_language_id ON submissions(language_id);
CREATE INDEX idx_submissions_status ON submissions(status);
CREATE INDEX idx_submissions_created_at ON submissions(created_at DESC);
CREATE INDEX idx_submissions_finished_at ON submissions(finished_at DESC);
CREATE INDEX idx_submissions_language_status_created_at
    ON submissions(language_id, status, created_at DESC);
