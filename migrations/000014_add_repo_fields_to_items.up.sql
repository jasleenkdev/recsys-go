-- 000014_add_repo_fields_to_items.up.sql
--
-- owner/language/topics/github_id were only ever written to
-- repo_ingest_staging, so serving paths had no way to build a GitHub URL
-- or filter by language without joining a staging table. Promote them to
-- items, where the API can read them.

ALTER TABLE items
    ADD COLUMN owner TEXT,
    ADD COLUMN language TEXT,
    ADD COLUMN topics TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN github_id BIGINT;

UPDATE items i
SET owner     = s.owner,
    language  = s.language,
    topics    = COALESCE(s.topics, '{}')
FROM repo_ingest_staging s
WHERE s.item_id = i.id;

-- github_id is set separately: it carries a UNIQUE constraint, so a
-- staging row that was promoted twice would abort the whole backfill
-- above. DISTINCT ON keeps the earliest staging row per item.
UPDATE items i
SET github_id = s.github_id
FROM (
    SELECT DISTINCT ON (item_id) item_id, github_id
    FROM repo_ingest_staging
    WHERE item_id IS NOT NULL
    ORDER BY item_id, id
) s
WHERE s.item_id = i.id;

-- Safe only because the backfill above covers every existing row; new
-- rows come from cmd/promote, which now supplies owner.
ALTER TABLE items ALTER COLUMN owner SET NOT NULL;

ALTER TABLE items ADD CONSTRAINT items_github_id_unique UNIQUE (github_id);

-- Browse orders by (stars DESC, id DESC) and optionally filters by
-- language; these two indexes cover both shapes as keyset scans.
CREATE INDEX items_stars_id_idx ON items (stars DESC, id DESC);
CREATE INDEX items_language_stars_id_idx ON items (language, stars DESC, id DESC);
