ALTER TABLE items ADD COLUMN stars INTEGER NOT NULL DEFAULT 0;

UPDATE items i
SET stars = s.stars
FROM repo_ingest_staging s
WHERE s.item_id = i.id;