

ALTER TABLE events
    DROP CONSTRAINT events_event_id_created_at_unique;

ALTER TABLE events
    DROP COLUMN event_id;