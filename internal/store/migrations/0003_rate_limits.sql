-- Rate limits per minute. NULL means no limit.
ALTER TABLE virtual_keys ADD COLUMN rpm INTEGER;
ALTER TABLE virtual_keys ADD COLUMN tpm INTEGER;
