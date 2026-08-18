ALTER TABLE events
    ADD COLUMN event_id UUID NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE events
    ADD CONSTRAINT events_event_id_created_at_unique
    UNIQUE (event_id, created_at);