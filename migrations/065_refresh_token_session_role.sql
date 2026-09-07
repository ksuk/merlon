-- Persist the authority snapshot that owns each refresh-token family. A role
-- or activation change requires a new login instead of allowing an older
-- refresh token to mint credentials with different authority.
ALTER TABLE refresh_tokens
    ADD COLUMN IF NOT EXISTS session_role TEXT NOT NULL DEFAULT ''
    CHECK (session_role IN ('', 'admin', 'analyst', 'viewer'));
