-- The exact response cache. The request hash identifies an entry; the
-- response is sealed with a key derived from the master key, and bound to
-- the hash. used_at is the last hit, for least-recently-used eviction.
CREATE TABLE cache_entries (
  hash              BLOB    PRIMARY KEY,
  ciphertext        BLOB    NOT NULL,
  content_type      TEXT    NOT NULL,
  original_cost_usd REAL,
  created_at        INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL,
  used_at           INTEGER NOT NULL,
  hits              INTEGER NOT NULL,
  size_bytes        INTEGER NOT NULL
);

CREATE INDEX cache_entries_used ON cache_entries (used_at);
CREATE INDEX cache_entries_expires ON cache_entries (expires_at);

-- Whether a key's requests use the exact cache: exact or off. NULL follows
-- the configuration's defaults.cache.
ALTER TABLE virtual_keys ADD COLUMN cache_mode TEXT;

-- hit, miss or bypass; empty for a request rejected before the cache.
ALTER TABLE requests ADD COLUMN cache_status TEXT NOT NULL DEFAULT '';
