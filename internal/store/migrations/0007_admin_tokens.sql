-- Admin tokens, for the admin API and the dashboard. As for virtual keys,
-- the token itself is never stored: only its prefix and its SHA-256 hash.
CREATE TABLE admin_tokens (
  id         INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL,
  prefix     TEXT    NOT NULL UNIQUE,
  token_hash BLOB    NOT NULL,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER
);
