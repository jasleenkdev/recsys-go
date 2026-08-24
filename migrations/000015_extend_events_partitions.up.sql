-- 000015_extend_events_partitions.up.sql
--
-- events had exactly one partition, ending 2026-09-01. Every insert past
-- that date fails with "no partition of relation events found" — which
-- POST /v1/events would have turned into user-facing data loss. Create
-- monthly partitions through 2027-12.
--
-- NOTE: this still needs extending before 2028-01. The real fix is a
-- scheduled job that rolls the next month's partition ahead of time.

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR month_start IN
        SELECT generate_series('2026-09-01'::date, '2027-12-01'::date, '1 month')::date
    LOOP
        month_end := month_start + INTERVAL '1 month';
        part_name := 'events_' || to_char(month_start, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF events FOR VALUES FROM (%L) TO (%L)',
            part_name, month_start, month_end
        );
    END LOOP;
END $$;
