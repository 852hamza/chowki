-- Times are Unix milliseconds (UTC). NULL token counts mean the provider
-- reported no usage; a NULL cost means the model is unpriced.

CREATE TABLE projects (
  id         INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

-- The key itself is never stored: only its prefix, which identifies it,
-- and its SHA-256 hash.
CREATE TABLE virtual_keys (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id),
  name       TEXT    NOT NULL,
  prefix     TEXT    NOT NULL UNIQUE,
  key_hash   BLOB    NOT NULL,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER
);

CREATE INDEX virtual_keys_project ON virtual_keys (project_id);

-- Request metadata. Never prompts, responses or keys.
CREATE TABLE requests (
  id                 TEXT    PRIMARY KEY,
  ts                 INTEGER NOT NULL,
  key_id             INTEGER NOT NULL,
  project_id         INTEGER NOT NULL,
  api_family         TEXT    NOT NULL,
  endpoint           TEXT    NOT NULL,
  provider           TEXT    NOT NULL,
  model              TEXT    NOT NULL,
  stream             INTEGER NOT NULL,
  status             INTEGER NOT NULL,
  error_type         TEXT    NOT NULL,
  latency_ms         INTEGER NOT NULL,
  ttfb_ms            INTEGER,
  input_tokens       INTEGER,
  output_tokens      INTEGER,
  cache_read_tokens  INTEGER,
  cache_write_tokens INTEGER,
  reasoning_tokens   INTEGER,
  cost_usd           REAL,
  savings_usd        REAL    NOT NULL,
  savings_method     TEXT    NOT NULL
);

CREATE INDEX requests_ts ON requests (ts);
CREATE INDEX requests_key_ts ON requests (key_id, ts);
CREATE INDEX requests_project_ts ON requests (project_id, ts);

-- Who changed what. details is a JSON object and never holds secrets.
CREATE TABLE audit_log (
  id      INTEGER PRIMARY KEY,
  ts      INTEGER NOT NULL,
  actor   TEXT    NOT NULL,
  action  TEXT    NOT NULL,
  target  TEXT    NOT NULL,
  details TEXT    NOT NULL
);
