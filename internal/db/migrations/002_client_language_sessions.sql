-- Client-facing language preference plus the sessions table for R2 Task 3
-- (created up front so that task ships without another migration; nothing
-- reads sessions yet).
ALTER TABLE clients ADD COLUMN language TEXT NOT NULL DEFAULT 'pt-BR';

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);
