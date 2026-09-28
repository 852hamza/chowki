-- What happens to secrets and personal data in a key's requests: off, mask,
-- block or alert. NULL follows the configuration's defaults.redaction.
ALTER TABLE virtual_keys ADD COLUMN redaction_mode TEXT;

-- What redaction found in a request, as a JSON object of counts by type,
-- such as {"email":2}; empty when it found nothing. Never the values.
ALTER TABLE requests ADD COLUMN redactions TEXT NOT NULL DEFAULT '';
