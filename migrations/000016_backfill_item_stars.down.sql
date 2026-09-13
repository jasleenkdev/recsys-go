-- 000016_backfill_item_stars.down.sql
--
-- Intentionally a no-op. The up migration only replaced stars = 0 values
-- that were a bug; restoring them would reintroduce it.
SELECT 1;
