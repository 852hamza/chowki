-- The models a key may request, as a JSON array of names, aliases and
-- patterns such as "openai/*". NULL allows every model.
ALTER TABLE virtual_keys ADD COLUMN allowed_models TEXT;
