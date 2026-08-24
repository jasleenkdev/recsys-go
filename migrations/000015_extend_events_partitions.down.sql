-- 000015_extend_events_partitions.down.sql

DO $$
DECLARE
    month_start DATE;
BEGIN
    FOR month_start IN
        SELECT generate_series('2026-09-01'::date, '2027-12-01'::date, '1 month')::date
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I', 'events_' || to_char(month_start, 'YYYY_MM'));
    END LOOP;
END $$;
