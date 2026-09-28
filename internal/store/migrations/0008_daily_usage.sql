-- Request records summed by day in UTC, for reports: the dashboard and the
-- admin API read these rather than every request, so they stay fast as
-- requests pile up. They're saved in the same transaction as the requests
-- they sum, and outlive their retention.
CREATE TABLE usage_daily (
  day                TEXT    NOT NULL,  -- such as 2026-09-27
  key_id             INTEGER NOT NULL,
  provider           TEXT    NOT NULL,
  model              TEXT    NOT NULL,
  requests           INTEGER NOT NULL,
  errors             INTEGER NOT NULL,  -- requests that got a status of 400 or more
  unpriced           INTEGER NOT NULL,  -- requests with usage but no price
  cost_usd           REAL    NOT NULL,
  savings_usd        REAL    NOT NULL,
  input_tokens       INTEGER NOT NULL,
  output_tokens      INTEGER NOT NULL,
  cache_read_tokens  INTEGER NOT NULL,
  cache_write_tokens INTEGER NOT NULL,
  reasoning_tokens   INTEGER NOT NULL,
  cache_hits         INTEGER NOT NULL,
  cache_misses       INTEGER NOT NULL,
  PRIMARY KEY (day, key_id, provider, model)
) WITHOUT ROWID;

-- Net savings by day and method, such as exact_cache.
CREATE TABLE savings_daily (
  day    TEXT NOT NULL,
  method TEXT NOT NULL,
  usd    REAL NOT NULL,
  PRIMARY KEY (day, method)
) WITHOUT ROWID;

-- What redaction found, by day and type, such as email.
CREATE TABLE redactions_daily (
  day   TEXT    NOT NULL,
  type  TEXT    NOT NULL,
  count INTEGER NOT NULL,
  PRIMARY KEY (day, type)
) WITHOUT ROWID;

INSERT INTO usage_daily
SELECT date(ts / 1000, 'unixepoch'), key_id, provider, model, count(*), sum(status >= 400),
  sum(cost_usd IS NULL AND input_tokens IS NOT NULL), coalesce(sum(cost_usd), 0), sum(savings_usd),
  coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0), coalesce(sum(cache_read_tokens), 0),
  coalesce(sum(cache_write_tokens), 0), coalesce(sum(reasoning_tokens), 0), sum(cache_status = 'hit'),
  sum(cache_status = 'miss')
FROM requests GROUP BY 1, 2, 3, 4;

INSERT INTO savings_daily
SELECT date(ts / 1000, 'unixepoch'), savings_method, sum(savings_usd) FROM requests
WHERE savings_method != '' GROUP BY 1, 2;

INSERT INTO redactions_daily
SELECT date(r.ts / 1000, 'unixepoch'), j.key, sum(j.value) FROM requests r, json_each(r.redactions) j
WHERE r.redactions != '' GROUP BY 1, 2;
