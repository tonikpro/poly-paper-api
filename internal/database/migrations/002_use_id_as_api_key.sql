-- Use the existing id (UUID) as the API key directly; drop the redundant api_key column.
ALTER TABLE api_keys DROP COLUMN api_key;
