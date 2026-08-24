-- 000014_add_repo_fields_to_items.down.sql

DROP INDEX IF EXISTS items_language_stars_id_idx;
DROP INDEX IF EXISTS items_stars_id_idx;

ALTER TABLE items DROP CONSTRAINT IF EXISTS items_github_id_unique;

ALTER TABLE items
    DROP COLUMN IF EXISTS github_id,
    DROP COLUMN IF EXISTS topics,
    DROP COLUMN IF EXISTS language,
    DROP COLUMN IF EXISTS owner;
