CREATE TABLE events (
    id BIGSERIAL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    item_id BIGINT NOT NULL REFERENCES items(id),
    event_type TEXT NOT NULL,
    event_value NUMERIC,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE events_2026_08
    PARTITION OF events
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
