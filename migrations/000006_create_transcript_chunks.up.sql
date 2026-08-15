CREATE TABLE transcript_chunks (
    id BIGSERIAL PRIMARY KEY,
    item_id BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    chunk_text TEXT NOT NULL,
    embedding vector(768) NOT NULL,
    timestamp_start INTEGER NOT NULL,
    timestamp_end INTEGER NOT NULL,
    model_id BIGINT NOT NULL REFERENCES models(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
