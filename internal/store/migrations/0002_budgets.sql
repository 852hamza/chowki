-- Monthly budgets in USD. NULL means no budget.
ALTER TABLE projects ADD COLUMN monthly_budget_usd REAL;
ALTER TABLE virtual_keys ADD COLUMN monthly_budget_usd REAL;

-- Spend of each key per calendar month in UTC, such as 2026-09. It's saved
-- in the same transaction as the request records it adds up, so the two
-- always agree, and it outlives their retention.
CREATE TABLE spend (
  key_id    INTEGER NOT NULL,
  period    TEXT    NOT NULL,
  spent_usd REAL    NOT NULL,
  PRIMARY KEY (key_id, period)
);
