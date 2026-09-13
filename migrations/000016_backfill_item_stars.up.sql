-- 000016_backfill_item_stars.up.sql
--
-- cmd/promote used to insert items without stars, so every repo promoted
-- after 000013's one-time backfill landed with stars = 0 — breaking browse
-- order and the 0.2 star weight in ranking. cmd/promote now copies stars;
-- this repairs rows promoted before that fix.
--
-- Nothing here is estimated. repo_ingest_staging keeps each repo's star
-- count and points at the item it became via item_id, and that is exactly
-- the value cmd/promote should have written. It is the count at load time,
-- not a live GitHub figure.
--
-- Only the bug's signature is touched — items.stars = 0 where staging
-- knows better — so this is idempotent and a no-op on unaffected
-- databases. DISTINCT ON keeps the earliest staging row per item, as in
-- 000014, in case one item is referenced more than once.

UPDATE items i
SET stars = s.stars
FROM (
    SELECT DISTINCT ON (item_id) item_id, stars
    FROM repo_ingest_staging
    WHERE item_id IS NOT NULL
    ORDER BY item_id, id
) s
WHERE s.item_id = i.id
  AND i.stars = 0
  AND s.stars > 0;
