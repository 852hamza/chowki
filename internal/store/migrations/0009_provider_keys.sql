-- Provider keys that the admin stores in Chowki instead of an environment
-- variable. Each is sealed with AES-256-GCM, under a key derived from the
-- master key, and bound to its provider's name. No part of a key is stored
-- in the clear.
CREATE TABLE provider_keys (
  provider   TEXT    PRIMARY KEY,
  sealed     BLOB    NOT NULL,
  updated_at INTEGER NOT NULL
);
