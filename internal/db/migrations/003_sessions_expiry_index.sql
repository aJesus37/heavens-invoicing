-- Sessions expiry index (R2 Task 3): backs both the per-login validation
-- lookup and the lazy DeleteExpired sweep. The table itself arrived in 002.
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
